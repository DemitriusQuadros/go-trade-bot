package modules

import (
	"go-trade-bot/internal/broker"

	"go.uber.org/fx"
)

var BrokerModule = fx.Module("broker",
	fx.Provide(broker.NewBroker),
)
