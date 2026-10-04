package modules

import (
	"go-trade-bot/internal/configuration"

	"go.uber.org/fx"
)

var ConfigurationModule = fx.Module("configuration",
	fx.Provide(configuration.NewConfiguration),
)
