package binancearchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T, zipBytes []byte, checksum string, zipStatus int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case len(r.URL.Path) > 9 && r.URL.Path[len(r.URL.Path)-9:] == ".CHECKSUM":
			if checksum == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(checksum + "  file.zip\n"))
		default:
			if zipStatus != 0 && zipStatus != http.StatusOK {
				w.WriteHeader(zipStatus)
				return
			}
			_, _ = w.Write(zipBytes)
		}
	}))
	t.Cleanup(srv.Close)
	withTestBaseURL(t, srv.URL)
	old := dailyBaseURL
	dailyBaseURL = srv.URL
	t.Cleanup(func() { dailyBaseURL = old })
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestFetchVerified_OKMonthlyAndDaily(t *testing.T) {
	z := buildZip(t, "x.csv", "1704067200000,1,2,0.5,1.5,10,1704070799999,0,0,0,0,0\n")
	serve(t, z, sha(z), 0)
	m := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	cs, ref, err := FetchVerified(context.Background(), http.DefaultClient, "BTCUSDT", "1h", m, false)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "BTCUSDT-1h-2024-01.zip sha256:"+sha(z), ref)

	_, ref, err = FetchVerified(context.Background(), http.DefaultClient, "BTCUSDT", "1h", m, true)
	require.NoError(t, err)
	assert.Contains(t, ref, "BTCUSDT-1h-2024-01-01.zip")
}

func TestFetchVerified_ChecksumMismatchReturnsNothing(t *testing.T) {
	z := buildZip(t, "x.csv", "1704067200000,1,2,0.5,1.5,10,0,0,0,0,0,0\n")
	serve(t, z, sha([]byte("other")), 0)

	cs, _, err := FetchVerified(context.Background(), http.DefaultClient, "BTCUSDT", "1h", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), false)

	require.ErrorIs(t, err, ErrChecksum)
	assert.Nil(t, cs)
}

func TestFetchVerified_MissingChecksumOrZipFailsClosed(t *testing.T) {
	z := buildZip(t, "x.csv", "1704067200000,1,2,0.5,1.5,10,0,0,0,0,0,0\n")
	m := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	serve(t, z, "", 0)
	_, _, err := FetchVerified(context.Background(), http.DefaultClient, "BTCUSDT", "1h", m, false)
	assert.ErrorIs(t, err, ErrNotFound, "no .CHECKSUM")

	serve(t, z, sha(z), http.StatusNotFound)
	_, _, err = FetchVerified(context.Background(), http.DefaultClient, "BTCUSDT", "1h", m, false)
	assert.ErrorIs(t, err, ErrNotFound, "no zip")

	serve(t, z, sha(z), http.StatusBadGateway)
	_, _, err = FetchVerified(context.Background(), http.DefaultClient, "BTCUSDT", "1h", m, false)
	var se *StatusError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, http.StatusBadGateway, se.Code)
}
