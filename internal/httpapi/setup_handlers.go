package httpapi

import (
	"context"
	"net/http"
	"slices"

	"hytale-server-manager/internal/settings"
)

// routeSetup registers the automatic installation endpoints.
func (a *api) routeSetup(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup", a.setupStatus)
	mux.HandleFunc("POST /api/setup/update", a.setupUpdate)
	mux.HandleFunc("POST /api/setup/version", a.setupVersion)
}

func (a *api) setupStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.deps.Provisioner.Status())
}

// setupUpdate installs the latest build now, stopping the server first and
// starting it again afterwards when it was running.
func (a *api) setupUpdate(w http.ResponseWriter, _ *http.Request) {
	a.respondAction(w, a.applyGameBuild())
}

// setupVersion freezes the game on a build (or follows the latest again
// with an empty version) and switches to it now. Accepted builds are the
// installed one, the stored ones (rollback) and the latest.
func (a *api) setupVersion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version string `json:"version"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	st := a.deps.Provisioner.Status()
	known := body.Version == "" || body.Version == st.GameVersion ||
		body.Version == st.LatestVersion || slices.Contains(st.StoredVersions, body.Version)
	if !known {
		writeError(w, a.log, errBadRequest("version inconnue : choisissez la version installée, une version stockée ou la dernière"))
		return
	}
	needsSwitch := body.Version != st.GameVersion
	if body.Version == "" {
		needsSwitch = st.LatestVersion != "" && st.LatestVersion != st.GameVersion
	}
	if _, err := a.deps.Settings.Update(func(s *settings.Settings) { s.GameVersionPin = body.Version }); err != nil {
		writeError(w, a.log, errBadRequest(err.Error()))
		return
	}
	if !needsSwitch {
		writeNoContent(w)
		return
	}
	a.respondAction(w, a.applyGameBuild())
}

// applyGameBuild stops the server if needed, asks the provisioner to bring
// the game to the wanted build and restarts the server afterwards.
func (a *api) applyGameBuild() error {
	wasRunning := a.deps.Controller.Running()
	if wasRunning {
		if err := a.deps.Controller.Stop(); err != nil {
			return err
		}
	}
	var after func()
	if wasRunning {
		after = func() {
			if err := a.deps.Controller.Start(context.Background()); err != nil {
				a.deps.Console.System("Redémarrage après changement de version impossible : " + err.Error())
			}
		}
	}
	return a.deps.Provisioner.RequestUpdate(after)
}
