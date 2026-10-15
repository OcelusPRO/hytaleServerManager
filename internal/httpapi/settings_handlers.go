package httpapi

import (
	"net/http"

	"hytale-server-manager/internal/settings"
)

// routeSettings registers the server settings endpoints.
func (a *api) routeSettings(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", a.getSettings)
	mux.HandleFunc("PUT /api/settings", a.putSettings)
}

func (a *api) getSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.deps.Settings.Get())
}

func (a *api) putSettings(w http.ResponseWriter, r *http.Request) {
	var body settings.Settings
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	previous := a.deps.Settings.Get()
	updated, err := a.deps.Settings.Update(func(s *settings.Settings) {
		active, pin := s.ActiveSaveID, s.GameVersionPin // changed through /saves and /setup/version
		*s = body
		s.ActiveSaveID, s.GameVersionPin = active, pin
	})
	if err != nil {
		writeError(w, a.log, errBadRequest(err.Error()))
		return
	}
	if a.deps.MetricsStream != nil {
		a.deps.MetricsStream.Reconcile()
	}
	if updated.Patchline != previous.Patchline && a.deps.Provisioner != nil {
		a.deps.Provisioner.Wake() // fetch the build of the new patchline now
	}
	writeJSON(w, http.StatusOK, updated)
}
