package httpapi

import (
	"net/http"

	"hytale-server-manager/internal/serverconfig"
)

func (a *api) routeConfigs(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/saves/{id}/configs", a.listConfigs)
	mux.HandleFunc("GET /api/saves/{id}/configs/file", a.readConfig)
	mux.HandleFunc("PATCH /api/saves/{id}/configs/file", a.updateConfig)
}

func (a *api) listConfigs(w http.ResponseWriter, r *http.Request) {
	dir, err := a.deps.Saves.Dir(r.PathValue("id"))
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	files, err := serverconfig.List(dir)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	if files == nil {
		files = []serverconfig.File{}
	}
	writeJSON(w, http.StatusOK, files)
}

func (a *api) readConfig(w http.ResponseWriter, r *http.Request) {
	dir, err := a.deps.Saves.Dir(r.PathValue("id"))
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	doc, err := serverconfig.Read(dir, r.URL.Query().Get("file"))
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(doc)
}

func (a *api) updateConfig(w http.ResponseWriter, r *http.Request) {
	saveID := r.PathValue("id")
	if a.deps.Controller.UsesSave(saveID) {
		writeError(w, a.log, errConflict("le serveur utilise ce monde et réécrit ses fichiers de configuration : arrêtez-le avant de modifier la configuration"))
		return
	}
	dir, err := a.deps.Saves.Dir(saveID)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	var body struct {
		Changes []serverconfig.Change `json:"changes"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	a.respondAction(w, serverconfig.Apply(dir, r.URL.Query().Get("file"), body.Changes))
}
