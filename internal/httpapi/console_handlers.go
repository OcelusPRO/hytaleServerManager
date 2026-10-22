package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"hytale-server-manager/internal/gameserver"
)

const sseHeartbeat = 20 * time.Second

func (a *api) consoleHistory(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseUint(r.URL.Query().Get("since"), 10, 64)
	writeJSON(w, http.StatusOK, a.deps.Console.Since(since))
}

// consoleStream pushes console lines as Server-Sent Events. Reconnecting
// clients resume from Last-Event-ID.
func (a *api) consoleStream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	since, _ := strconv.ParseUint(r.Header.Get("Last-Event-ID"), 10, 64)
	token := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		token = c.Value
	}

	// Subscribe before reading history so that no line is missed.
	lines, cancel := a.deps.Console.Subscribe()
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	last := since
	for _, l := range a.deps.Console.Since(since) {
		if err := writeEvent(w, l); err != nil {
			return
		}
		last = l.Seq
	}
	if rc.Flush() != nil {
		return
	}

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			// The session may have ended (logout, expiry) since the stream opened.
			if !a.deps.Sessions.Active(token) {
				return
			}
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case l := <-lines:
			if l.Seq <= last {
				continue
			}
			if err := writeEvent(w, l); err != nil {
				return
			}
			last = l.Seq
		}
		if rc.Flush() != nil {
			return
		}
	}
}

func writeEvent(w http.ResponseWriter, l gameserver.Line) error {
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", l.Seq, data)
	return err
}
