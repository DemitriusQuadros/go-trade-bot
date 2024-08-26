package configuration

import (
	"log"
	"os"

	"github.com/spf13/viper"
)

type Configuration struct {
	Broker Broker
	DB     DB
}

type Broker struct {
	ApiKey    string
	ApiSecret string
}

type DB struct {
	URI      string
	PoolSize int64
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

	uri, ok := viper.Get("DB.URI").(string)
	if !ok {
		log.Fatalf("Invalid db uri")
	}

	poolSize, ok := viper.Get("DB.POOLSIZE").(int)
	if !ok {
		log.Fatalf("Invalid pool size config")
	}

	return &Configuration{
		Broker: Broker{
			ApiKey:    key,
			ApiSecret: secret,
		},
		DB: DB{
			URI:      uri,
			PoolSize: int64(poolSize),
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
