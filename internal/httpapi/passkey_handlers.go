package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"hytale-server-manager/internal/passkey"
)

// routePasskeysPublic registers the sign-in endpoints usable without a session.
func (a *api) routePasskeysPublic(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/login/methods", a.loginMethods)
	mux.HandleFunc("POST /api/login/passkey/begin", a.passkeyLoginBegin)
	mux.HandleFunc("POST /api/login/passkey/finish", a.passkeyLoginFinish)
}

// routePasskeys registers the passkey management endpoints.
func (a *api) routePasskeys(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account/passkeys", a.listPasskeys)
	mux.HandleFunc("POST /api/account/passkeys/begin", a.passkeyRegisterBegin)
	mux.HandleFunc("POST /api/account/passkeys", a.passkeyRegisterFinish)
	mux.HandleFunc("PATCH /api/account/passkeys/{id}", a.renamePasskey)
	mux.HandleFunc("DELETE /api/account/passkeys/{id}", a.deletePasskey)
	mux.HandleFunc("PUT /api/account/password-login", a.setPasswordLogin)
}

// passkeyOrigin is the panel's address as the browser sees it.
func (a *api) passkeyOrigin(r *http.Request) passkey.Origin {
	scheme, host := "http", r.Host
	if r.TLS != nil {
		scheme = "https"
	}
	if a.deps.Config.TrustProxy {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" || proto == "http" {
			scheme = proto
		}
		if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
			host, _, _ = strings.Cut(fwd, ",")
			host = strings.TrimSpace(host)
		}
	}
	return passkey.Origin{Scheme: scheme, Host: host}
}

func (a *api) loginMethods(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"password": !a.deps.Passkeys.PasswordDisabled(),
		"passkey":  a.deps.Passkeys.Count() > 0,
	})
}

func (a *api) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	opts, err := a.deps.Passkeys.BeginLogin(a.passkeyOrigin(r))
	if err != nil {
		writeError(w, a.log, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (a *api) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r, a.deps.Config.TrustProxy)
	if ok, wait := a.deps.LoginLimiter.Allow(ip); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(int(math.Ceil(wait.Seconds()))))
		writeMessage(w, http.StatusTooManyRequests,
			fmt.Sprintf("too many attempts, try again in %d minute(s)", int(math.Ceil(wait.Minutes()))))
		return
	}
	var body ceremonyAnswer
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	if err := a.deps.Passkeys.FinishLogin(body.Ceremony, body.Credential); err != nil {
		a.deps.LoginLimiter.RecordFailure(ip)
		a.log.Warn("failed passkey login", "ip", ip, "error", err)
		writeMessage(w, http.StatusUnauthorized, publicPasskeyError(err).Error())
		return
	}
	a.deps.LoginLimiter.RecordSuccess(ip)
	a.startSession(w)
}

// startSession opens a panel session for a successful sign-in.
func (a *api) startSession(w http.ResponseWriter) {
	token, err := a.deps.Sessions.Create()
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	a.setSessionCookie(w, token, a.deps.Sessions.IdleTTL())
	writeNoContent(w)
}

// ceremonyAnswer is the browser's answer to a ceremony: the
// PublicKeyCredential it returned, in JSON.
type ceremonyAnswer struct {
	Ceremony   string          `json:"ceremony"`
	Name       string          `json:"name"` // registration only
	Credential json.RawMessage `json:"credential"`
}

// publicPasskeyError hides the verification details (logged) from the client.
func publicPasskeyError(err error) error {
	switch {
	case errors.Is(err, passkey.ErrCeremony), errors.Is(err, passkey.ErrDuplicate), errors.Is(err, passkey.ErrInvalidName), errors.Is(err, passkey.ErrTooMany):
		return err
	}
	return passkey.ErrVerification
}

type passkeysResponse struct {
	Passkeys         []passkey.Info `json:"passkeys"`
	PasswordDisabled bool           `json:"passwordDisabled"`
}

func (a *api) listPasskeys(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, passkeysResponse{
		Passkeys:         a.deps.Passkeys.List(),
		PasswordDisabled: a.deps.Passkeys.PasswordDisabled(),
	})
}

func (a *api) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	opts, err := a.deps.Passkeys.BeginRegistration(a.passkeyOrigin(r))
	if err != nil {
		writeError(w, a.log, a.passkeyError(err))
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (a *api) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	var body ceremonyAnswer
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	cred, err := a.deps.Passkeys.FinishRegistration(body.Ceremony, body.Name, body.Credential)
	if err != nil {
		a.log.Warn("passkey registration failed", "error", err)
		writeError(w, a.log, a.passkeyError(publicPasskeyError(err)))
		return
	}
	a.log.Info("passkey registered", "name", cred.Name)
	writeJSON(w, http.StatusCreated, cred)
}

func (a *api) renamePasskey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	a.respondAction(w, a.passkeyError(a.deps.Passkeys.Rename(r.PathValue("id"), body.Name)))
}

func (a *api) deletePasskey(w http.ResponseWriter, r *http.Request) {
	a.respondAction(w, a.passkeyError(a.deps.Passkeys.Delete(r.PathValue("id"))))
}

func (a *api) setPasswordLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	a.respondAction(w, a.passkeyError(a.deps.Passkeys.SetPasswordDisabled(!body.Enabled)))
}

// passkeyError maps the passkey errors the operator can act on to 4xx.
func (a *api) passkeyError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, passkey.ErrUnknown):
		return &httpError{status: http.StatusNotFound, message: err.Error()}
	case errors.Is(err, passkey.ErrLastPasskey), errors.Is(err, passkey.ErrNoPasskey), errors.Is(err, passkey.ErrTooMany), errors.Is(err, passkey.ErrDuplicate):
		return errConflict(err.Error())
	case errors.Is(err, passkey.ErrCeremony), errors.Is(err, passkey.ErrVerification), errors.Is(err, passkey.ErrInvalidName):
		return errBadRequest(err.Error())
	}
	return err
}
