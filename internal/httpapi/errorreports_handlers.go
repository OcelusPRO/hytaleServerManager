package httpapi

import (
	"net/http"

	"hytale-server-manager/internal/errorreports"
)

// errorReports is the store of the server's error reports.
type errorReports interface {
	Overview() errorreports.Overview
	Store() *errorreports.Store
}

var errUnknownIssue = &httpError{status: http.StatusNotFound, message: "unknown error report"}

// routeErrorReports registers the endpoints of the error reports.
func (a *api) routeErrorReports(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/errors", a.getErrorReports)
	mux.HandleFunc("DELETE /api/errors", a.clearErrorReports)
	mux.HandleFunc("GET /api/errors/{id}", a.getErrorReport)
	mux.HandleFunc("PATCH /api/errors/{id}", a.patchErrorReport)
	mux.HandleFunc("DELETE /api/errors/{id}", a.deleteErrorReport)
}

func (a *api) getErrorReports(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.deps.ErrorReports.Overview())
}

func (a *api) getErrorReport(w http.ResponseWriter, r *http.Request) {
	issue, ok, err := a.deps.ErrorReports.Store().Get(r.PathValue("id"))
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	if !ok {
		writeError(w, a.log, errUnknownIssue)
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

func (a *api) patchErrorReport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Resolved bool `json:"resolved"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	ok, err := a.deps.ErrorReports.Store().SetResolved(r.PathValue("id"), body.Resolved)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	if !ok {
		writeError(w, a.log, errUnknownIssue)
		return
	}
	writeNoContent(w)
}

func (a *api) deleteErrorReport(w http.ResponseWriter, r *http.Request) {
	ok, err := a.deps.ErrorReports.Store().Delete(r.PathValue("id"))
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	if !ok {
		writeError(w, a.log, errUnknownIssue)
		return
	}
	writeNoContent(w)
}

func (a *api) clearErrorReports(w http.ResponseWriter, _ *http.Request) {
	if err := a.deps.ErrorReports.Store().Clear(); err != nil {
		writeError(w, a.log, err)
		return
	}
	writeNoContent(w)
}
