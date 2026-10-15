package httpapi

import (
	"fmt"
	"math"
	"net/http"

	"hytale-server-manager/internal/passkey"
)

// routeAccount registers the panel account endpoints.
func (a *api) routeAccount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/logout", a.logout)
	mux.HandleFunc("POST /api/account/password", a.changePassword)
}

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r, a.deps.Config.TrustProxy)
	if ok, wait := a.deps.LoginLimiter.Allow(ip); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(int(math.Ceil(wait.Seconds()))))
		writeMessage(w, http.StatusTooManyRequests,
			fmt.Sprintf("too many attempts, try again in %d minute(s)", int(math.Ceil(wait.Minutes()))))
		return
	}
	if a.deps.Passkeys != nil && a.deps.Passkeys.PasswordDisabled() {
		writeMessage(w, http.StatusForbidden, passkey.ErrPasswordDisabled.Error())
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	if !a.deps.Credentials.Verify(body.Password) {
		a.deps.LoginLimiter.RecordFailure(ip)
		a.log.Warn("failed panel login", "ip", ip)
		writeMessage(w, http.StatusUnauthorized, "wrong password")
		return
	}
	a.deps.LoginLimiter.RecordSuccess(ip)
	a.startSession(w)
}

func (a *api) session(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": a.authenticated(r)})
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.deps.Sessions.Revoke(c.Value)
	}
	a.setSessionCookie(w, "", -1)
	writeNoContent(w)
}

func (a *api) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	if err := a.deps.Credentials.Change(body.Current, body.Next); err != nil {
		writeError(w, a.log, err)
		return
	}
	if err := a.deps.PasswordNotice.Clear(); err != nil {
		a.log.Warn("could not remove initial password file", "error", err)
	}
	// Every other session must authenticate again with the new password.
	a.deps.Sessions.RevokeAll()
	token, err := a.deps.Sessions.Create()
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	a.setSessionCookie(w, token, a.deps.Sessions.IdleTTL())
	writeNoContent(w)
}
