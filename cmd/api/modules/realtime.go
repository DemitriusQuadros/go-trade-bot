package modules

import (
	"context"
	"time"

	realtimehandler "go-trade-bot/app/handler/web/realtime"
	signalrepo "go-trade-bot/app/repository/signal"
	strategyrepo "go-trade-bot/app/repository/strategy"
	"go-trade-bot/app/usecase/realtime"
	"go.uber.org/fx"
)

var RealtimeModule = fx.Module("realtime",
	fx.Provide(
		func(s signalrepo.SignalRepository) realtime.SignalRepository { return s },
		func(s strategyrepo.StrategyRepository) realtime.StrategyRepository { return s },
		realtime.NewDashboardBroadcaster,
		func(b *realtime.DashboardBroadcaster) realtimehandler.Broadcaster { return b },
		realtime.NewPreviewBroadcaster,
		func(b *realtime.PreviewBroadcaster) realtimehandler.PreviewBroadcaster { return b },
		realtimehandler.NewRealtimeHandler,
	),
	fx.Invoke(func(lc fx.Lifecycle, b *realtime.DashboardBroadcaster) {
		ctx, cancel := context.WithCancel(context.Background())
		lc.Append(fx.Hook{
			OnStart: func(_ context.Context) error {
				go b.Run(ctx, 3*time.Second)
				return nil
			},
			OnStop: func(_ context.Context) error {
				cancel()
				return nil
			},
		})
	}),
)
