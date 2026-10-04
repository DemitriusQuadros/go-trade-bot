// Package cfaccess verifies Cloudflare Access JWTs (auth-01 §4, defense in
// depth): with CF_ACCESS.TEAM_DOMAIN and CF_ACCESS.AUD set, every request
// must carry a valid Cf-Access-Jwt-Assertion header (or CF_Authorization
// cookie) signed by the team's keys, so anyone reaching the homelab port
// directly - around the tunnel - gets 403 {"error":"access_required"}.
package cfaccess

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// HeaderName / CookieName carry the Access JWT.
const (
	HeaderName = "Cf-Access-Jwt-Assertion"
	CookieName = "CF_Authorization"
	// MinRefreshInterval bounds how often the certs are re-fetched.
	MinRefreshInterval = 5 * time.Minute
)

// Verifier checks Access JWTs against the team's JWKS.
type Verifier struct {
	aud      string
	issuer   string
	certsURL string
	client   *http.Client
	// Now is injectable for tests.
	Now func() time.Time

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	lastFetch time.Time
}

// NewVerifier builds a Verifier for teamDomain (e.g.
// "myteam.cloudflareaccess.com") and the application audience tag.
func NewVerifier(teamDomain, aud string) *Verifier {
	teamDomain = strings.TrimSuffix(strings.TrimPrefix(teamDomain, "https://"), "/")
	return NewVerifierWithURLs("https://"+teamDomain, "https://"+teamDomain+"/cdn-cgi/access/certs", aud)
}

// NewVerifierWithURLs builds a Verifier with an explicit issuer and certs
// URL (tests serve the JWKS from an httptest.Server).
func NewVerifierWithURLs(issuer, certsURL, aud string) *Verifier {
	return &Verifier{
		aud: aud, issuer: issuer, certsURL: certsURL,
		client: &http.Client{Timeout: 10 * time.Second},
		Now:    time.Now, keys: map[string]*rsa.PublicKey{},
	}
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func (v *Verifier) fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.certsURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cf-access: certs returned %d", resp.StatusCode)
	}
	var set jwks
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&set); err != nil {
		return err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		nb, err1 := base64.RawURLEncoding.DecodeString(k.N)
		eb, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(eb) == 0 || len(eb) > 4 {
			continue
		}
		e := 0
		for _, b := range eb {
			e = e<<8 | int(b)
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
	}
	if len(keys) == 0 {
		return errors.New("cf-access: certs contain no RSA keys")
	}
	v.keys = keys
	return nil
}

// key returns the key for kid, re-fetching the certs when kid is unknown
// (at most once per MinRefreshInterval).
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	now := v.Now()
	if v.lastFetch.IsZero() || now.Sub(v.lastFetch) >= MinRefreshInterval {
		v.lastFetch = now
		if err := v.fetch(ctx); err != nil {
			log.Printf("cf-access: fetching certs failed: %v", err)
			return nil, err
		}
		if k, ok := v.keys[kid]; ok {
			return k, nil
		}
	}
	return nil, fmt.Errorf("cf-access: unknown key id %q", kid)
}

// Verify validates token: RS256 with a team key, aud contains the AUD, iss
// is the team URL, exp present and not passed, nbf (if any) reached.
func (v *Verifier) Verify(ctx context.Context, token string) error {
	if token == "" {
		return errors.New("cf-access: missing token")
	}
	_, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return v.key(ctx, kid)
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(v.aud),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(v.Now),
		jwt.WithLeeway(30*time.Second),
	)
	return err
}

// Middleware rejects requests without a valid Access JWT with 403
// {"error":"access_required"}. /metrics is exempt only for loopback/private
// source addresses that did not come through Cloudflare (no
// Cf-Connecting-IP header) - cloudflared itself connects from loopback.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" && isPrivateSource(r) {
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get(HeaderName)
		if token == "" {
			if c, err := r.Cookie(CookieName); err == nil {
				token = c.Value
			}
		}
		if err := v.Verify(r.Context(), token); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "access_required", "message": "a valid Cloudflare Access session is required",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isPrivateSource(r *http.Request) bool {
	if r.Header.Get("Cf-Connecting-IP") != "" || r.Header.Get("Cf-Ray") != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// Wrap returns next wrapped by the verifier when both values are set;
// otherwise it logs "cf-access: disabled" once and returns next unchanged.
func Wrap(teamDomain, aud string, next http.Handler) http.Handler {
	if teamDomain == "" || aud == "" {
		log.Printf("cf-access: disabled")
		return next
	}
	log.Printf("cf-access: enabled for team %s", teamDomain)
	return NewVerifier(teamDomain, aud).Middleware(next)
}
