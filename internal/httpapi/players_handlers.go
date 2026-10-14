package httpapi

import (
	"net/http"

	"hytale-server-manager/internal/players"
)

// routePlayers registers player management endpoints.
func (a *api) routePlayers(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/players", a.listPlayers)
	mux.HandleFunc("POST /api/players/action", a.playerAction)
	mux.HandleFunc("POST /api/players/remove", a.playerRemove)
	mux.HandleFunc("PUT /api/players/whitelist", a.setWhitelist)
}

type playersResponse struct {
	// Live is true when the server runs the active save: actions then go
	// through the console, otherwise only removals are possible.
	Live   bool                   `json:"live"`
	Online []players.OnlinePlayer `json:"online"`
	players.Roster
}

func (a *api) listPlayers(w http.ResponseWriter, _ *http.Request) {
	roster, err := a.deps.PlayerAccess.Roster()
	if err != nil && err != players.ErrNoActiveSave {
		writeError(w, a.log, err)
		return
	}
	if roster.Bans == nil {
		roster = players.Roster{Whitelist: players.Whitelist{Players: []players.PlayerRef{}}, Bans: []players.Ban{}, Operators: []players.PlayerRef{}}
	}
	writeJSON(w, http.StatusOK, playersResponse{
		Live:   a.deps.PlayerAccess.Live(),
		Online: a.deps.Players.Online(),
		Roster: roster,
	})
}

func (a *api) playerAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		Player string `json:"player"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	a.respondAction(w, a.deps.PlayerAccess.Act(players.Action(body.Action), body.Player, body.Reason))
}

func (a *api) playerRemove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		List string `json:"list"`
		UUID string `json:"uuid"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	a.respondAction(w, a.deps.PlayerAccess.Remove(players.List(body.List), body.UUID))
}

func (a *api) setWhitelist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	a.respondAction(w, a.deps.PlayerAccess.SetWhitelist(body.Enabled))
}
