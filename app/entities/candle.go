package entities

import "time"

// Candle is the persisted OHLCV row. (Symbol, Timeframe, OpenTime) is unique -
// one row per candle per timeframe per symbol, ever.
type Candle struct {
	ID        uint      `gorm:"primaryKey"`
	Symbol    string    `gorm:"type:varchar(20);not null;uniqueIndex:idx_candle_symbol_tf_time"`
	Timeframe string    `gorm:"type:varchar(5);not null;uniqueIndex:idx_candle_symbol_tf_time"`
	OpenTime  time.Time `gorm:"not null;uniqueIndex:idx_candle_symbol_tf_time"`
	Open      float64   `gorm:"not null"`
	High      float64   `gorm:"not null"`
	Low       float64   `gorm:"not null"`
	Close     float64   `gorm:"not null"`
	Volume    float64   `gorm:"not null"`
	CreatedAt time.Time
}
