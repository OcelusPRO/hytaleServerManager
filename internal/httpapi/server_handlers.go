package httpapi

import (
	"net/http"

	"hytale-server-manager/internal/gameserver"
	"hytale-server-manager/internal/lifecycle"
)

// stateSleeping is reported instead of "stopped" while the server is asleep.
const stateSleeping gameserver.State = "sleeping"

// routeServer registers process control and console endpoints.
func (a *api) routeServer(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/server/status", a.serverStatus)
	mux.HandleFunc("POST /api/server/start", a.serverStart)
	mux.HandleFunc("POST /api/server/stop", a.serverStop)
	mux.HandleFunc("POST /api/server/restart", a.serverRestart)
	mux.HandleFunc("POST /api/server/kill", a.serverKill)
	mux.HandleFunc("POST /api/server/command", a.serverCommand)
	mux.HandleFunc("GET /api/console/history", a.consoleHistory)
	mux.HandleFunc("GET /api/console/stream", a.consoleStream)
	mux.HandleFunc("GET /api/console/commands", a.consoleCommands)
	mux.HandleFunc("POST /api/console/commands/refresh", a.refreshConsoleCommands)
}

type serverStatusResponse struct {
	lifecycle.Status
	PlayerCount int `json:"playerCount"`
}

func (a *api) serverStatus(w http.ResponseWriter, _ *http.Request) {
	resp := serverStatusResponse{
		Status:      a.deps.Controller.Status(),
		PlayerCount: a.deps.Players.Count(),
	}
	if a.sleeping() && !a.deps.Controller.Running() {
		resp.State = stateSleeping
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *api) sleeping() bool { return a.deps.Sleep != nil && a.deps.Sleep.Sleeping() }

// cancelSleep ends the sleep mode: stopping a sleeping server means keeping
// it stopped. It reports whether there was nothing else to stop.
func (a *api) cancelSleep() bool {
	return a.deps.Sleep != nil && a.deps.Sleep.Cancel() && !a.deps.Controller.Running()
}

func (a *api) serverStart(w http.ResponseWriter, r *http.Request) {
	a.respondAction(w, a.deps.Controller.Start(r.Context()))
}

func (a *api) serverStop(w http.ResponseWriter, _ *http.Request) {
	if a.cancelSleep() {
		writeNoContent(w)
		return
	}
	a.respondAction(w, a.deps.Controller.Stop())
}

func (a *api) serverRestart(w http.ResponseWriter, r *http.Request) {
	a.respondAction(w, a.deps.Controller.Restart(r.Context()))
}

func (a *api) serverKill(w http.ResponseWriter, _ *http.Request) {
	if a.cancelSleep() {
		writeNoContent(w)
		return
	}
	a.respondAction(w, a.deps.Controller.Kill())
}

func (a *api) serverCommand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command string `json:"command"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	a.respondAction(w, a.deps.Controller.Send(body.Command))
}

// respondAction answers 204 on success or maps the error.
func (a *api) respondAction(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeNoContent(w)
}
