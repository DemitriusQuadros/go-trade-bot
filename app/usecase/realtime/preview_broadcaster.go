package realtime

import (
	"sync"
)

// PreviewBroadcaster implements a per-strategy fan-out for live preview events.
type PreviewBroadcaster struct {
	mu     sync.RWMutex
	clients map[uint]map[chan Event]struct{}
}

func NewPreviewBroadcaster() *PreviewBroadcaster {
	return &PreviewBroadcaster{
		clients: make(map[uint]map[chan Event]struct{}),
	}
}

func (b *PreviewBroadcaster) Subscribe(strategyID uint) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, 64)
	if _, ok := b.clients[strategyID]; !ok {
		b.clients[strategyID] = make(map[chan Event]struct{})
	}
	b.clients[strategyID][ch] = struct{}{}

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if clients, ok := b.clients[strategyID]; ok {
			delete(clients, ch)
			close(ch)
			if len(clients) == 0 {
				delete(b.clients, strategyID)
			}
		}
	}
}

func (b *PreviewBroadcaster) Broadcast(strategyID uint, event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	clients, ok := b.clients[strategyID]
	if !ok {
		return
	}
	for ch := range clients {
		select {
		case ch <- event:
		default:
			// drop if client is blocked
		}
	}
}
