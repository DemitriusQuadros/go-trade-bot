package cfaccess_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go-trade-bot/internal/cfaccess"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const aud = "aud-tag-123"

type env struct {
	key     *rsa.PrivateKey
	srv     *httptest.Server
	fetches *int32
	issuer  string
	v       *cfaccess.Verifier
}

func newEnv(t *testing.T) env {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var fetches int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&fetches, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "k1", "kty": "RSA", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	issuer := "https://team.cloudflareaccess.com"
	return env{key: key, srv: srv, fetches: &fetches, issuer: issuer, v: cfaccess.NewVerifierWithURLs(issuer, srv.URL, aud)}
}

func (e env) sign(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

func (e env) claims() jwt.MapClaims {
	return jwt.MapClaims{"aud": []string{aud}, "iss": e.issuer, "exp": time.Now().Add(time.Hour).Unix(), "email": "friend@example.com"}
}

func call(h http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "203.0.113.9:5555"
	if token != "" {
		req.Header.Set(cfaccess.HeaderName, token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

func TestMiddleware(t *testing.T) {
	e := newEnv(t)
	h := e.v.Middleware(okHandler)

	assert.Equal(t, http.StatusOK, call(h, "/api/strategy", e.sign(t, e.key, "k1", e.claims())).Code)

	rec := call(h, "/api/strategy", "")
	assert.Equal(t, http.StatusForbidden, rec.Code, "missing header")
	assert.Contains(t, rec.Body.String(), `"error":"access_required"`)

	c := e.claims()
	c["aud"] = []string{"someone-else"}
	assert.Equal(t, http.StatusForbidden, call(h, "/", e.sign(t, e.key, "k1", c)).Code, "wrong aud")

	c = e.claims()
	c["iss"] = "https://evil.cloudflareaccess.com"
	assert.Equal(t, http.StatusForbidden, call(h, "/", e.sign(t, e.key, "k1", c)).Code, "wrong iss")

	c = e.claims()
	c["exp"] = time.Now().Add(-time.Hour).Unix()
	assert.Equal(t, http.StatusForbidden, call(h, "/", e.sign(t, e.key, "k1", c)).Code, "expired")

	c = e.claims()
	delete(c, "exp")
	assert.Equal(t, http.StatusForbidden, call(h, "/", e.sign(t, e.key, "k1", c)).Code, "exp required")

	c = e.claims()
	c["nbf"] = time.Now().Add(time.Hour).Unix()
	assert.Equal(t, http.StatusForbidden, call(h, "/", e.sign(t, e.key, "k1", c)).Code, "not yet valid")

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, call(h, "/", e.sign(t, other, "k1", e.claims())).Code, "bad signature")

	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, e.claims())
	hs.Header["kid"] = "k1"
	hsTok, _ := hs.SignedString([]byte("secret"))
	assert.Equal(t, http.StatusForbidden, call(h, "/", hsTok).Code, "alg must be RS256")

	// The CF_Authorization cookie also works.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cfaccess.CookieName, Value: e.sign(t, e.key, "k1", e.claims())})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestKeysAreCachedAndRefreshBounded(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	e.v.Now = func() time.Time { return now }
	good := e.sign(t, e.key, "k1", e.claims())
	require.NoError(t, e.v.Verify(t.Context(), good))
	require.NoError(t, e.v.Verify(t.Context(), good))
	assert.Equal(t, int32(1), atomic.LoadInt32(e.fetches), "keys are cached")

	unknown := e.sign(t, e.key, "k2", e.claims())
	assert.Error(t, e.v.Verify(t.Context(), unknown))
	assert.Error(t, e.v.Verify(t.Context(), unknown))
	assert.Equal(t, int32(1), atomic.LoadInt32(e.fetches), "no refetch within 5 minutes")

	now = now.Add(cfaccess.MinRefreshInterval)
	assert.Error(t, e.v.Verify(t.Context(), unknown))
	assert.Equal(t, int32(2), atomic.LoadInt32(e.fetches), "unknown kid refreshes after the interval")
}

func TestMetricsExemptOnlyForPrivateSources(t *testing.T) {
	e := newEnv(t)
	h := e.v.Middleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "192.168.1.10:9999"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	assert.Equal(t, http.StatusForbidden, call(h, "/metrics", "").Code, "public source")

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	req.Header.Set("Cf-Connecting-IP", "198.51.100.1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "came through the tunnel")
}

func TestWrapDisabledWhenUnset(t *testing.T) {
	h := cfaccess.Wrap("", "x", okHandler)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}
