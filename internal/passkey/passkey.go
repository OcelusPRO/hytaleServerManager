// Package passkey lets the operator sign in to the panel with passkeys
// (WebAuthn), and optionally turn the password off once one is registered.
//
// The WebAuthn ceremonies are verified by github.com/go-webauthn/webauthn;
// this package keeps the credentials, the pending ceremonies and the
// password switch. Attestation is not requested, credentials are
// discoverable and user verification (biometrics, PIN) is required.
// Resetting the password from the environment (HSM_RESET_PASSWORD) also
// removes every passkey.
package passkey

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"hytale-server-manager/internal/fsutil"
)

const (
	ceremonyTTL   = 5 * time.Minute
	maxCeremonies = 64
	maxPasskeys   = 20
	maxNameLength = 64
	rpName        = "Hytale Server Manager"
	userName      = "admin"
)

// Errors.
var (
	ErrCeremony         = errors.New("the request expired, try again")
	ErrVerification     = errors.New("passkey verification failed")
	ErrUnknown          = errors.New("unknown passkey")
	ErrTooMany          = fmt.Errorf("no more than %d passkeys", maxPasskeys)
	ErrLastPasskey      = errors.New("turn password sign-in back on before deleting the last passkey")
	ErrNoPasskey        = errors.New("register a passkey first")
	ErrInvalidName      = fmt.Errorf("invalid name (1 to %d characters)", maxNameLength)
	ErrPasswordDisabled = errors.New("password sign-in is disabled")
	ErrDuplicate        = errors.New("passkey already registered")
)

var b64 = base64.RawURLEncoding

// Info describes a registered passkey for the interface.
type Info struct {
	ID         string    `json:"id"` // base64url credential id
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt,omitzero"`
}

// record is a stored passkey: the credential record of the library plus
// what the operator sees.
type record struct {
	Name       string              `json:"name"`
	CreatedAt  time.Time           `json:"createdAt"`
	LastUsedAt time.Time           `json:"lastUsedAt,omitzero"`
	Credential webauthn.Credential `json:"webauthn"`
}

func (r record) id() string { return b64.EncodeToString(r.Credential.ID) }

type stored struct {
	UserHandle       []byte   `json:"userHandle"`
	PasswordDisabled bool     `json:"passwordDisabled"`
	Passkeys         []record `json:"passkeys"`
}

// Origin is where a ceremony takes place: the panel's address as seen by
// the browser.
type Origin struct {
	Scheme string // "https", or "http" on localhost
	Host   string // host[:port]
}

// rpID is the WebAuthn relying party id: the host name without the port.
func (o Origin) rpID() string {
	host := o.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.Trim(host, "[]"))
}

func (o Origin) String() string { return strings.ToLower(o.Scheme + "://" + o.Host) }

// relyingParty configures the library for the address in use: passkeys are
// bound to the host name the panel is reached at.
func (o Origin) relyingParty() (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPID:          o.rpID(),
		RPDisplayName: rpName,
		RPOrigins:     []string{o.String()},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL},
		},
	})
}

// user is the panel's single account, as the library sees it.
type user struct {
	handle      []byte
	credentials []webauthn.Credential
}

func (u user) WebAuthnID() []byte                         { return u.handle }
func (u user) WebAuthnName() string                       { return userName }
func (u user) WebAuthnDisplayName() string                { return rpName }
func (u user) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

type ceremony struct {
	session  webauthn.SessionData
	origin   Origin
	register bool
	expires  time.Time
}

// Service manages passkeys and their ceremonies.
type Service struct {
	path string
	now  func() time.Time

	mu         sync.Mutex
	data       stored
	ceremonies map[string]ceremony
}

// Open loads the passkeys stored in path.
func Open(path string) (*Service, error) {
	s := &Service{path: path, now: time.Now, ceremonies: make(map[string]ceremony)}
	if _, err := fsutil.ReadJSON(path, &s.data); err != nil {
		return nil, err
	}
	// Records without a credential (unreadable or from an older format)
	// cannot be used to sign in.
	s.data.Passkeys = slices.DeleteFunc(s.data.Passkeys, func(r record) bool {
		return len(r.Credential.ID) == 0 || len(r.Credential.PublicKey) == 0
	})
	return s, nil
}

// Reset removes every passkey and turns password sign-in back on.
func (s *Service) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Passkeys = nil
	s.data.PasswordDisabled = false
	return s.saveLocked()
}

// List returns the registered passkeys.
func (s *Service) List() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Info, len(s.data.Passkeys))
	for i, r := range s.data.Passkeys {
		out[i] = Info{ID: r.id(), Name: r.Name, CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt}
	}
	return out
}

// Count returns the number of registered passkeys.
func (s *Service) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data.Passkeys)
}

// PasswordDisabled reports whether password sign-in is turned off.
func (s *Service) PasswordDisabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.PasswordDisabled && len(s.data.Passkeys) > 0
}

// SetPasswordDisabled turns password sign-in off (at least one passkey is
// required) or back on.
func (s *Service) SetPasswordDisabled(disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if disabled && len(s.data.Passkeys) == 0 {
		return ErrNoPasskey
	}
	s.data.PasswordDisabled = disabled
	return s.saveLocked()
}

// Rename changes the label of a passkey.
func (s *Service) Rename(id, name string) error {
	name, err := cleanName(name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return ErrUnknown
	}
	s.data.Passkeys[i].Name = name
	return s.saveLocked()
}

// Delete removes a passkey. The last one cannot be removed while the
// password is disabled: the panel would become unreachable.
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexLocked(id)
	if i < 0 {
		return ErrUnknown
	}
	if s.data.PasswordDisabled && len(s.data.Passkeys) == 1 {
		return ErrLastPasskey
	}
	s.data.Passkeys = slices.Delete(s.data.Passkeys, i, i+1)
	return s.saveLocked()
}

// Options are the creation or request options sent to the browser, with
// the ceremony to answer.
type Options struct {
	Ceremony  string `json:"ceremony"`
	PublicKey any    `json:"publicKey"`
}

// BeginRegistration starts adding a passkey from origin.
func (s *Service) BeginRegistration(origin Origin) (Options, error) {
	rp, err := origin.relyingParty()
	if err != nil {
		return Options{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data.Passkeys) >= maxPasskeys {
		return Options{}, ErrTooMany
	}
	if len(s.data.UserHandle) == 0 {
		s.data.UserHandle = randomBytes(64)
		if err := s.saveLocked(); err != nil {
			return Options{}, err
		}
	}
	u := s.userLocked()
	exclude := make([]protocol.CredentialDescriptor, 0, len(u.credentials))
	for _, c := range u.credentials {
		exclude = append(exclude, c.Descriptor())
	}
	requireKey := true
	creation, session, err := rp.BeginRegistration(u,
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: &requireKey,
			UserVerification:   protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(exclude),
	)
	if err != nil {
		return Options{}, err
	}
	return Options{Ceremony: s.newCeremonyLocked(*session, origin, true), PublicKey: creation.Response}, nil
}

// FinishRegistration verifies the credential returned by the browser for
// ceremonyID (a PublicKeyCredential in JSON) and stores it under name.
func (s *Service) FinishRegistration(ceremonyID, name string, response json.RawMessage) (Info, error) {
	name, err := cleanName(name)
	if err != nil {
		return Info{}, err
	}
	c, ok := s.takeCeremony(ceremonyID, true)
	if !ok {
		return Info{}, ErrCeremony
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(response))
	if err != nil {
		return Info{}, verificationError(err)
	}
	rp, err := c.origin.relyingParty()
	if err != nil {
		return Info{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	credential, err := rp.CreateCredential(s.userLocked(), c.session, parsed)
	if err != nil {
		return Info{}, verificationError(err)
	}
	r := record{Name: name, CreatedAt: s.now().UTC(), Credential: *credential}
	if s.indexLocked(r.id()) >= 0 {
		return Info{}, ErrDuplicate
	}
	if len(s.data.Passkeys) >= maxPasskeys {
		return Info{}, ErrTooMany
	}
	s.data.Passkeys = append(s.data.Passkeys, r)
	if err := s.saveLocked(); err != nil {
		return Info{}, err
	}
	return Info{ID: r.id(), Name: r.Name, CreatedAt: r.CreatedAt}, nil
}

// BeginLogin starts a sign-in from origin.
func (s *Service) BeginLogin(origin Origin) (Options, error) {
	rp, err := origin.relyingParty()
	if err != nil {
		return Options{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data.Passkeys) == 0 {
		return Options{}, ErrNoPasskey
	}
	assertion, session, err := rp.BeginLogin(s.userLocked(), webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return Options{}, err
	}
	return Options{Ceremony: s.newCeremonyLocked(*session, origin, false), PublicKey: assertion.Response}, nil
}

// FinishLogin verifies the assertion returned by the browser for
// ceremonyID (a PublicKeyCredential in JSON).
func (s *Service) FinishLogin(ceremonyID string, response json.RawMessage) error {
	c, ok := s.takeCeremony(ceremonyID, false)
	if !ok {
		return ErrCeremony
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(response))
	if err != nil {
		return verificationError(err)
	}
	rp, err := c.origin.relyingParty()
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	credential, err := rp.ValidateLogin(s.userLocked(), c.session, parsed)
	if err != nil {
		return verificationError(err)
	}
	// A counter going backwards reveals a cloned authenticator: refused.
	if credential.Authenticator.CloneWarning {
		return fmt.Errorf("%w: signature counter went backwards (cloned authenticator?)", ErrVerification)
	}
	i := s.indexLocked(b64.EncodeToString(credential.ID))
	if i < 0 {
		return ErrUnknown
	}
	// The library returns the updated record (counter, flags): keep it.
	s.data.Passkeys[i].Credential = *credential
	s.data.Passkeys[i].LastUsedAt = s.now().UTC()
	return s.saveLocked()
}

// ---- internals ----

func (s *Service) userLocked() user {
	creds := make([]webauthn.Credential, len(s.data.Passkeys))
	for i, r := range s.data.Passkeys {
		creds[i] = r.Credential
	}
	return user{handle: s.data.UserHandle, credentials: creds}
}

func (s *Service) newCeremonyLocked(session webauthn.SessionData, origin Origin, register bool) string {
	now := s.now()
	for id, c := range s.ceremonies {
		if now.After(c.expires) {
			delete(s.ceremonies, id)
		}
	}
	if len(s.ceremonies) >= maxCeremonies {
		clear(s.ceremonies) // flooding: start over
	}
	id := b64.EncodeToString(randomBytes(16))
	s.ceremonies[id] = ceremony{session: session, origin: origin, register: register, expires: now.Add(ceremonyTTL)}
	return id
}

// takeCeremony consumes a ceremony: each challenge is usable once.
func (s *Service) takeCeremony(id string, register bool) (ceremony, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.ceremonies[id]
	delete(s.ceremonies, id)
	if !ok || c.register != register || s.now().After(c.expires) {
		return ceremony{}, false
	}
	return c, true
}

func (s *Service) indexLocked(id string) int {
	return slices.IndexFunc(s.data.Passkeys, func(r record) bool { return r.id() == id })
}

func (s *Service) saveLocked() error {
	return fsutil.WriteJSONAtomic(s.path, s.data, 0o600)
}

// verificationError wraps a library error, keeping its detail for the logs.
func verificationError(err error) error {
	var perr *protocol.Error
	if errors.As(err, &perr) && perr.DevInfo != "" {
		return fmt.Errorf("%w: %s (%s)", ErrVerification, perr.Details, perr.DevInfo)
	}
	return fmt.Errorf("%w: %v", ErrVerification, err)
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 }) {
		return "", ErrInvalidName
	}
	return name, nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return b
}
