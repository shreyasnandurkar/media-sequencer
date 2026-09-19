package api

import (
	"fmt"
	"net/http"
	"time"
)

const heartbeatInterval = 20 * time.Second

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	w.Header().Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.log.Debug("could not clear write deadline for SSE", "err", err)
	}

	sub, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	fmt.Fprintf(w, "retry: 2000\n\n")
	fmt.Fprintf(w, "event: hello\ndata: {\"serverTimeMs\":%d}\n\n", s.now())
	_ = rc.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return

		case e, ok := <-sub:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name, e.Data); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}

		case <-ticker.C:

			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}
