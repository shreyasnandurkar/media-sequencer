// Package events is an in-memory fan-out hub for Server-Sent Events.
//
// Why SSE and not WebSockets: everything we push is server -> client, and
// EventSource reconnects on its own. Why in-memory: this service runs as a
// single instance. Scaling to several instances would need Postgres
// LISTEN/NOTIFY or Redis pub/sub here instead (see README tradeoffs).
package events

import (
	"log/slog"
	"sync"
)

// Event is what clients receive. Data is already-encoded JSON.
type Event struct {
	Name string // SSE "event:" field, e.g. "window.updated"
	Data string // SSE "data:" field
}

type Hub struct {
	mu  sync.Mutex
	log *slog.Logger
	// subscribers maps a subscription id to its buffered channel. A map (not a
	// slice) makes Unsubscribe O(1) and avoids index-shuffling bugs.
	subscribers map[int64]chan Event
	nextID      int64
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{log: log, subscribers: map[int64]chan Event{}}
}

// Subscribe returns a channel of events plus the function to release it.
// The channel is buffered so a slow client cannot block the publisher.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	id := h.nextID
	h.nextID++
	ch := make(chan Event, 16)
	h.subscribers[id] = ch

	unsubscribe := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if c, ok := h.subscribers[id]; ok {
			delete(h.subscribers, id)
			close(c)
		}
	}
	return ch, unsubscribe
}

// Publish delivers an event to every subscriber.
//
// The `select` with a `default` is the standard Go non-blocking send: if a
// subscriber's buffer is full we drop the event for that client rather than
// stalling everyone. That is safe here because clients re-fetch /api/state on
// any event, so one missed nudge is corrected by the next one (and by the
// reconnect refresh).
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, ch := range h.subscribers {
		select {
		case ch <- e:
		default:
			h.log.Warn("sse subscriber is slow, dropping event", "subscriber", id, "event", e.Name)
		}
	}
}

func (h *Hub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subscribers)
}
