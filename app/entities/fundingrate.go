package entities

import "time"

// FundingRate stores historical 8-hour perpetual futures funding rates
// (00:00, 08:00, 16:00 UTC) per symbol.
type FundingRate struct {
	ID          uint      `gorm:"primaryKey"`
	Symbol      string    `gorm:"size:32;not null;uniqueIndex:idx_funding_symbol_time"`
	FundingTime time.Time `gorm:"not null;uniqueIndex:idx_funding_symbol_time"`
	Rate        float64   `gorm:"not null"`
	MarkPrice   float64   `gorm:"default:0"`
	CreatedAt   time.Time
}
