package configuration

import (
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestConfiguration_MarketTypeAndPositionMode_Defaults(t *testing.T) {
	viper.Reset()
	// Set required fields so NewConfiguration doesn't fail
	viper.Set("BROKER.KEY", "key123")
	viper.Set("BROKER.SECRET", "sec123")
	viper.Set("DB.HOST", "localhost")
	viper.Set("DB.PORT", 5432)
	viper.Set("DB.USER", "postgres")
	viper.Set("DB.PASSWORD", "postgres")
	viper.Set("DB.DBNAME", "test")
	viper.Set("DB.SSLMODE", "disable")
	viper.Set("REDIS.ADDR", "localhost:6379")
	viper.Set("PROMETHEUS.ADDRESS", ":9090")

	// Create a temp dummy config file so setupViper succeeds
	tmpFile, err := os.CreateTemp("", "config-*.yml")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_ = tmpFile.Close()

	os.Setenv("CONFIG_PATH", tmpFile.Name())
	defer os.Unsetenv("CONFIG_PATH")

	cfg := NewConfiguration()
	assert.Equal(t, "spot", cfg.MarketType)
	assert.Equal(t, "spot", cfg.Broker.MarketType)
	assert.Equal(t, "one_way", cfg.PositionMode)
	assert.Equal(t, "one_way", cfg.Broker.PositionMode)
}

func TestConfiguration_MarketTypeAndPositionMode_Explicit(t *testing.T) {
	viper.Reset()
	viper.Set("BROKER.KEY", "key123")
	viper.Set("BROKER.SECRET", "sec123")
	viper.Set("DB.HOST", "localhost")
	viper.Set("DB.PORT", 5432)
	viper.Set("DB.USER", "postgres")
	viper.Set("DB.PASSWORD", "postgres")
	viper.Set("DB.DBNAME", "test")
	viper.Set("DB.SSLMODE", "disable")
	viper.Set("REDIS.ADDR", "localhost:6379")
	viper.Set("PROMETHEUS.ADDRESS", ":9090")

	viper.Set("MARKET_TYPE", "FUTURES")
	viper.Set("POSITION_MODE", "HEDGE")

	tmpFile, err := os.CreateTemp("", "config-*.yml")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_ = tmpFile.Close()

	os.Setenv("CONFIG_PATH", tmpFile.Name())
	defer os.Unsetenv("CONFIG_PATH")

	cfg := NewConfiguration()
	assert.Equal(t, "futures", cfg.MarketType)
	assert.Equal(t, "futures", cfg.Broker.MarketType)
	assert.Equal(t, "hedge", cfg.PositionMode)
	assert.Equal(t, "hedge", cfg.Broker.PositionMode)
}

func TestConfiguration_MarketTypeAndPositionMode_BrokerFallback(t *testing.T) {
	viper.Reset()
	viper.Set("BROKER.KEY", "key123")
	viper.Set("BROKER.SECRET", "sec123")
	viper.Set("DB.HOST", "localhost")
	viper.Set("DB.PORT", 5432)
	viper.Set("DB.USER", "postgres")
	viper.Set("DB.PASSWORD", "postgres")
	viper.Set("DB.DBNAME", "test")
	viper.Set("DB.SSLMODE", "disable")
	viper.Set("REDIS.ADDR", "localhost:6379")
	viper.Set("PROMETHEUS.ADDRESS", ":9090")

	viper.Set("BROKER.MARKET_TYPE", "futures")
	viper.Set("BROKER.POSITION_MODE", "one_way")

	tmpFile, err := os.CreateTemp("", "config-*.yml")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_ = tmpFile.Close()

	os.Setenv("CONFIG_PATH", tmpFile.Name())
	defer os.Unsetenv("CONFIG_PATH")

	cfg := NewConfiguration()
	assert.Equal(t, "futures", cfg.MarketType)
	assert.Equal(t, "futures", cfg.Broker.MarketType)
	assert.Equal(t, "one_way", cfg.PositionMode)
	assert.Equal(t, "one_way", cfg.Broker.PositionMode)
}
