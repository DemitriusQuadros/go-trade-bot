package modules

import (
	"go-trade-bot/internal/i18n"
	"go-trade-bot/internal/notifier"

	"go.uber.org/fx"
)

var NotifierModule = fx.Module("notifier",
	fx.Provide(
		notifier.NewSwappableNotifier,
		asNotificationSenderInterface,
	),
	// i18n-02 §4: any trade-event webhook from this process uses
	// Settings.DefaultLocale (source provided by AgentModule).
	fx.Invoke(func(n *notifier.SwappableNotifier, src i18n.Source) { n.SetLocaleSource(src) }),
)

func asNotificationSenderInterface(s *notifier.SwappableNotifier) notifier.NotificationSender {
	return s
}
