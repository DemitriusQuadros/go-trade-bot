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

type RealtimeHandler struct {
	broadcaster Broadcaster
}

func NewRealtimeHandler(broadcaster Broadcaster) *RealtimeHandler {
	return &RealtimeHandler{
		broadcaster: broadcaster,
	}
}

func (h *RealtimeHandler) Handlers() []handler.Configuration {
	return []handler.Configuration{
		{
			Pattern: "/stream/dashboard",
			Action:  h.StreamDashboard,
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
