## GO BINANCE TRADE BOT

This project aims to provide a golang application with all the structure needed to communicate with Binace APIs and handle strategies to create tradingbots with golang

# Used packages
- FX to handle dependecy injection (https://uber-go.github.io/fx)
- go-binance to communicate with Binance API (https://github.com/adshao/go-binance)
- viper to handle configuration properties (https://github.com/spf13/viper)
- asynq to handle all the asyncronous jobs (https://github.com/hibiken/asynq?tab=readme-ov-file)
- Postgres as database (https://www.postgresql.org/)
- Gorm as ORM do manage database (https://gorm.io/index.html)
- Redis to cache and queue to use with asynq
