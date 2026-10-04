package modules

import (
	"go-trade-bot/internal/indicators"

	"go.uber.org/fx"
)

// IndicatorsModule provides the IndicatorProvider ACL (Spec 07) - the only
// consumer in Phase 1 is app/strategies' ported strategies (Spec 08), wired
// through app/engine.
var IndicatorsModule = fx.Module("indicators",
	fx.Provide(func() indicators.IndicatorProvider {
		return indicators.NewTalibAdapter()
	}),
)
