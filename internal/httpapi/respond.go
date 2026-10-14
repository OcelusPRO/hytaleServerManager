package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const maxJSONBody = 64 << 10

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func writeMessage(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// writeError maps err to an HTTP status. Unexpected errors are logged and
// reported with a generic message so internals do not leak.
func writeError(w http.ResponseWriter, logger *slog.Logger, err error) {
	status, public := classify(err)
	if status >= 500 {
		logger.Error("request failed", "error", err)
	}
	writeMessage(w, status, public)
}

// decodeJSON strictly decodes a small JSON body into v.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return errBadRequest("le corps doit être du JSON")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errBadRequest(fmt.Sprintf("JSON invalide: %v", err))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errBadRequest("un seul objet JSON est attendu")
	}
	return nil
}
