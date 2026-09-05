package modules

import (
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/notifier"

	"go.uber.org/fx"
)

// NotifierModule provides the NotificationSender ACL (Spec 09). An empty
// WebhookURL yields a WebhookNotifier whose Send is a safe no-op - webhook
// configuration is optional for basic live trading to work.
var NotifierModule = fx.Module("notifier",
	fx.Provide(provideNotifier),
)

func provideNotifier(cfg *configuration.Configuration) (notifier.NotificationSender, error) {
	n, err := notifier.NewWebhookNotifier(cfg)
	if err != nil {
		return nil, err
	}
	return n, nil
}
