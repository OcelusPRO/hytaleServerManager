package httpapi

import (
	"net/http"
	"os"

	"hytale-server-manager/internal/archive"
	"hytale-server-manager/internal/saves"
	"hytale-server-manager/internal/settings"
)

const errSaveInUse = "this save is used by the running server, stop it first"

// routeSaves registers world save, mod and backup endpoints.
func (a *api) routeSaves(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/saves", a.listSaves)
	mux.HandleFunc("POST /api/saves", a.createSave)
	mux.HandleFunc("POST /api/saves/import", a.importSave)
	mux.HandleFunc("PATCH /api/saves/{id}", a.renameSave)
	mux.HandleFunc("DELETE /api/saves/{id}", a.deleteSave)
	mux.HandleFunc("POST /api/saves/{id}/activate", a.activateSave)
	a.routeMods(mux)
	a.routeBackups(mux)
	a.routeConfigs(mux)
}

type savesResponse struct {
	Saves        []saves.Save `json:"saves"`
	ActiveSaveID string       `json:"activeSaveId"`
	RunningSave  string       `json:"runningSaveId,omitempty"`
}

func (a *api) listSaves(w http.ResponseWriter, _ *http.Request) {
	list, err := a.deps.Saves.List()
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	resp := savesResponse{Saves: list, ActiveSaveID: a.deps.Settings.Get().ActiveSaveID}
	if a.deps.Controller.Running() {
		resp.RunningSave = a.deps.Controller.Status().SaveID
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *api) createSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	save, err := a.deps.Saves.Create(body.Name)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	a.activateIfNone(save.ID)
	writeJSON(w, http.StatusCreated, save)
}

func (a *api) importSave(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	tmp, err := saveUploadToTemp(w, r, a.deps.TmpDir, a.deps.Config.MaxUploadBytes)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	defer os.Remove(tmp)

	save, err := a.deps.Saves.Import(name, tmp, a.extractLimits())
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	a.activateIfNone(save.ID)
	writeJSON(w, http.StatusCreated, save)
}

func (a *api) renameSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	save, err := a.deps.Saves.Rename(r.PathValue("id"), body.Name)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusOK, save)
}

func (a *api) deleteSave(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if a.deps.Controller.UsesSave(id) {
		writeError(w, a.log, errConflict(errSaveInUse))
		return
	}
	if err := a.deps.Saves.Delete(id); err != nil {
		writeError(w, a.log, err)
		return
	}
	if err := a.deps.Backups.DeleteAll(id); err != nil {
		a.log.Warn("could not delete backups of removed save", "save", id, "error", err)
	}
	if a.deps.Settings.Get().ActiveSaveID == id {
		if _, err := a.deps.Settings.Update(func(s *settings.Settings) { s.ActiveSaveID = "" }); err != nil {
			writeError(w, a.log, err)
			return
		}
	}
	writeNoContent(w)
}

func (a *api) activateSave(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.deps.Saves.Dir(id); err != nil {
		writeError(w, a.log, err)
		return
	}
	_, err := a.deps.Settings.Update(func(s *settings.Settings) { s.ActiveSaveID = id })
	a.respondAction(w, err)
}

// activateIfNone selects the save when no save is active yet.
func (a *api) activateIfNone(id string) {
	if a.deps.Settings.Get().ActiveSaveID != "" {
		return
	}
	if _, err := a.deps.Settings.Update(func(s *settings.Settings) { s.ActiveSaveID = id }); err != nil {
		a.log.Warn("could not activate save", "save", id, "error", err)
	}
}

// extractLimits bounds archives uploaded by the operator.
func (a *api) extractLimits() archive.Limits {
	return archive.Limits{MaxFiles: 500_000, MaxTotalBytes: a.deps.Config.MaxUploadBytes * 20}
}
