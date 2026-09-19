package events

import (
	"log/slog"
	"sync"
)

type Event struct {
	Name string
	Data string
}

type Hub struct {
	mu  sync.Mutex
	log *slog.Logger

	subscribers map[int64]chan Event
	nextID      int64
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{log: log, subscribers: map[int64]chan Event{}}
}

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
