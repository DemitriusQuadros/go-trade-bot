package configuration

import (
	"log"
	"os"
	"time"

	"github.com/spf13/viper"
)

type Configuration struct {
	Broker      Broker
	DB          DB
	Redis       Redis
	Prometheus  Prometheus
	Mode        string // process-wide execution-mode ceiling, from MODE env var, e.g. "paper" (Spec 10)
	ConfirmLive bool   // from --confirm-live CLI flag (Spec 10)
	Testnet     bool   // whether the exchange adapter should target Binance testnet (Spec 01/10)
	WebhookURL  string // outbound trade-event notification target (Spec 09)
	DryRun      DryRunConfig
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
		Mode:        mode,
		ConfirmLive: confirmLive,
		Testnet:     testnet,
		WebhookURL:  webhookURL,
		DryRun: DryRunConfig{
			SlippagePct: dryRunSlippage,
			FeePct:      dryRunFeePct,
			FillDelay:   dryRunFillDelay,
		},
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
