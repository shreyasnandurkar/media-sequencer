package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/shreyasnandurkar/media-sequencer/backend/internal/model"
	"github.com/shreyasnandurkar/media-sequencer/backend/internal/scheduler"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleTime is the reference clock. Clients sample it a few times and keep the
// round trip with the smallest latency to estimate their offset (see
// frontend/src/lib/clock.ts).
func (s *Server) handleTime(w http.ResponseWriter, r *http.Request) {
	// No caching — a cached clock is worse than no clock.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]int64{"serverTimeMs": s.now()})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.State(r.Context(), s.cfg.CycleMs, s.now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleListMedia(w http.ResponseWriter, r *http.Request) {
	media, err := s.store.ListMedia(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, media)
}

type createMediaReq struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	URL        *string `json:"url"`
	DurationMs int64   `json:"durationMs"`
}

func (s *Server) handleCreateMedia(w http.ResponseWriter, r *http.Request) {
	var req createMediaReq
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		badRequest(w, "name is required")
		return
	}
	mt := model.MediaType(req.Type)
	if mt != model.MediaImage && mt != model.MediaVideo && mt != model.MediaBlank {
		badRequest(w, `type must be one of "image", "video", "blank"`)
		return
	}
	if req.DurationMs <= 0 {
		badRequest(w, "durationMs must be > 0")
		return
	}

	// The DB has a CHECK that ties url presence to type; validate up front so
	// the client gets a readable 400 rather than a constraint violation.
	if mt == model.MediaBlank {
		if req.URL != nil && *req.URL != "" {
			badRequest(w, "blank media must not have a url")
			return
		}
		req.URL = nil
	} else {
		if req.URL == nil || strings.TrimSpace(*req.URL) == "" {
			badRequest(w, "url is required for image and video media")
			return
		}
		trimmed := strings.TrimSpace(*req.URL)
		if err := validateMediaURL(trimmed); err != nil {
			badRequest(w, err.Error())
			return
		}
		req.URL = &trimmed
	}

	created, err := s.store.CreateMedia(r.Context(), model.Media{
		ID:         strings.TrimSpace(req.ID),
		Name:       req.Name,
		Type:       mt,
		URL:        req.URL,
		DurationMs: req.DurationMs,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}

	s.broadcast("media.created", `{"mediaId":"`+created.ID+`"}`)
	writeJSON(w, http.StatusCreated, created)
}

var errBadURL = errors.New("url must be an absolute http:// or https:// address")

func validateMediaURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errBadURL
	}
	return nil
}

func (s *Server) handleListWindows(w http.ResponseWriter, r *http.Request) {
	windows, err := s.store.ListWindows(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, windows)
}

type createWindowReq struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateWindow(w http.ResponseWriter, r *http.Request) {
	var req createWindowReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		badRequest(w, "name is required")
		return
	}

	// New windows join the existing cycle grid: use the same epoch alignment as
	// the seeded windows so every window flips at the same instant.
	epoch := scheduler.CycleStart(0, s.cfg.CycleMs, s.now())
	win, err := s.store.CreateWindow(r.Context(), req.Name, epoch)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	s.broadcast("window.updated", `{"windowId":"`+win.ID+`","version":1}`)
	writeJSON(w, http.StatusCreated, win)
}

// handleWindowNow exposes the server-side Resolve() result. It exists purely so
// an evaluator (or a confused developer) can compare what the backend thinks is
// playing with what the browser is showing.
func (s *Server) handleWindowNow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	win, err := s.store.GetWindow(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	now := s.now()
	res := scheduler.Resolve(win, win.Items, s.cfg.CycleMs, now)

	sync, err := s.store.ActiveSync(r.Context(), now)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"windowId":     win.ID,
		"serverTimeMs": now,
		"cycleMs":      s.cfg.CycleMs,
		"resolved":     res,
		"activeSync":   sync,
	})
}
