package candlesource

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"go-trade-bot/internal/exchange"
)

// ListingResolver finds the first month Binance has data for a symbol, so a
// dataset can start "from the beginning" without a hardcoded date. It reads the
// archive bucket's S3 listing and, when a REST fetcher is available, refines
// the answer to the exact first candle. Results are cached per (symbol, tf).
type ListingResolver struct {
	Client  *http.Client
	Fetcher exchange.HistoricalKlineFetcher // optional refinement / fallback
	// BaseURL is the S3 bucket endpoint; overridable for tests.
	BaseURL string

	mu    sync.Mutex
	cache map[string]time.Time
}

const defaultListingURL = "https://s3-ap-northeast-1.amazonaws.com/data.binance.vision"

func NewListingResolver(c *http.Client, f exchange.HistoricalKlineFetcher) *ListingResolver {
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	return &ListingResolver{Client: c, Fetcher: f, BaseURL: defaultListingURL, cache: map[string]time.Time{}}
}

var monthKey = regexp.MustCompile(`-(\d{4})-(\d{2})\.zip$`)

type listBucketResult struct {
	Contents []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
}

func (l *ListingResolver) ListingStart(ctx context.Context, symbol, timeframe string) (time.Time, error) {
	key := symbol + "/" + timeframe
	l.mu.Lock()
	if t, ok := l.cache[key]; ok {
		l.mu.Unlock()
		return t, nil
	}
	l.mu.Unlock()

	month, err := l.firstArchiveMonth(ctx, symbol, timeframe)
	if err != nil && l.Fetcher == nil {
		return time.Time{}, err
	}
	start := month
	if l.Fetcher != nil {
		// exact first candle: from the archive month if known, else from 2017-07
		from := month
		if err != nil {
			from = time.Date(2017, 7, 1, 0, 0, 0, 0, time.UTC)
		}
		ks, ferr := l.Fetcher.ListKlineRange(ctx, symbol, timeframe, from, time.Now().UTC(), 1)
		if ferr != nil {
			if err != nil {
				return time.Time{}, fmt.Errorf("listing start for %s: archive: %v; rest: %w", symbol, err, ferr)
			}
		} else if len(ks) > 0 {
			start, err = ks[0].OpenTime.UTC(), nil
		}
	}
	if err != nil {
		return time.Time{}, err
	}
	l.mu.Lock()
	l.cache[key] = start
	l.mu.Unlock()
	return start, nil
}

func (l *ListingResolver) firstArchiveMonth(ctx context.Context, symbol, timeframe string) (time.Time, error) {
	url := fmt.Sprintf("%s?delimiter=/&prefix=data/spot/monthly/klines/%s/%s/", l.BaseURL, symbol, timeframe)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	resp, err := l.Client.Do(req)
	if err != nil {
		return time.Time{}, fmt.Errorf("listing %s/%s: %w", symbol, timeframe, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("listing %s/%s: status %d", symbol, timeframe, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return time.Time{}, err
	}
	var res listBucketResult
	if err := xml.Unmarshal(body, &res); err != nil {
		return time.Time{}, fmt.Errorf("listing %s/%s: %w", symbol, timeframe, err)
	}
	var first time.Time
	for _, c := range res.Contents {
		m := monthKey.FindStringSubmatch(c.Key)
		if m == nil {
			continue
		}
		t, err := time.Parse("2006-01", m[1]+"-"+m[2])
		if err == nil && (first.IsZero() || t.Before(first)) {
			first = t
		}
	}
	if first.IsZero() {
		return time.Time{}, fmt.Errorf("no archive months listed for %s/%s", symbol, timeframe)
	}
	return first.UTC(), nil
}
