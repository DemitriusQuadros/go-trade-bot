package modules

import (
	"go-trade-bot/internal/notifier"

	"go.uber.org/fx"
)

// NotifierModule provides the NotificationSender ACL (Spec 09) for the API
// process - needed because SignalUseCase.Close (POST /signal/close/{id})
// can now emit position.closed / strategy.error webhook events.
//
// Spec backend-05 (ADR-016): wrapped in *notifier.SwappableNotifier - see
// cmd/worker/modules/notifier.go's comment for the full rationale.
var NotifierModule = fx.Module("notifier",
	fx.Provide(
		notifier.NewSwappableNotifier,
		asNotificationSenderInterface,
	),
)

func asNotificationSenderInterface(s *notifier.SwappableNotifier) notifier.NotificationSender {
	return s
}
