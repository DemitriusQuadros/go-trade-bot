package configuration

import (
	"log"
	"os"

	"github.com/spf13/viper"
)

type Configuration struct {
	BinanceApiKey    string
	BinanceAPISecret string
}

func NewConfiguration() *Configuration {
	if err := setupViper(); err != nil {
		log.Printf("Critical error reading configuration")
		panic("Error reading config file")
	}

	key, ok := viper.Get("BINANCE.KEY").(string)
	if !ok {
		log.Fatalf("Invalid binance key")
	}

	secret, ok := viper.Get("BINANCE.SECRET").(string)
	if !ok {
		log.Fatalf("Invalid binance secret")
	}

	return &Configuration{
		BinanceApiKey:    key,
		BinanceAPISecret: secret,
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
