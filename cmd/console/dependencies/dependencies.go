package dependencies

import (
	"go-trade-bot/cmd/console/apiclient"
	"go-trade-bot/internal/configuration"
)

type Dependencies struct {
	Cfg *configuration.Configuration
	API *apiclient.Client
}

func Init() *Dependencies {
	cfg := configuration.NewConfiguration()
	baseURL := cfg.APIBaseURL
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	return &Dependencies{
		Cfg: cfg,
		API: apiclient.NewClient(baseURL),
	}
}
