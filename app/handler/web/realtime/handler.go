package realtime

import (
	"fmt"
	"net/http"

	"go-trade-bot/app/usecase/realtime"
	"go-trade-bot/internal/handler"
)

type Broadcaster interface {
	Subscribe() (<-chan realtime.Event, func())
}

type PreviewBroadcaster interface {
	Subscribe(strategyID uint) (<-chan realtime.Event, func())
}

type RealtimeHandler struct {
	broadcaster        Broadcaster
	previewBroadcaster PreviewBroadcaster
}

func NewRealtimeHandler(broadcaster Broadcaster, previewBroadcaster PreviewBroadcaster) *RealtimeHandler {
	return &RealtimeHandler{
		broadcaster:        broadcaster,
		previewBroadcaster: previewBroadcaster,
	}
}

func (h *RealtimeHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/stream/dashboard",
			Action:  h.StreamDashboard,
			Method:  http.MethodGet,
		},
		{
			Pattern: "/stream/script-preview/{id}",
			Action:  h.StreamPreview,
			Method:  http.MethodGet,
		},
	}
}

func (h *RealtimeHandler) StreamDashboard(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	fmt.Fprintf(w, "retry: 5000\n\n")
	flusher.Flush()

	ch, unsubscribe := h.broadcaster.Subscribe()
	defer unsubscribe()

	for {
		select {
		case event, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.JSON())
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (h *RealtimeHandler) StreamPreview(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	importIdStr := r.URL.Path[len("/stream/script-preview/"):]
	var id uint
	if _, err := fmt.Sscanf(importIdStr, "%d", &id); err != nil {
		http.Error(w, "invalid strategy ID", http.StatusBadRequest)
		return
	}

	if h.previewBroadcaster == nil {
		http.Error(w, "preview broadcaster not configured", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	fmt.Fprintf(w, "retry: 5000\n\n")
	flusher.Flush()

	ch, unsubscribe := h.previewBroadcaster.Subscribe(id)
	defer unsubscribe()

	for {
		select {
		case event, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.JSON())
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
