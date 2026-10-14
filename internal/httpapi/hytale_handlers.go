package httpapi

import (
	"net/http"
)

// routeHytale registers the Hytale account endpoints.
func (a *api) routeHytale(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hytale/auth", a.hytaleStatus)
	mux.HandleFunc("POST /api/hytale/auth/login", a.hytaleLogin)
	mux.HandleFunc("POST /api/hytale/auth/cancel", a.hytaleCancel)
	mux.HandleFunc("POST /api/hytale/auth/logout", a.hytaleLogout)
	mux.HandleFunc("GET /api/hytale/auth/profiles", a.hytaleProfiles)
	mux.HandleFunc("POST /api/hytale/auth/profile", a.hytaleSelectProfile)
}

func (a *api) hytaleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.deps.HytaleAuth.Status())
}

func (a *api) hytaleLogin(w http.ResponseWriter, r *http.Request) {
	status, err := a.deps.HytaleAuth.StartLogin(r.Context())
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *api) hytaleCancel(w http.ResponseWriter, _ *http.Request) {
	a.deps.HytaleAuth.CancelLogin()
	writeNoContent(w)
}

func (a *api) hytaleLogout(w http.ResponseWriter, _ *http.Request) {
	a.respondAction(w, a.deps.HytaleAuth.Logout())
}

func (a *api) hytaleProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := a.deps.HytaleAuth.Profiles(r.Context())
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusOK, profiles)
}

func (a *api) hytaleSelectProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UUID string `json:"uuid"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	a.respondAction(w, a.deps.HytaleAuth.SelectProfile(r.Context(), body.UUID))
}
