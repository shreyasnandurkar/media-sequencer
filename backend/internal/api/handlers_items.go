package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type addItemReq struct {
	MediaID    string `json:"mediaId"`
	Position   *int   `json:"position"`
	DurationMs *int64 `json:"durationMs"`
}

// handleAddItem is the "add media to a window's list at runtime" requirement.
// The store re-anchors inside the same transaction so nothing on screen jumps.
func (s *Server) handleAddItem(w http.ResponseWriter, r *http.Request) {
	windowID := r.PathValue("id")

	var req addItemReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.MediaID = strings.TrimSpace(req.MediaID)
	if req.MediaID == "" {
		badRequest(w, "mediaId is required")
		return
	}
	if req.Position != nil && *req.Position < 0 {
		badRequest(w, "position must be >= 0")
		return
	}
	if req.DurationMs != nil && *req.DurationMs <= 0 {
		badRequest(w, "durationMs must be > 0 when provided")
		return
	}

	win, err := s.store.AddItem(r.Context(), windowID, req.MediaID, req.Position, req.DurationMs, s.cfg.CycleMs, s.now())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	s.broadcastWindow(win.ID, win.Version)
	writeJSON(w, http.StatusCreated, win)
}

func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	windowID := r.PathValue("id")
	itemID, err := strconv.ParseInt(r.PathValue("itemId"), 10, 64)
	if err != nil {
		badRequest(w, "itemId must be an integer")
		return
	}

	win, err := s.store.DeleteItem(r.Context(), windowID, itemID, s.cfg.CycleMs, s.now())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	s.broadcastWindow(win.ID, win.Version)
	writeJSON(w, http.StatusOK, win)
}

type reorderReq struct {
	ItemIDs []int64 `json:"itemIds"`
}

func (s *Server) handleReorderItems(w http.ResponseWriter, r *http.Request) {
	windowID := r.PathValue("id")

	var req reorderReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.ItemIDs) == 0 {
		badRequest(w, "itemIds must be a non-empty array")
		return
	}

	win, err := s.store.SetOrder(r.Context(), windowID, req.ItemIDs, s.cfg.CycleMs, s.now())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	s.broadcastWindow(win.ID, win.Version)
	writeJSON(w, http.StatusOK, win)
}

func (s *Server) broadcastWindow(id string, version int64) {
	s.broadcast("window.updated", fmt.Sprintf(`{"windowId":%q,"version":%d}`, id, version))
}
