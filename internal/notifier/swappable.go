package notifier

import (
	"context"
	"sync/atomic"

	"go-trade-bot/internal/configuration"
)

type SwappableNotifier struct {
	current atomic.Pointer[WebhookNotifier]
}

func NewSwappableNotifier(cfg *configuration.Configuration) (*SwappableNotifier, error) {
	n, err := NewWebhookNotifier(cfg)
	if err != nil {
		return nil, err
	}
	s := &SwappableNotifier{}
	s.current.Store(n)
	return s, nil
}

func (s *SwappableNotifier) Send(ctx context.Context, event Event) error {
	return s.current.Load().Send(ctx, event)
}

func (s *SwappableNotifier) Swap(cfg *configuration.Configuration) error {
	n, err := NewWebhookNotifier(cfg)
	if err != nil {
		return err
	}
	s.current.Store(n)
	return nil
}
