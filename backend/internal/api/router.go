// Package api wires HTTP routes to the store, the scheduler and the SSE hub.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/shreyasnandurkar/media-sequencer/backend/internal/config"
	"github.com/shreyasnandurkar/media-sequencer/backend/internal/events"
	"github.com/shreyasnandurkar/media-sequencer/backend/internal/store"
)

type Server struct {
	cfg   config.Config
	store *store.Store
	hub   *events.Hub
	log   *slog.Logger
	// now is injectable so handler tests can pin the clock.
	now func() int64
}

func NewServer(cfg config.Config, st *store.Store, hub *events.Hub, log *slog.Logger) *Server {
	return &Server{
		cfg:   cfg,
		store: st,
		hub:   hub,
		log:   log,
		now:   func() int64 { return time.Now().UnixMilli() },
	}
}

// Handler builds the route table. Go 1.22's net/http can match methods and
// path wildcards ("GET /api/windows/{id}"), so no router library is needed.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/time", s.handleTime)
	mux.HandleFunc("GET /api/state", s.handleState)

	mux.HandleFunc("GET /api/media", s.handleListMedia)
	mux.HandleFunc("POST /api/media", s.handleCreateMedia)

	mux.HandleFunc("GET /api/windows", s.handleListWindows)
	mux.HandleFunc("POST /api/windows", s.handleCreateWindow)
	mux.HandleFunc("GET /api/windows/{id}/now", s.handleWindowNow)
	mux.HandleFunc("POST /api/windows/{id}/items", s.handleAddItem)
	mux.HandleFunc("DELETE /api/windows/{id}/items/{itemId}", s.handleDeleteItem)
	mux.HandleFunc("PUT /api/windows/{id}/items/order", s.handleReorderItems)

	mux.HandleFunc("POST /api/sync", s.handleStartSync)
	mux.HandleFunc("GET /api/sync/active", s.handleGetSync)
	mux.HandleFunc("DELETE /api/sync/active", s.handleCancelSync)

	mux.HandleFunc("GET /api/events", s.handleEvents)

	// Outermost first: recover -> log -> CORS -> routes.
	return withRecover(s.log, withLogging(s.log, withCORS(s.cfg.AllowedOrigins, mux)))
}

// broadcast is called after every mutation so open clients re-fetch state.
func (s *Server) broadcast(name, data string) {
	s.hub.Publish(events.Event{Name: name, Data: data})
}
