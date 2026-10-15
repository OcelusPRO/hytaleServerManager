package httpapi

import (
	"net/http"
	"time"
)

// metricRanges are the history spans the interface can ask for.
var metricRanges = map[string]time.Duration{
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"14d": 14 * 24 * time.Hour,
}

// routeMetrics registers the resource history endpoint.
func (a *api) routeMetrics(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/metrics", a.getMetrics)
}

func (a *api) getMetrics(w http.ResponseWriter, r *http.Request) {
	if a.deps.Metrics == nil {
		writeError(w, a.log, &httpError{status: http.StatusServiceUnavailable, message: "metrics unavailable"})
		return
	}
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "24h"
	}
	span, ok := metricRanges[name]
	if !ok {
		writeError(w, a.log, errBadRequest("unknown range"))
		return
	}
	writeJSON(w, http.StatusOK, a.deps.Metrics.Series(span))
}
