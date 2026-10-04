package middleware

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go-trade-bot/internal/authz"
	"go-trade-bot/internal/configuration"
)

// SessionCookieName is the session cookie (auth-01 §4).
const SessionCookieName = "gtb_session"

// CSRFHeaderValue is the X-Requested-With value cookie-authenticated writes
// must carry (unless their Origin matches).
const CSRFHeaderValue = "gtb"

// SessionResolver turns a raw session cookie into a principal
// (app/usecase/auth.UseCase.ResolveSession).
type SessionResolver interface {
	ResolveSession(ctx context.Context, raw string) (authz.Principal, error)
}

// Auth authenticates /api requests (auth-01 §4):
//
//   - Authorization: Bearer <API_TOKEN> -> the synthetic "service" admin
//     principal (only when API_TOKEN is set; a wrong bearer is a 401).
//     Bearer requests are exempt from the CSRF check.
//   - the gtb_session cookie -> that user's principal. Every non-GET/HEAD
//     request authenticated by the cookie must carry X-Requested-With: gtb
//     or an Origin whose host is the request host (or AUTH.ALLOWED_ORIGINS),
//     otherwise 403.
//   - neither, with ALLOW_INSECURE_NO_AUTH=true -> a synthetic admin.
//
// There is no ?token= query fallback any more: SSE and iframes are same
// origin and send the cookie.
type Auth struct {
	cfg      *configuration.Configuration
	sessions SessionResolver
}

// NewAuth builds an Auth. sessions may be nil (service token only).
func NewAuth(cfg *configuration.Configuration, sessions SessionResolver) *Auth {
	if cfg == nil {
		cfg = &configuration.Configuration{}
	}
	return &Auth{cfg: cfg, sessions: sessions}
}

// Authenticate resolves the principal and puts it in the request context.
// It rejects only a bad bearer token (401) and a cookie-authenticated write
// that fails the CSRF check (403); requests without credentials pass
// through unauthenticated (RequireCapability decides).
func (a *Auth) Authenticate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token, ok := bearerToken(r); ok {
			if a.cfg.APIToken == "" || !ConstantTimeEquals(token, a.cfg.APIToken) {
				WriteUnauthorized(w)
				return
			}
			next(w, r.WithContext(authz.WithUser(r.Context(), authz.ServicePrincipal())))
			return
		}
		if a.sessions != nil {
			if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
				if p, err := a.sessions.ResolveSession(r.Context(), c.Value); err == nil {
					if !isSafeMethod(r.Method) && !a.csrfOK(r) {
						WriteJSONError(w, http.StatusForbidden, "forbidden",
							"cross-site request refused: send X-Requested-With: gtb or a same-origin Origin header")
						return
					}
					next(w, r.WithContext(authz.WithUser(r.Context(), p)))
					return
				}
			}
		}
		if a.cfg.AllowInsecureNoAuth {
			next(w, r.WithContext(authz.WithUser(r.Context(), authz.InsecurePrincipal())))
			return
		}
		next(w, r)
	}
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func (a *Auth) csrfOK(r *http.Request) bool {
	if r.Header.Get("X-Requested-With") == CSRFHeaderValue {
		return true
	}
	origin := strings.TrimRight(r.Header.Get("Origin"), "/")
	if origin == "" || origin == "null" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, allowed := range a.cfg.Auth.AllowedOrigins {
		if strings.EqualFold(allowed, origin) || strings.EqualFold(allowed, u.Host) {
			return true
		}
	}
	return false
}

// RequireCapability lets the request through only when the principal holds
// capability (admin implies all): no principal -> 401, missing capability ->
// 403 {"error":"forbidden"}. authz.CapPublic passes everything through.
func RequireCapability(capability string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if capability == authz.CapPublic {
			next(w, r)
			return
		}
		p, ok := authz.FromContext(r.Context())
		if !ok {
			WriteUnauthorized(w)
			return
		}
		if !p.Has(capability) {
			WriteJSONError(w, http.StatusForbidden, "forbidden", "this action requires the \""+capability+"\" capability")
			return
		}
		next(w, r)
	}
}

// RequireAuth guards a non-/api handler (cmd/mcp's HTTP transport) with the
// service token only: Authorization: Bearer <API_TOKEN>, or nothing when
// ALLOW_INSECURE_NO_AUTH=true. With no API_TOKEN configured every request
// is refused - auth is on by default.
func RequireAuth(cfg *configuration.Configuration, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg != nil && cfg.AllowInsecureNoAuth {
			next(w, r)
			return
		}
		token, ok := bearerToken(r)
		if cfg == nil || cfg.APIToken == "" || !ok || !ConstantTimeEquals(token, cfg.APIToken) {
			WriteUnauthorized(w)
			return
		}
		next(w, r.WithContext(authz.WithUser(r.Context(), authz.ServicePrincipal())))
	}
}

// ExtractBearerToken returns the Authorization: Bearer token, or "".
func ExtractBearerToken(r *http.Request) string {
	t, _ := bearerToken(r)
	return t
}

func bearerToken(r *http.Request) (string, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", false
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		if token := strings.TrimSpace(parts[1]); token != "" {
			return token, true
		}
	}
	return "", false
}

// ConstantTimeEquals compares two strings in constant time.
func ConstantTimeEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// WriteUnauthorized writes the standard 401 JSON.
func WriteUnauthorized(w http.ResponseWriter) {
	WriteJSONError(w, http.StatusUnauthorized, "unauthorized", "sign in required")
}

// WriteJSONError writes {"error": code, "message": message}.
func WriteJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}

// IsHTTPS reports whether the request came over HTTPS: r.TLS, or
// X-Forwarded-Proto: https when trustProxy (AUTH.TRUST_PROXY) is on.
func IsHTTPS(r *http.Request, trustProxy bool) bool {
	if r.TLS != nil {
		return true
	}
	return trustProxy && strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

// SetSessionCookie writes the gtb_session cookie (HttpOnly, SameSite=Lax,
// Path=/, Secure over HTTPS).
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time, trustProxy bool) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: token, Path: "/", Expires: expires,
		MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: IsHTTPS(r, trustProxy),
	})
}

// ClearSessionCookie deletes the gtb_session cookie.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request, trustProxy bool) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(0, 0),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: IsHTTPS(r, trustProxy),
	})
}

// WarnIfInsecure logs the loud ALLOW_INSECURE_NO_AUTH warning (every startup).
func WarnIfInsecure(cfg *configuration.Configuration) {
	if cfg != nil && cfg.AllowInsecureNoAuth {
		log.Printf("!!!!!!!! WARNING: ALLOW_INSECURE_NO_AUTH=true - AUTHENTICATION IS DISABLED. " +
			"Every request acts as an admin. Never expose this process to a network. !!!!!!!!")
	}
}
