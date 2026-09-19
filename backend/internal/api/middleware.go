package api

import (
	"log/slog"
	"net/http"
	"time"
)

// withCORS allows the deployed frontend origin (and localhost in dev) to call
// the API from the browser. Written by hand: the rules are three lines and a
// dependency would hide them.
func withCORS(allowed []string, next http.Handler) http.Handler {
	allowAll := false
	set := map[string]bool{}
	for _, o := range allowed {
		if o == "*" {
			allowAll = true
		}
		set[o] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowAll || set[origin]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			// Responses differ by Origin, so caches must not share them.
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder remembers the status code so the log line can report it.
// http.ResponseWriter does not expose what was written, so we wrap it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.NewResponseController reach the real writer, so SSE
// flushing still works through this wrapper. (Go 1.20+ convention.)
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func withLogging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// The SSE stream is long-lived; logging it on completion is fine but
		// keep it at debug level so it does not dominate the log.
		log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"durationMs", time.Since(start).Milliseconds())
	})
}

// withRecover turns a panic in any handler into a 500 instead of killing the
// whole process (net/http would otherwise drop just that connection, but a
// consistent JSON error is nicer and the stack gets logged).
func withRecover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				log.Error("panic in handler", "path", r.URL.Path, "panic", p)
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
