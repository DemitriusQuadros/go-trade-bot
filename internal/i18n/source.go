package i18n

import (
	"context"
	"log"
	"sync"
	"time"
)

// Source provides the shared-output locale (Settings.DefaultLocale). It
// never fails: implementations fall back to Default.
type Source interface {
	DefaultLocale(ctx context.Context) Locale
}

// Static is a fixed Source.
type Static Locale

// DefaultLocale implements Source.
func (s Static) DefaultLocale(context.Context) Locale { return ParseOr(string(s), Default) }

// DefaultCacheTTL is how long CachedSource keeps a value (i18n-02 §4).
const DefaultCacheTTL = 60 * time.Second

// fetchTimeout bounds one settings read.
const fetchTimeout = 2 * time.Second

// CachedSource reads the default locale through fetch and caches it for TTL.
// A fetch error or an unknown/empty value yields Default (en); the failure is
// cached for TTL too, so a broken settings store is not hammered.
type CachedSource struct {
	fetch func(ctx context.Context) (string, error)
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	value   Locale
	fetched time.Time
	loaded  bool
}

// NewCachedSource builds a CachedSource. ttl <= 0 means DefaultCacheTTL.
func NewCachedSource(fetch func(ctx context.Context) (string, error), ttl time.Duration) *CachedSource {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &CachedSource{fetch: fetch, ttl: ttl, now: time.Now, value: Default}
}

// DefaultLocale implements Source.
func (s *CachedSource) DefaultLocale(ctx context.Context) Locale {
	if s == nil || s.fetch == nil {
		return Default
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.loaded && now.Sub(s.fetched) < s.ttl {
		return s.value
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	raw, err := s.fetch(fctx)
	s.loaded, s.fetched = true, now
	if err != nil {
		log.Printf("i18n: could not read the default locale (using %s): %v", Default, err)
		s.value = Default
		return s.value
	}
	s.value = ParseOr(raw, Default)
	return s.value
}
