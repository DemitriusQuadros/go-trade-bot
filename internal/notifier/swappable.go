package notifier

import (
	"context"
	"sync/atomic"

	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/i18n"
)

type SwappableNotifier struct {
	current atomic.Pointer[WebhookNotifier]
	// locales (i18n-02 §4) is carried over to every swapped-in notifier.
	locales atomic.Pointer[localeSourceBox]
}

type localeSourceBox struct{ src i18n.Source }

// SetLocaleSource sets the trade-event webhook language source
// (Settings.DefaultLocale, cached); it survives Swap.
func (s *SwappableNotifier) SetLocaleSource(src i18n.Source) {
	s.locales.Store(&localeSourceBox{src: src})
	if cur := s.current.Load(); cur != nil {
		next := *cur
		next.locales = src
		s.current.Store(&next)
	}
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
	if box := s.locales.Load(); box != nil {
		n.locales = box.src
	}
	s.current.Store(n)
	return nil
}
