package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/shreyasnandurkar/media-sequencer/backend/internal/store"
)

// Every error response has the same shape:
//
//	{ "error": { "code": "...", "message": "..." } }
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func badRequest(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, "bad_request", message)
}

func notFound(w http.ResponseWriter, message string) {
	writeError(w, http.StatusNotFound, "not_found", message)
}

// writeStoreError maps the store's sentinel errors onto status codes so
// handlers do not each repeat the same switch.
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		notFound(w, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	default:
		slog.Error("internal error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}

// decodeJSON reads a request body into dst, rejecting unknown fields so typos
// in a client payload surface as a 400 instead of being silently ignored.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}
