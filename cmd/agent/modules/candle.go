package modules

import (
	repository "go-trade-bot/app/repository/candle"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

var CandleModule = fx.Module("candle",
	fx.Provide(
		func(db *gorm.DB) repository.Repository {
			return repository.NewCandleRepository(db)
		},
	),
)
