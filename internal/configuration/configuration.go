package configuration

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Configuration struct {
	Broker              Broker
	DB                  DB
	Redis               Redis
	Prometheus          Prometheus
	Mode                string // process-wide execution-mode ceiling, from MODE env var, e.g. "paper" (Spec 10)
	ConfirmLive         bool   // from --confirm-live CLI flag (Spec 10)
	Testnet             bool   // whether the exchange adapter should target Binance testnet (Spec 01/10)
	WebhookURL          string // outbound trade-event notification target (Spec 09)
	APIBaseURL          string // base URL for cmd/api (Spec TUI-01, default http://localhost:8080)
	// APIToken, when set, is a service token: Authorization: Bearer
	// <API_TOKEN> acts as a synthetic admin principal (auth-01 §4) for
	// scripts and MCP-over-HTTP. Optional - users log in with sessions.
	APIToken string
	// AllowInsecureNoAuth disables authentication (every request acts as a
	// synthetic admin). Only ever true when ALLOW_INSECURE_NO_AUTH=true is
	// set explicitly; auth is ON by default (auth-01 §4).
	AllowInsecureNoAuth bool
	// Auth configures app accounts and sessions (auth-01 §4).
	Auth AuthConfig
	// CFAccess configures Cloudflare Access JWT verification (auth-01 §4).
	CFAccess CFAccessConfig
	// InternalBridgeAddr is the address cmd/worker's host-internal
	// settings-apply listener binds to (Spec backend-05) - loopback-only by
	// default (127.0.0.1), deliberately never the wildcard/all-interfaces
	// address cmd/worker's public :9191 monitoring server uses. cmd/api's
	// settings usecase dials this same address to forward a validated
	// PUT /settings call into cmd/worker's process-local drain-then-swap.
	InternalBridgeAddr string
	// InternalBridgeSecret, if set, must be sent by cmd/api on every call to
	// the internal settings-apply endpoint (as a header - see
	// internal/settingsbridge) and is checked by cmd/worker before
	// processing the request. Defense-in-depth on top of the loopback bind,
	// for deployments where cmd/api and cmd/worker might not share a host.
	// Empty is accepted (with a startup warning) for backward compatibility
	// with existing config.yml files that predate this field.
	InternalBridgeSecret string
	DryRun               DryRunConfig
	Console              ConsoleConfig
	Agent                Agent
	AgentRuntime         AgentRuntime
	CandleData           CandleData
}

// AuthConfig is the AUTH.* block (auth-01 §4). All keys optional.
type AuthConfig struct {
	// TrustProxy trusts X-Forwarded-Proto for the cookie's Secure flag
	// (AUTH.TRUST_PROXY; set it behind cloudflared/a reverse proxy).
	TrustProxy bool
	// AllowedOrigins are extra Origin values (e.g. "https://bot.example.com")
	// accepted by the CSRF check besides the request's own host
	// (AUTH.ALLOWED_ORIGINS, a list or a comma-separated string).
	AllowedOrigins []string
	// BootstrapAdminUsername/Password create the first admin on startup when
	// the users table is empty (AUTH.BOOTSTRAP_ADMIN_USERNAME/_PASSWORD).
	// Remove the password from config after the first run.
	BootstrapAdminUsername string
	BootstrapAdminPassword string
}

// CFAccessConfig is the CF_ACCESS.* block (auth-01 §4). Verification is on
// only when both are set.
type CFAccessConfig struct {
	// TeamDomain, e.g. "myteam.cloudflareaccess.com" (CF_ACCESS.TEAM_DOMAIN).
	TeamDomain string
	// AUD is the Access application audience tag (CF_ACCESS.AUD).
	AUD string
}

// Enabled reports whether both values are set.
func (c CFAccessConfig) Enabled() bool { return c.TeamDomain != "" && c.AUD != "" }

// CandleData configures the candle-dataset reconciler running inside
// cmd/worker (docs/specs/candle-data). Read leniently with defaults.
type CandleData struct {
	// Concurrency is the asynq worker count of the dedicated candle server
	// (queues candles_live + candles), separate from the trading server so a
	// long backfill can never delay a strategy cycle. CANDLE_DATA.CONCURRENCY,
	// default 2.
	Concurrency int
	// WeightBudget is how much Binance request weight per minute the
	// reconciler may spend across all worker replicas (the limit is 6000 per
	// IP and trading shares it). CANDLE_DATA.WEIGHT_BUDGET, default 3000.
	WeightBudget int
}

const (
	DefaultCandleDataConcurrency  = 2
	DefaultCandleDataWeightBudget = 3000
)

func loadCandleData() CandleData {
	c := CandleData{
		Concurrency:  viper.GetInt("CANDLE_DATA.CONCURRENCY"),
		WeightBudget: viper.GetInt("CANDLE_DATA.WEIGHT_BUDGET"),
	}
	if c.Concurrency <= 0 {
		c.Concurrency = DefaultCandleDataConcurrency
	}
	if c.WeightBudget <= 0 {
		c.WeightBudget = DefaultCandleDataWeightBudget
	}
	return c
}

// AgentRuntime configures the cmd/agent binary (agents-platform A-02 §1).
// Read leniently with defaults - absent keys never block any binary.
type AgentRuntime struct {
	// Concurrency is the asynq worker count for the "agents" queue.
	// AGENT_RUNTIME.CONCURRENCY, default 2.
	Concurrency int
	// MetricsPort is the port cmd/agent serves /metrics on.
	// AGENT_RUNTIME.METRICS_PORT, default "9194" - NOT 9193, which is
	// cmd/worker's default loopback settings-bridge port
	// (INTERNAL_BRIDGE_ADDR 127.0.0.1:9193); both binaries run on the same
	// host (host networking in docker-compose).
	MetricsPort string
	// SyncInterval is how often the cron PeriodicTaskManager re-reads agent
	// schedules from the DB. AGENT_RUNTIME.SYNC_INTERVAL, default 30s.
	SyncInterval time.Duration
}

// Agent holds model-provider credentials and selection for the AI strategy
// agent (Backend Spec 01). Single-operator scope (no per-user rows) - this
// is process config, not DB state, matching the resolved decision to reuse
// the Broker/API_TOKEN pattern rather than add an encryption-at-rest layer.
type Agent struct {
	Provider       string // "anthropic" | "gemini" - which provider is active
	AnthropicKey   string // env AGENT_ANTHROPIC_KEY / config.yml AGENT.ANTHROPIC_KEY
	AnthropicModel string // e.g. "claude-sonnet-5"; empty uses the adapter's built-in default
	GeminiKey      string // env AGENT_GEMINI_KEY / config.yml AGENT.GEMINI_KEY
	GeminiModel    string
}

type ConsoleConfig struct {
	MetricsEnabled bool   // default false
	MetricsPort    string // default "9192"
}

type DryRunConfig struct {
	SlippagePct float64
	FeePct      float64
	FillDelay   time.Duration
}

type Broker struct {
	ApiKey    string
	ApiSecret string
	// TestnetApiKey/TestnetApiSecret are used exclusively when Testnet == true
	// (ModePaper, Spec 10) - never the production ApiKey/ApiSecret.
	TestnetApiKey    string
	TestnetApiSecret string
}

type Prometheus struct {
	Address string
}

type DB struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}
type Redis struct {
	Addr string
}

func NewConfiguration() *Configuration {
	if err := setupViper(); err != nil {
		log.Printf("Critical error reading configuration")
		panic("Error reading config file")
	}

	key, ok := viper.Get("BROKER.KEY").(string)
	if !ok {
		log.Fatalf("Invalid broker key")
	}

	secret, ok := viper.Get("BROKER.SECRET").(string)
	if !ok {
		log.Fatalf("Invalid broker secret")
	}

	host, ok := viper.Get("DB.HOST").(string)
	if !ok {
		log.Fatalf("Invalid db host")
	}

	port, ok := viper.Get("DB.PORT").(int)
	if !ok {
		log.Fatalf("Invalid db port")
	}

	user, ok := viper.Get("DB.USER").(string)
	if !ok {
		log.Fatalf("Invalid db user")
	}

	password, ok := viper.Get("DB.PASSWORD").(string)
	if !ok {
		log.Fatalf("Invalid db password")
	}

	dbName, ok := viper.Get("DB.DBNAME").(string)
	if !ok {
		log.Fatalf("Invalid db name")
	}

	sslMode, ok := viper.Get("DB.SSLMODE").(string)
	if !ok {
		log.Fatalf("Invalid db sslmode")
	}

	redisAddr, ok := viper.Get("REDIS.ADDR").(string)
	if !ok {
		log.Fatalf("Invalid redis configuration")
	}

	prometheus, ok := viper.Get("PROMETHEUS.ADDRESS").(string)
	if !ok {
		log.Fatalf("Invalid prometheus address")
	}

	// New Phase 1 fields are read leniently (no log.Fatalf on absence) so
	// existing deployments/config.yml files that predate this feature set
	// keep working: unset MODE defaults to the safest tier (Spec 10 AC#8),
	// unset WebhookURL is a documented no-op (Spec 09 AC#4), unset Testnet
	// defaults to false (production), and unset testnet credentials only
	// matter if Testnet is actually enabled.
	mode := viper.GetString("MODE")
	if mode == "" {
		// Spec 10 AC#8: an unset MODE must resolve to the safest tier, not an
		// error and not "live" - matches the per-strategy schema default.
		mode = "dryrun"
	}
	confirmLive := viper.GetBool("CONFIRM_LIVE")
	testnet := viper.GetBool("TESTNET")
	webhookURL := viper.GetString("WEBHOOK_URL")
	testnetKey := viper.GetString("BROKER.TESTNET_KEY")
	testnetSecret := viper.GetString("BROKER.TESTNET_SECRET")

	dryRunSlippage := viper.GetFloat64("DRY_RUN.SLIPPAGE_PCT")
	dryRunFeePct := viper.GetFloat64("DRY_RUN.FEE_PCT")
	if dryRunFeePct == 0 {
		dryRunFeePct = 0.1
	}
	dryRunFillDelay := viper.GetDuration("DRY_RUN.FILL_DELAY")

	consoleMetricsEnabled := viper.GetBool("CONSOLE.METRICS_ENABLED")
	consoleMetricsPort := viper.GetString("CONSOLE.METRICS_PORT")
	if consoleMetricsPort == "" {
		consoleMetricsPort = "9192"
	}

	apiBaseURL := viper.GetString("API_BASE_URL")
	if apiBaseURL == "" {
		apiBaseURL = viper.GetString("API.BASE_URL")
	}
	if apiBaseURL == "" {
		apiBaseURL = "http://localhost:8080"
	}

	apiToken := viper.GetString("API_TOKEN")
	if apiToken == "" {
		apiToken = viper.GetString("API.TOKEN")
	}
	// Auth is ON by default (auth-01 §4): there is no "no token -> insecure"
	// fallback any more. Only an explicit ALLOW_INSECURE_NO_AUTH=true
	// disables it (cmd/api logs a loud warning on every startup).
	allowInsecure := viper.GetBool("ALLOW_INSECURE_NO_AUTH")
	authCfg := loadAuth()
	cfAccess := CFAccessConfig{
		TeamDomain: strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(viper.GetString("CF_ACCESS.TEAM_DOMAIN")), "https://"), "/"),
		AUD:        strings.TrimSpace(viper.GetString("CF_ACCESS.AUD")),
	}

	internalBridgeAddr := viper.GetString("INTERNAL_BRIDGE_ADDR")
	if internalBridgeAddr == "" {
		// Loopback-only default (Spec backend-05) - deliberately distinct
		// from cmd/worker's public :9191 monitoring server, which binds all
		// interfaces. Never default this to a wildcard address.
		internalBridgeAddr = "127.0.0.1:9193"
	}
	internalBridgeSecret := viper.GetString("INTERNAL_BRIDGE_SECRET")

	// Agent config (Backend Spec 01) is read leniently, same as the other
	// Phase 1+ additions above - an unset AGENT.* block must not prevent
	// cmd/api/cmd/worker (which never construct a ModelProvider) from
	// starting. cmd/mcp's ModelProviderModule is the only place that fails
	// fast on a missing/invalid value (see cmd/mcp/modules/modelprovider.go).
	agentProvider := viper.GetString("AGENT.PROVIDER")
	agentAnthropicKey := viper.GetString("AGENT.ANTHROPIC_KEY")
	agentAnthropicModel := viper.GetString("AGENT.ANTHROPIC_MODEL")
	agentGeminiKey := viper.GetString("AGENT.GEMINI_KEY")
	agentGeminiModel := viper.GetString("AGENT.GEMINI_MODEL")

	agentRuntime := loadAgentRuntime()

	return &Configuration{
		Broker: Broker{
			ApiKey:           key,
			ApiSecret:        secret,
			TestnetApiKey:    testnetKey,
			TestnetApiSecret: testnetSecret,
		},
		DB: DB{
			Host:     host,
			Port:     port,
			User:     user,
			Password: password,
			DBName:   dbName,
			SSLMode:  sslMode,
		},
		Redis: Redis{
			Addr: redisAddr,
		},
		Prometheus: Prometheus{
			Address: prometheus,
		},
		Mode:                 mode,
		ConfirmLive:          confirmLive,
		Testnet:              testnet,
		WebhookURL:           webhookURL,
		APIBaseURL:           apiBaseURL,
		APIToken:             apiToken,
		AllowInsecureNoAuth:  allowInsecure,
		Auth:                 authCfg,
		CFAccess:             cfAccess,
		InternalBridgeAddr:   internalBridgeAddr,
		InternalBridgeSecret: internalBridgeSecret,
		DryRun: DryRunConfig{
			SlippagePct: dryRunSlippage,
			FeePct:      dryRunFeePct,
			FillDelay:   dryRunFillDelay,
		},
		Console: ConsoleConfig{
			MetricsEnabled: consoleMetricsEnabled,
			MetricsPort:    consoleMetricsPort,
		},
		Agent: Agent{
			Provider:       agentProvider,
			AnthropicKey:   agentAnthropicKey,
			AnthropicModel: agentAnthropicModel,
			GeminiKey:      agentGeminiKey,
			GeminiModel:    agentGeminiModel,
		},
		AgentRuntime: agentRuntime,
		CandleData:   loadCandleData(),
	}
}

// Defaults for AgentRuntime.
const (
	DefaultAgentConcurrency  = 2
	DefaultAgentMetricsPort  = "9194"
	DefaultAgentSyncInterval = 30 * time.Second
)

func loadAgentRuntime() AgentRuntime {
	rt := AgentRuntime{
		Concurrency:  viper.GetInt("AGENT_RUNTIME.CONCURRENCY"),
		MetricsPort:  viper.GetString("AGENT_RUNTIME.METRICS_PORT"),
		SyncInterval: viper.GetDuration("AGENT_RUNTIME.SYNC_INTERVAL"),
	}
	return rt.WithDefaults()
}

// WithDefaults fills zero values with the documented defaults.
func (rt AgentRuntime) WithDefaults() AgentRuntime {
	if rt.Concurrency <= 0 {
		rt.Concurrency = DefaultAgentConcurrency
	}
	if rt.MetricsPort == "" {
		rt.MetricsPort = DefaultAgentMetricsPort
	}
	if rt.SyncInterval <= 0 {
		rt.SyncInterval = DefaultAgentSyncInterval
	}
	return rt
}

func loadAuth() AuthConfig {
	var origins []string
	switch v := viper.Get("AUTH.ALLOWED_ORIGINS").(type) {
	case []any:
		for _, o := range v {
			if s, ok := o.(string); ok {
				origins = append(origins, s)
			}
		}
	case []string:
		origins = v
	case string:
		origins = strings.Split(v, ",")
	}
	cleaned := make([]string, 0, len(origins))
	for _, o := range origins {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			cleaned = append(cleaned, o)
		}
	}
	return AuthConfig{
		TrustProxy:             viper.GetBool("AUTH.TRUST_PROXY"),
		AllowedOrigins:         cleaned,
		BootstrapAdminUsername: strings.TrimSpace(viper.GetString("AUTH.BOOTSTRAP_ADMIN_USERNAME")),
		BootstrapAdminPassword: viper.GetString("AUTH.BOOTSTRAP_ADMIN_PASSWORD"),
	}
}

func setupViper() error {
	configFilePath := os.Getenv("CONFIG_PATH")
	if configFilePath == "" {
		viper.SetConfigName("config")
		viper.AddConfigPath("./")
		viper.AutomaticEnv()
		viper.SetConfigType("yml")
		return viper.ReadInConfig()
	}
	viper.SetConfigFile(configFilePath)
	return viper.ReadInConfig()
}
