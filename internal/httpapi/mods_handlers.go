package httpapi

import (
	"io"
	"net/http"
)

func (a *api) routeMods(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/saves/{id}/mods", a.listMods)
	mux.HandleFunc("POST /api/saves/{id}/mods", a.uploadMod)
	mux.HandleFunc("PATCH /api/saves/{id}/mods/{file}", a.toggleMod)
	mux.HandleFunc("DELETE /api/saves/{id}/mods/{file}", a.deleteMod)
}

func (a *api) listMods(w http.ResponseWriter, r *http.Request) {
	mods, err := a.deps.Mods.List(r.PathValue("id"))
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusOK, mods)
}

func (a *api) uploadMod(w http.ResponseWriter, r *http.Request) {
	saveID := r.PathValue("id")
	limit := a.deps.Config.MaxUploadBytes
	err := withUploadedFile(w, r, limit, func(name string, body io.Reader) error {
		mod, err := a.deps.Mods.Add(saveID, name, body, limit)
		if err == nil {
			writeJSON(w, http.StatusCreated, mod)
		}
		return err
	})
	if err != nil {
		writeError(w, a.log, err)
	}
}

func (a *api) toggleMod(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	a.respondAction(w, a.deps.Mods.SetEnabled(r.PathValue("id"), r.PathValue("file"), body.Enabled))
}

func (a *api) deleteMod(w http.ResponseWriter, r *http.Request) {
	a.respondAction(w, a.deps.Mods.Remove(r.PathValue("id"), r.PathValue("file")))
}
