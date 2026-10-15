package passkey

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"testing"
)

// The tests drive the service like a browser would, with a software
// authenticator producing genuine WebAuthn responses: the verification
// itself is the library's.

// authenticator data flags (WebAuthn §6.1) and COSE algorithms.
const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
	flagAttested     = 0x40
	algES256         = -7
	algEdDSA         = -8
)

// ---- a tiny CBOR encoder ----

func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 1<<8:
		return []byte{major<<5 | 24, byte(n)}
	case n < 1<<16:
		return binary.BigEndian.AppendUint16([]byte{major<<5 | 25}, uint16(n))
	default:
		return binary.BigEndian.AppendUint32([]byte{major<<5 | 26}, uint32(n))
	}
}

func cbor(v any) []byte {
	switch x := v.(type) {
	case int:
		if x >= 0 {
			return cborHead(0, uint64(x))
		}
		return cborHead(1, uint64(-1-x))
	case []byte:
		return append(cborHead(2, uint64(len(x))), x...)
	case string:
		return append(cborHead(3, uint64(len(x))), x...)
	case map[any]any:
		keys := make([]any, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		// Canonical order: shorter encodings first, then bytewise.
		sort.Slice(keys, func(i, j int) bool {
			a, b := cbor(keys[i]), cbor(keys[j])
			if len(a) != len(b) {
				return len(a) < len(b)
			}
			return string(a) < string(b)
		})
		out := cborHead(5, uint64(len(x)))
		for _, k := range keys {
			out = append(out, cbor(k)...)
			out = append(out, cbor(x[k])...)
		}
		return out
	}
	panic("unsupported")
}

// ---- software authenticator ----

type authenticator struct {
	id        []byte
	ec        *ecdsa.PrivateKey
	ed        ed25519.PrivateKey
	signCount uint32
	flags     byte
}

func newAuthenticator(t *testing.T, edDSA bool) *authenticator {
	a := &authenticator{id: randomBytes(16), flags: flagUserPresent | flagUserVerified}
	if edDSA {
		_, a.ed, _ = ed25519.GenerateKey(rand.Reader)
	} else {
		var err error
		if a.ec, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func (a *authenticator) coseKey() []byte {
	if a.ed != nil {
		return cbor(map[any]any{1: 1, 3: algEdDSA, -1: 6, -2: []byte(a.ed.Public().(ed25519.PublicKey))})
	}
	pub, _ := a.ec.PublicKey.ECDH()
	raw := pub.Bytes() // 0x04 || X || Y
	return cbor(map[any]any{1: 2, 3: algES256, -1: 1, -2: raw[1:33], -3: raw[33:]})
}

func (a *authenticator) authData(rpID string, attested bool) []byte {
	hash := sha256.Sum256([]byte(rpID))
	flags := a.flags
	if attested {
		flags |= flagAttested
	}
	out := append(hash[:], flags)
	out = binary.BigEndian.AppendUint32(out, a.signCount)
	if attested {
		out = append(out, make([]byte, 16)...) // AAGUID
		out = binary.BigEndian.AppendUint16(out, uint16(len(a.id)))
		out = append(out, a.id...)
		out = append(out, a.coseKey()...)
	}
	return out
}

func clientData(kind, challenge, origin string) []byte {
	data, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return data
}

// publicKey returns the options as the browser receives them.
func publicKey(t *testing.T, opts Options) map[string]any {
	t.Helper()
	data, err := json.Marshal(opts.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func credentialJSON(t *testing.T, id []byte, response map[string]any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"id": b64.EncodeToString(id), "rawId": b64.EncodeToString(id), "type": "public-key",
		"response": response, "clientExtensionResults": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (a *authenticator) register(t *testing.T, opts Options, origin string) json.RawMessage {
	pk := publicKey(t, opts)
	rpID := pk["rp"].(map[string]any)["id"].(string)
	return credentialJSON(t, a.id, map[string]any{
		"clientDataJSON": b64.EncodeToString(clientData("webauthn.create", pk["challenge"].(string), origin)),
		"attestationObject": b64.EncodeToString(cbor(map[any]any{
			"fmt": "none", "attStmt": map[any]any{}, "authData": a.authData(rpID, true),
		})),
		"transports": []string{"internal"},
	})
}

func (a *authenticator) login(t *testing.T, opts Options, origin string, userHandle []byte) json.RawMessage {
	pk := publicKey(t, opts)
	a.signCount++
	authData := a.authData(pk["rpId"].(string), false)
	cd := clientData("webauthn.get", pk["challenge"].(string), origin)
	hash := sha256.Sum256(cd)
	signed := append(append([]byte(nil), authData...), hash[:]...)
	var sig []byte
	if a.ed != nil {
		sig = ed25519.Sign(a.ed, signed)
	} else {
		digest := sha256.Sum256(signed)
		sig, _ = ecdsa.SignASN1(rand.Reader, a.ec, digest[:])
	}
	return credentialJSON(t, a.id, map[string]any{
		"clientDataJSON":    b64.EncodeToString(cd),
		"authenticatorData": b64.EncodeToString(authData),
		"signature":         b64.EncodeToString(sig),
		"userHandle":        b64.EncodeToString(userHandle),
	})
}

// ---- tests ----

var panel = Origin{Scheme: "https", Host: "panel.example.com:8443"}

const panelOrigin = "https://panel.example.com:8443"

func open(t *testing.T) *Service {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "passkeys.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func registered(t *testing.T, s *Service, a *authenticator) {
	t.Helper()
	opts, err := s.BeginRegistration(panel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishRegistration(opts.Ceremony, "Test key", a.register(t, opts, panelOrigin)); err != nil {
		t.Fatal(err)
	}
}

func loggedIn(t *testing.T, s *Service, a *authenticator) error {
	t.Helper()
	opts, err := s.BeginLogin(panel)
	if err != nil {
		t.Fatal(err)
	}
	return s.FinishLogin(opts.Ceremony, a.login(t, opts, panelOrigin, s.data.UserHandle))
}

func TestRegisterAndLogin(t *testing.T) {
	for _, edDSA := range []bool{false, true} {
		s := open(t)
		a := newAuthenticator(t, edDSA)
		registered(t, s, a)
		if s.Count() != 1 || s.List()[0].Name != "Test key" {
			t.Fatalf("list %+v", s.List())
		}
		for range 2 {
			if err := loggedIn(t, s, a); err != nil {
				t.Fatalf("edDSA=%v: %v", edDSA, err)
			}
		}
		if s.List()[0].LastUsedAt.IsZero() || s.data.Passkeys[0].Credential.Authenticator.SignCount != 2 {
			t.Fatal("use not recorded")
		}
	}
}

func TestLoginRejections(t *testing.T) {
	s := open(t)
	a := newAuthenticator(t, false)
	registered(t, s, a)
	for range 3 { // stored counter above 1
		if err := loggedIn(t, s, a); err != nil {
			t.Fatal(err)
		}
	}

	cases := map[string]func(opts Options) json.RawMessage{
		"other origin": func(o Options) json.RawMessage { return a.login(t, o, "https://evil.example.com", s.data.UserHandle) },
		"wrong user":   func(o Options) json.RawMessage { return a.login(t, o, panelOrigin, []byte("someone")) },
		"bad signature": func(o Options) json.RawMessage {
			var m map[string]any
			json.Unmarshal(a.login(t, o, panelOrigin, s.data.UserHandle), &m)
			resp := m["response"].(map[string]any)
			sig, _ := b64.DecodeString(resp["signature"].(string))
			sig[len(sig)-1] ^= 1
			resp["signature"] = b64.EncodeToString(sig)
			data, _ := json.Marshal(m)
			return data
		},
		"no user verification": func(o Options) json.RawMessage {
			a.flags = flagUserPresent
			defer func() { a.flags = flagUserPresent | flagUserVerified }()
			return a.login(t, o, panelOrigin, s.data.UserHandle)
		},
		"cloned counter": func(o Options) json.RawMessage {
			a.signCount = 0 // sends 1, below the stored counter
			return a.login(t, o, panelOrigin, s.data.UserHandle)
		},
	}
	for name, forge := range cases {
		opts, _ := s.BeginLogin(panel)
		err := s.FinishLogin(opts.Ceremony, forge(opts))
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
		t.Logf("%s: %v", name, err)
	}
	a.signCount = 100

	// A challenge cannot be replayed.
	opts, _ := s.BeginLogin(panel)
	r := a.login(t, opts, panelOrigin, s.data.UserHandle)
	if err := s.FinishLogin(opts.Ceremony, r); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishLogin(opts.Ceremony, r); !errors.Is(err, ErrCeremony) {
		t.Fatalf("replay: %v", err)
	}
	// A registration ceremony cannot be used to sign in.
	reg, _ := s.BeginRegistration(panel)
	if err := s.FinishLogin(reg.Ceremony, r); !errors.Is(err, ErrCeremony) {
		t.Fatalf("cross ceremony: %v", err)
	}
}

func TestRegistrationRejections(t *testing.T) {
	s := open(t)
	a := newAuthenticator(t, false)
	opts, _ := s.BeginRegistration(panel)
	if _, err := s.FinishRegistration(opts.Ceremony, "k", a.register(t, opts, "http://panel.example.com:8443")); err == nil {
		t.Fatal("accepted a plain-http origin")
	}
	opts, _ = s.BeginRegistration(Origin{Scheme: "https", Host: "other.example.com"})
	if _, err := s.FinishRegistration(opts.Ceremony, "k", a.register(t, opts, panelOrigin)); err == nil {
		t.Fatal("accepted a credential for another relying party")
	}
	registered(t, s, a)
	opts, _ = s.BeginRegistration(panel)
	if _, err := s.FinishRegistration(opts.Ceremony, "k", a.register(t, opts, panelOrigin)); err == nil {
		t.Fatal("registered the same credential twice")
	}
	opts, _ = s.BeginRegistration(panel)
	if _, err := s.FinishRegistration(opts.Ceremony, " ", a.register(t, opts, panelOrigin)); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("blank name: %v", err)
	}
}

func TestPasswordDisableAndReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passkeys.json")
	s, _ := Open(path)
	if err := s.SetPasswordDisabled(true); !errors.Is(err, ErrNoPasskey) {
		t.Fatalf("disabled without passkey: %v", err)
	}
	a := newAuthenticator(t, false)
	registered(t, s, a)
	if err := s.SetPasswordDisabled(true); err != nil || !s.PasswordDisabled() {
		t.Fatalf("disable: %v", err)
	}
	if err := s.Delete(b64.EncodeToString(a.id)); !errors.Is(err, ErrLastPasskey) {
		t.Fatalf("deleted the last passkey: %v", err)
	}

	s, _ = Open(path) // persisted, and still usable
	if !s.PasswordDisabled() || s.Count() != 1 {
		t.Fatal("state not persisted")
	}
	if err := loggedIn(t, s, a); err != nil {
		t.Fatalf("login after reload: %v", err)
	}
	if err := s.Reset(); err != nil || s.PasswordDisabled() || s.Count() != 0 {
		t.Fatalf("reset: %v", err)
	}
}

func TestRelyingPartyID(t *testing.T) {
	for host, want := range map[string]string{
		"panel.example.com:8443": "panel.example.com",
		"Panel.Example.com":      "panel.example.com",
		"localhost:8080":         "localhost",
		"[::1]:8080":             "::1",
	} {
		if got := (Origin{Host: host}).rpID(); got != want {
			t.Errorf("%s: %s", host, got)
		}
	}
}
