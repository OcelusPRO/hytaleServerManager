package httpapi

import (
	"net/http"
	"strconv"

	"hytale-server-manager/internal/metricsstream"
)

// metricsStream is the optional MetricsStream plugin integration.
type metricsStream interface {
	Overview() metricsstream.Overview
	Store() *metricsstream.Store
	Reconcile()
}

// routeMetricsStream registers the endpoints of the metrics stream.
func (a *api) routeMetricsStream(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/metrics-stream", a.getMetricsStream)
	mux.HandleFunc("GET /api/metrics-stream/events", a.getMetricsStreamEvents)
	mux.HandleFunc("GET /api/metrics-stream/dump", a.getMetricsStreamDump)
}

func (a *api) getMetricsStream(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.deps.MetricsStream.Overview())
}

// getMetricsStreamEvents returns the gameplay events newer than ?after=.
func (a *api) getMetricsStreamEvents(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	events, next := a.deps.MetricsStream.Store().EventsAfter(after, limit)
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "next": next})
}

// getMetricsStreamDump returns the full metrics tree the server dumps
// periodically (large: only fetched on demand).
func (a *api) getMetricsStreamDump(w http.ResponseWriter, _ *http.Request) {
	doc, timeMs := a.deps.MetricsStream.Store().Dump()
	if doc == nil {
		writeError(w, a.log, &httpError{status: http.StatusNotFound, message: "no dump received yet"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"timeMs": timeMs, "dump": doc})
}
