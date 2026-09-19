package api

import (
	"fmt"
	"net/http"
	"time"
)

// heartbeatInterval keeps the connection alive through proxies that drop idle
// streams (Render, Cloudflare and friends usually cut at 30-60s).
const heartbeatInterval = 20 * time.Second

// handleEvents is the SSE stream. It holds the request open and writes a line
// whenever something changes; the browser's EventSource reconnects by itself if
// it drops.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable proxy buffering (nginx honours this); without it events can sit
	// in a buffer for seconds, which would defeat the point.
	w.Header().Set("X-Accel-Buffering", "no")

	// ResponseController handles our middleware wrapper for us (it follows
	// Unwrap), and lets us clear the write deadline so the stream can stay open
	// longer than the server's WriteTimeout.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.log.Debug("could not clear write deadline for SSE", "err", err)
	}

	sub, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	// Tell the client how long to wait before reconnecting, and give it the
	// current server time so it can start scheduling immediately.
	fmt.Fprintf(w, "retry: 2000\n\n")
	fmt.Fprintf(w, "event: hello\ndata: {\"serverTimeMs\":%d}\n\n", s.now())
	_ = rc.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	// r.Context() is cancelled when the client disconnects — that is how a
	// long-lived handler in Go knows to stop and release its subscription.
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
			// A comment line: valid SSE, ignored by EventSource, keeps the
			// socket warm.
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}
