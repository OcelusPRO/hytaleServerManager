package httpapi

import (
	"fmt"
	"net/http"
	"os"
)

func (a *api) routeBackups(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/saves/{id}/backups", a.listBackups)
	mux.HandleFunc("POST /api/saves/{id}/backups", a.createBackup)
	mux.HandleFunc("GET /api/saves/{id}/backups/{backup}/download", a.downloadBackup)
	mux.HandleFunc("POST /api/saves/{id}/backups/{backup}/restore", a.restoreBackup)
	mux.HandleFunc("DELETE /api/saves/{id}/backups/{backup}", a.deleteBackup)
}

func (a *api) listBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := a.deps.Backups.List(r.PathValue("id"))
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusOK, backups)
}

func (a *api) createBackup(w http.ResponseWriter, r *http.Request) {
	backup, err := a.deps.Backups.Create(r.PathValue("id"))
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, backup)
}

func (a *api) downloadBackup(w http.ResponseWriter, r *http.Request) {
	saveID, backupID := r.PathValue("id"), r.PathValue("backup")
	path, err := a.deps.Backups.Path(saveID, backupID)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	// IDs are validated by the store, so the file name is safe to echo.
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="hytale-%s-%s.zip"`, saveID, backupID))
	w.Header().Set("Content-Type", "application/zip")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

func (a *api) restoreBackup(w http.ResponseWriter, r *http.Request) {
	saveID, backupID := r.PathValue("id"), r.PathValue("backup")
	if a.deps.Controller.UsesSave(saveID) {
		writeError(w, a.log, errConflict(errSaveInUse))
		return
	}
	if _, err := a.deps.Backups.Path(saveID, backupID); err != nil {
		writeError(w, a.log, err)
		return
	}
	if a.deps.Settings.Get().BackupBeforeRestore {
		if _, err := a.deps.Backups.Create(saveID); err != nil {
			writeError(w, a.log, fmt.Errorf("safety backup before restore: %w", err))
			return
		}
	}
	a.respondAction(w, a.deps.Backups.Restore(saveID, backupID))
}

func (a *api) deleteBackup(w http.ResponseWriter, r *http.Request) {
	a.respondAction(w, a.deps.Backups.Delete(r.PathValue("id"), r.PathValue("backup")))
}
