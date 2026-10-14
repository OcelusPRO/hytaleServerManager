package httpapi

import (
	"context"
	"net/http"

	"hytale-server-manager/internal/gameserver"
)

// consoleCommands returns the command catalog used for autocompletion: the
// one of the running save, or of the active save when stopped.
func (a *api) consoleCommands(w http.ResponseWriter, _ *http.Request) {
	saveID := a.deps.Settings.Get().ActiveSaveID
	if st := a.deps.Controller.Status(); a.deps.Controller.Running() {
		saveID = st.SaveID
	}
	writeJSON(w, http.StatusOK, a.deps.Commands.Get(saveID))
}

// refreshConsoleCommands rebuilds the catalog from the running server.
func (a *api) refreshConsoleCommands(w http.ResponseWriter, _ *http.Request) {
	if !a.deps.Controller.Running() {
		writeError(w, a.log, gameserver.ErrNotRunning)
		return
	}
	go func() {
		if err := a.deps.Commands.Refresh(context.Background(), true); err != nil {
			a.log.Warn("command catalog refresh failed", "error", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}
