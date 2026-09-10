package modules

import (
	"go-trade-bot/internal/notifier"

	"go.uber.org/fx"
)

// NotifierModule provides the NotificationSender ACL (Spec 09). An empty
// WebhookURL yields a WebhookNotifier whose Send is a safe no-op - webhook
// configuration is optional for basic live trading to work.
//
// Spec backend-05 (ADR-016): wrapped in *notifier.SwappableNotifier so
// WebhookURL changes hot-swap immediately with no drain guard (it's a
// safe-tier field - worst case of a mid-flight swap is one notification
// using the old URL). fx also provides the concrete *notifier.SwappableNotifier
// type directly so the settings usecase can be injected with it.
var NotifierModule = fx.Module("notifier",
	fx.Provide(
		notifier.NewSwappableNotifier,
		asNotificationSenderInterface,
	),
)

func asNotificationSenderInterface(s *notifier.SwappableNotifier) notifier.NotificationSender {
	return s
}
