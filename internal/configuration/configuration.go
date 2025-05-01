package configuration

import (
	"log"
	"os"

	"github.com/spf13/viper"
)

type Configuration struct {
	Broker Broker
	DB     DB
	Redis  Redis
}

type Broker struct {
	ApiKey    string
	ApiSecret string
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

	return &Configuration{
		Broker: Broker{
			ApiKey:    key,
			ApiSecret: secret,
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
