package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"go-trade-bot/internal/configuration"
)

// RequireAuth wraps a single handler.Configuration's Action.
// Checks Authorization: Bearer <token> (or ?token= query parameter) against cfg.APIToken
// using constant-time comparison (crypto/subtle.ConstantTimeCompare).
func RequireAuth(cfg *configuration.Configuration, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg == nil || cfg.AllowInsecureNoAuth {
			next(w, r)
			return
		}

		token := ExtractBearerToken(r)
		if token == "" || !ConstantTimeEquals(token, cfg.APIToken) {
			WriteUnauthorized(w)
			return
		}

		next(w, r)
	}
}

// ExtractBearerToken extracts token from Authorization: Bearer <token> header,
// or falls back to ?token= query parameter.
func ExtractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			token := strings.TrimSpace(parts[1])
			if token != "" {
				return token
			}
		}
	}

	// Fallback to query parameter ?token= for SSE / iframe requests
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// ConstantTimeEquals compares two strings in constant time.
func ConstantTimeEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// WriteUnauthorized writes standard 401 response JSON.
func WriteUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized","message":"missing or invalid Authorization header"}`))
}
