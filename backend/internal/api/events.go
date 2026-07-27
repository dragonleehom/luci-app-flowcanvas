package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type streamEvent struct {
	ID   uint64
	Name string
	Data []byte
}

type EventHub struct {
	mu          sync.RWMutex
	subscribers map[uint64]chan streamEvent
	nextID      atomic.Uint64
	nextEventID atomic.Uint64
}

func NewEventHub() *EventHub {
	return &EventHub{subscribers: make(map[uint64]chan streamEvent)}
}

func (h *EventHub) Publish(name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	event := streamEvent{ID: h.nextEventID.Add(1), Name: name, Data: data}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, subscriber := range h.subscribers {
		select {
		case subscriber <- event:
		default:
			// A slow UI must resynchronize rather than block the watcher or SQLite writer.
			select {
			case <-subscriber:
			default:
			}
			resyncPayload := []byte(`{"resync":true}`)
			select {
			case subscriber <- streamEvent{ID: h.nextEventID.Add(1), Name: "resync", Data: resyncPayload}:
			default:
			}
		}
	}
}

func (h *EventHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	subscriberID, queue := h.subscribe()
	defer h.unsubscribe(subscriberID)
	if _, err := fmt.Fprint(w, "event: ready\ndata: {\"ready\":true}\n\n"); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event := <-queue:
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Name, event.Data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *EventHub) subscribe() (uint64, chan streamEvent) {
	id := h.nextID.Add(1)
	queue := make(chan streamEvent, 256)
	h.mu.Lock()
	h.subscribers[id] = queue
	h.mu.Unlock()
	return id, queue
}

func (h *EventHub) unsubscribe(id uint64) {
	h.mu.Lock()
	delete(h.subscribers, id)
	h.mu.Unlock()
}
