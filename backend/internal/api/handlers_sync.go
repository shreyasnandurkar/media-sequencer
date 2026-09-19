package api

import (
	"fmt"
	"net/http"
	"strings"
)

type startSyncReq struct {
	MediaID    string `json:"mediaId"`
	DurationMs *int64 `json:"durationMs"`
}

func (s *Server) handleStartSync(w http.ResponseWriter, r *http.Request) {
	var req startSyncReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.MediaID = strings.TrimSpace(req.MediaID)
	if req.MediaID == "" {
		badRequest(w, "mediaId is required")
		return
	}

	media, err := s.store.GetMedia(r.Context(), req.MediaID)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	duration := media.DurationMs
	if req.DurationMs != nil {
		duration = *req.DurationMs
	}
	if duration < s.cfg.MinSyncMs || duration > s.cfg.MaxSyncMs {
		badRequest(w, fmt.Sprintf("durationMs must be between %d and %d", s.cfg.MinSyncMs, s.cfg.MaxSyncMs))
		return
	}

	now := s.now()
	startAt := now + s.cfg.SyncLeadMs
	sy, err := s.store.StartSync(r.Context(), req.MediaID, startAt, startAt+duration, now)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	s.broadcast("sync.started", fmt.Sprintf(`{"syncId":%d,"mediaId":%q,"startAt":%d,"endAt":%d}`,
		sy.ID, sy.MediaID, sy.StartAt, sy.EndAt))
	writeJSON(w, http.StatusCreated, sy)
}

func (s *Server) handleGetSync(w http.ResponseWriter, r *http.Request) {
	sy, err := s.store.ActiveSync(r.Context(), s.now())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, sy)
}

func (s *Server) handleCancelSync(w http.ResponseWriter, r *http.Request) {
	cancelled, err := s.store.CancelActiveSync(r.Context(), s.now())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if cancelled {
		s.broadcast("sync.cancelled", `{}`)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": cancelled})
}
