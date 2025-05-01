package modules

import (
	"go-trade-bot/internal/db"

	"go.uber.org/fx"
)

var DbModule = fx.Module("db",
	fx.Provide(
		db.NewDatabase,
	),
)
