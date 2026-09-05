package modules

import (
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/notifier"

	"go.uber.org/fx"
)

// NotifierModule provides the NotificationSender ACL (Spec 09) for the API
// process - needed because SignalUseCase.Close (POST /signal/close/{id})
// can now emit position.closed / strategy.error webhook events.
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
