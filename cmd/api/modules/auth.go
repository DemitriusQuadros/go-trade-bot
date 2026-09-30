package modules

import (
	agenthandler "go-trade-bot/app/handler/web/agent"
	authhandler "go-trade-bot/app/handler/web/auth"
	usershandler "go-trade-bot/app/handler/web/users"
	userrepo "go-trade-bot/app/repository/user"
	authusecase "go-trade-bot/app/usecase/auth"
	"go-trade-bot/internal/configuration"
	"go-trade-bot/internal/middleware"
	"go-trade-bot/internal/ratelimit"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// AuthModule wires app accounts, sessions and permissions (auth-01): the
// user repository, the auth usecase (login rate limit on Redis with an
// in-memory fallback), the /auth and /users handlers, and the request
// authenticator NewServeMux wraps every /api route with.
var AuthModule = fx.Module("auth",
	fx.Provide(
		func(db *gorm.DB) authusecase.Repository { return userrepo.NewGormRepository(db) },
		func(cfg *configuration.Configuration) authusecase.Counter {
			return ratelimit.NewRedisCounterFromAddr(cfg.Redis.Addr)
		},
		authusecase.NewUseCase,
		func(u *authusecase.UseCase) authhandler.UseCase { return u },
		func(u *authusecase.UseCase) usershandler.UseCase { return u },
		func(u *authusecase.UseCase) agenthandler.UserBudget { return u },
		func(cfg *configuration.Configuration, u *authusecase.UseCase) *middleware.Auth {
			return middleware.NewAuth(cfg, u)
		},
	),
)
