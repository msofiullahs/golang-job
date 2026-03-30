package dashboard

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
)

// Event represents a server-sent event.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// SSEHub manages Server-Sent Events connections.
type SSEHub struct {
	mu      sync.RWMutex
	clients map[chan Event]struct{}
	logger  *slog.Logger
}

// NewSSEHub creates a new SSE hub.
func NewSSEHub(logger *slog.Logger) *SSEHub {
	return &SSEHub{
		clients: make(map[chan Event]struct{}),
		logger:  logger.With("component", "sse"),
	}
}

// Broadcast sends an event to all connected clients.
func (h *SSEHub) Broadcast(event Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.clients {
		select {
		case ch <- event:
		default:
			// Client is slow, skip this event
		}
	}
}

// ServeHTTP handles SSE connections.
func (h *SSEHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan Event, 32)

	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
		close(ch)
	}()

	h.logger.Info("SSE client connected", "clients", h.clientCount())

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("SSE client disconnected", "clients", h.clientCount()-1)
			return
		case event := <-ch:
			data, err := json.Marshal(event.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		}
	}
}

func (h *SSEHub) clientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
