package main

import (
	"context"
	"fmt"
	"go-trade-bot/app/entities"
	account "go-trade-bot/app/handler/web/account"
	agenthandler "go-trade-bot/app/handler/web/agent"
	agentreports "go-trade-bot/app/handler/web/agentreports"
	agentshandler "go-trade-bot/app/handler/web/agents"
	authhandler "go-trade-bot/app/handler/web/auth"
	backtest "go-trade-bot/app/handler/web/backtest"
	broker "go-trade-bot/app/handler/web/broker"
	candledatahandler "go-trade-bot/app/handler/web/candledata"
	optimize "go-trade-bot/app/handler/web/optimize"
	performancehistory "go-trade-bot/app/handler/web/performancehistory"
	proposalshandler "go-trade-bot/app/handler/web/proposals"
	realtime "go-trade-bot/app/handler/web/realtime"
	scripthandler "go-trade-bot/app/handler/web/script"
	settings "go-trade-bot/app/handler/web/settings"
	signal "go-trade-bot/app/handler/web/signal"
	strategy "go-trade-bot/app/handler/web/strategy"
	usershandler "go-trade-bot/app/handler/web/users"
	webhooktargets "go-trade-bot/app/handler/web/webhooktargets"
	accountrepo "go-trade-bot/app/repository/account"
	agentrepo "go-trade-bot/app/repository/agent"
	"go-trade-bot/app/repository/agentplatform"
	proposalrepo "go-trade-bot/app/repository/proposal"
	"go-trade-bot/app/strategies"
	_ "go-trade-bot/app/strategies/mlgrpc"
	strategyscript "go-trade-bot/app/strategies/script"
	authusecase "go-trade-bot/app/usecase/auth"
	"go-trade-bot/cmd/api/modules"
	"go-trade-bot/cmd/api/webui"
	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/cfaccess"
	config "go-trade-bot/internal/configuration"
	"go-trade-bot/internal/handler"
	"go-trade-bot/internal/metrics"
	"go-trade-bot/internal/middleware"
	"log"
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

type Route interface {
	Handlers() []handler.Configuration
}

func main() {
	fx.New(appOptions()).Run()
}

// appOptions is the full fx graph (extracted so a test can fx.ValidateApp it).
func appOptions() fx.Option {
	return fx.Options(
		modules.ConfigurationModule,
		modules.DbModule,
		modules.ExchangeModule,
		modules.NotifierModule,
		modules.StrategyModule,
		modules.MetricsModule,
		modules.AccountModule,
		modules.SignalModule,
		modules.CandleModule,
		modules.BacktestModule,
		modules.OptimizeModule,
		modules.PerformanceHistoryModule,
		modules.RealtimeModule,
		modules.CandleDataModule,
		modules.SettingsModule,
		modules.ScriptModule,
		modules.AgentModule,
		modules.AgentPlatformModule,
		modules.AuthModule,
		fx.Provide(routeProviders()...),
		fx.Provide(
			NewHTTPServer,
			fx.Annotate(
				NewServeMux,
				fx.ParamTags(`group:"routes"`, ``, ``),
			),
		),
		fx.Invoke(func(db *gorm.DB) {
			if err := Migrate(db); err != nil {
				log.Fatalf("failed to migrate database: %v", err)
			}
		}),
		// Auth-01: create the first admin from AUTH.BOOTSTRAP_ADMIN_* when
		// the users table is empty (runs after Migrate - fx.Invoke order).
		fx.Invoke(func(u *authusecase.UseCase, cfg *config.Configuration) {
			middleware.WarnIfInsecure(cfg)
			if err := u.Bootstrap(context.Background(), cfg.Auth.BootstrapAdminUsername, cfg.Auth.BootstrapAdminPassword); err != nil {
				log.Fatalf("auth: bootstrap admin failed: %v", err)
			}
		}),
		fx.Invoke(RegisterScriptStrategy),
		fx.Invoke(func(*http.Server) {}),
	)
}

// RegisterScriptStrategy wires the "script" strategy into the global registry
// (backend-04), mirroring cmd/worker/main.go's function of the same name.
// The API process never executes a strategy cycle, but app/usecase/strategy's
// Save/UpdateStatus validation calls strategies.Exists("script") - without
// this, saving or enabling any script-type strategy fails validation here
// even though the worker (which does register it) could run it fine.
func RegisterScriptStrategy(runner *strategyscript.Runner, store strategyscript.ScriptStateStore) {
	strategies.Register("script", func(dbStrategy entities.Strategy) strategies.Strategy {
		return strategyscript.NewScriptStrategy(dbStrategy, store, runner)
	})
}

func NewHTTPServer(
	lc fx.Lifecycle,
	router *mux.Router,
	cfg *config.Configuration,
	collector *metrics.MetricsCollector,
) *http.Server {
	// Cloudflare Access (auth-01 §4) guards the whole hostname - /api and the
	// SPA - when CF_ACCESS.TEAM_DOMAIN and CF_ACCESS.AUD are both set.
	wrappedMux := middleware.ConfigMiddleware(cfg, collector)(cfaccess.Wrap(cfg.CFAccess.TeamDomain, cfg.CFAccess.AUD, router))
	srv := &http.Server{Addr: ":8080", Handler: wrappedMux}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}
			fmt.Println("Starting HTTP server at", srv.Addr)
			go srv.Serve(ln)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	})
	return srv
}

// NewServeMux mounts every handler-declared route under "/api" so the
// backend's path space can never collide with the frontend SPA's
// client-side routes (both used bare names like "/backtest", "/settings" -
// a full-page load/refresh on one of those SPA routes used to hit this
// router's exact-match API route instead of falling through to the SPA's
// index.html, since mux matches routes in registration order and the API
// routes were registered before the catch-all). "/metrics" (Prometheus
// scrape target) and the embedded SPA's static assets stay unprefixed.
//
// Auth-01 §3: every route is wrapped with auth.Authenticate (session cookie,
// API_TOKEN bearer, or the explicit insecure mode) and
// middleware.RequireCapability(route.Capability). A route with an empty or
// unknown capability panics here, at startup, so it can never ship
// unprotected. auth may be nil (tests): then only the API_TOKEN bearer and
// insecure mode authenticate.
func NewServeMux(routes []Route, cfg *config.Configuration, auth *middleware.Auth) *mux.Router {
	if auth == nil {
		auth = middleware.NewAuth(cfg, nil)
	}
	router := mux.NewRouter()

	apiRouter := router.PathPrefix("/api").Subrouter()
	for _, route := range routes {
		for _, h := range route.Handlers() {
			authz.MustRouteCapability(h.Method, h.Pattern, h.Capability)
			apiRouter.HandleFunc(h.Pattern, auth.Authenticate(middleware.RequireCapability(h.Capability, h.Action))).Methods(h.Method)
		}
	}

	router.Handle("/metrics", promhttp.Handler())
	router.PathPrefix("/").Handler(webui.Handler())
	return router
}

// routeConstructors are every Route mounted under /api. The route-walk
// test (auth-01 §3) iterates this list, so every registered route is
// checked for a capability.
var routeConstructors = []any{
	strategy.NewStrategyHandler,
	broker.NewBrokerHandler,
	account.NewAccountHandler,
	signal.NewSignalHandler,
	backtest.NewBacktestHandler,
	optimize.NewOptimizeHandler,
	performancehistory.NewHandler,
	realtime.NewRealtimeHandler,
	candledatahandler.NewHandler,
	settings.NewSettingsHandler,
	scripthandler.NewScriptHandler,
	agenthandler.NewAgentHandlerWithUsers,
	agentshandler.NewAgentsHandler,
	agentreports.NewAgentReportsHandler,
	webhooktargets.NewWebhookTargetsHandler,
	proposalshandler.NewProposalsHandler,
	authhandler.NewAuthHandler,
	usershandler.NewUsersHandler,
}

func routeProviders() []any {
	out := make([]any, 0, len(routeConstructors))
	for _, c := range routeConstructors {
		out = append(out, AsRoute(c))
	}
	return out
}

func AsRoute(f any) any {
	return fx.Annotate(
		f,
		fx.As(new(Route)),
		fx.ResultTags(`group:"routes"`),
	)
}

func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&entities.Strategy{},
		&entities.StrategyExecution{},
		&entities.Signal{},
		&entities.Order{},
		&entities.Account{},
		&entities.Candle{},
		&entities.CandleDataset{},
		&entities.CandleSegment{},
		&entities.CandleChunk{},
		&entities.BacktestRun{},
		&entities.OptimizationRun{},
		&entities.StrategyPerformanceSnapshot{},
		&entities.Settings{},
		&entities.ScriptVersion{},
		&entities.ScriptState{},
		&entities.AgentInstruction{},
		&entities.AgentRun{},
		// Agents platform (Phase A-01) - keep in sync across cmd/api,
		// cmd/mcp and cmd/agent.
		&entities.Agent{},
		&entities.AgentStrategyBinding{},
		&entities.StrategyMemoryEntry{},
		&entities.AgentReport{},
		&entities.WebhookTarget{},
		&entities.AgentUsage{},
		// Agents platform Phase B-01.
		&entities.StrategyChangeProposal{},
		&entities.DeployGateConfig{},
		// Auth-01 (multi-user).
		&entities.User{},
		&entities.Session{},
		&entities.UserUsage{},
	); err != nil {
		return err
	}
	// Seed the deploy gate thresholds (idempotent).
	if err := proposalrepo.NewGormRepository(db).EnsureGateConfig(context.Background()); err != nil {
		return err
	}

	// Seed the default "Copilot" agent persona (idempotent).
	if _, err := agentplatform.NewGormRepository(db).EnsureDefaultAgent(context.Background()); err != nil {
		return err
	}
	// Seed the empty house-rules row (idempotent, fix-02 B4).
	if err := agentrepo.NewGormRepository(db).EnsureInstruction(context.Background()); err != nil {
		return err
	}

	// Seed default accounts (idempotent, ensures dryrun and live accounts exist).
	if err := accountrepo.NewAccountRepository(db).EnsureDefaultAccounts(); err != nil {
		return err
	}

	// Legacy backfill (Spec 06): older DBs may still carry rows that only set
	// the now-removed `algorithm` column (backend-05 dropped the Strategy.
	// Algorithm field/enum entirely). If that column still physically exists
	// on this database (AutoMigrate never drops columns), backfill
	// strategy_name from it once. On a fresh database the column was never
	// created, so this step is skipped rather than failing on an unknown
	// column reference.
	if db.Migrator().HasColumn(&entities.Strategy{}, "algorithm") {
		if err := db.Exec(`
			UPDATE strategies
			SET strategy_name = algorithm
			WHERE (strategy_name IS NULL OR strategy_name = '') AND algorithm IS NOT NULL AND algorithm <> ''
		`).Error; err != nil {
			return err
		}
	}

	return nil
}
