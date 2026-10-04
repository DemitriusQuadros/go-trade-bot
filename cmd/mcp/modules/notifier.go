package modules

import (
	"go-trade-bot/internal/notifier"

	"go.uber.org/fx"
)

var NotifierModule = fx.Module("notifier",
	fx.Provide(
		notifier.NewSwappableNotifier,
		asNotificationSenderInterface,
	),
)

func asNotificationSenderInterface(s *notifier.SwappableNotifier) notifier.NotificationSender {
	return s
}
