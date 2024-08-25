package middleware

import (
	"context"
	"go-trade-bot/internal/configuration"
	"net/http"
)

type key int

const configKey key = 0

func ConfigMiddleware(cfg *configuration.Configuration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), configKey, cfg)
			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}

func FromContext(ctx context.Context) *configuration.Configuration {
	cfg, ok := ctx.Value(configKey).(*configuration.Configuration)
	if !ok {
		return nil
	}
	return cfg
}
