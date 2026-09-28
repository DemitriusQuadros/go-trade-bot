package candle

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"go-trade-bot/app/entities"
)

// Coverage is the stored candle span for one (symbol, timeframe): the first
// and last OpenTime and the row count (fix-02 B2).
type Coverage struct {
	Symbol    string
	Timeframe string
	From      time.Time
	To        time.Time
	Count     int64
}

// coverageRow is the raw grouped-query row. MIN/MAX(open_time) come back as
// time.Time from Postgres but as text from SQLite (an aggregate has no
// declared column type), so they scan through flexTime.
type coverageRow struct {
	Symbol      string
	Timeframe   string
	MinOpen     flexTime
	MaxOpen     flexTime
	CandleCount int64
}

// Coverage returns every stored (symbol, timeframe) span in one grouped
// query, ordered by symbol then descending count. symbol == "" returns all
// symbols.
func (r CandleRepository) Coverage(ctx context.Context, symbol string) ([]Coverage, error) {
	q := r.db.WithContext(ctx).
		Model(&entities.Candle{}).
		Select("symbol, timeframe, MIN(open_time) AS min_open, MAX(open_time) AS max_open, COUNT(*) AS candle_count")
	if symbol != "" {
		q = q.Where("symbol = ?", symbol)
	}
	var rows []coverageRow
	if err := q.Group("symbol, timeframe").Order("symbol ASC, candle_count DESC, timeframe ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Coverage, 0, len(rows))
	for _, row := range rows {
		out = append(out, Coverage{
			Symbol:    row.Symbol,
			Timeframe: row.Timeframe,
			From:      row.MinOpen.Time.UTC(),
			To:        row.MaxOpen.Time.UTC(),
			Count:     row.CandleCount,
		})
	}
	return out, nil
}

// FormatCoverage renders coverage as `1h 2021-01-01..2026-09-20 (36267), 1d
// ...` (the symbol is omitted - callers name it), or "none" when empty.
func FormatCoverage(cov []Coverage) string {
	if len(cov) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(cov))
	for _, c := range cov {
		parts = append(parts, fmt.Sprintf("%s %s..%s (%d)", c.Timeframe, c.From.Format("2006-01-02"), c.To.Format("2006-01-02"), c.Count))
	}
	return strings.Join(parts, ", ")
}

// flexTime scans a timestamp delivered either as time.Time or as text.
type flexTime struct{ Time time.Time }

var flexTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// Scan implements sql.Scanner.
func (f *flexTime) Scan(v any) error {
	switch t := v.(type) {
	case nil:
		f.Time = time.Time{}
		return nil
	case time.Time:
		f.Time = t
		return nil
	case []byte:
		return f.parse(string(t))
	case string:
		return f.parse(t)
	}
	return fmt.Errorf("candle coverage: cannot scan %T as a timestamp", v)
}

// Value implements driver.Valuer (unused for writes; keeps the type symmetric).
func (f flexTime) Value() (driver.Value, error) { return f.Time, nil }

func (f *flexTime) parse(s string) error {
	s = strings.TrimSpace(s)
	for _, layout := range flexTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			f.Time = t
			return nil
		}
	}
	return fmt.Errorf("candle coverage: unrecognised timestamp %q", s)
}
