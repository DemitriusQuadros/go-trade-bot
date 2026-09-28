package entities

import (
	"time"
)

type SignalStatus string

const (
	Open   SignalStatus = "open"
	Closed SignalStatus = "closed"
)

type MarginType string

const (
	Isolated MarginType = "isolated"
	Cross    MarginType = "cross"
)

type Signal struct {
	ID         uint `gorm:"primaryKey"`
	Symbol     string
	Strategy   Strategy `gorm:"foreignKey:StrategyID"`
	StrategyID uint
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Status     SignalStatus `gorm:"type:varchar(10);not null"`
	// Mode is the effective execution mode ("live", "paper", "dryrun") the
	// position was opened under (fix-01). Dryrun rows are simulated: their
	// order IDs carry exchange.SimulatedOrderIDPrefix ("SIM-") and no real
	// exchange order ever backs them. Empty on rows created before fix-01.
	Mode   string  `gorm:"type:varchar(16);default:''"`
	Orders []Order `gorm:"foreignKey:SignalID"`
}

type Order struct {
	ID            uint   `gorm:"primaryKey"`
	SignalID      uint   `gorm:"not null"`
	BrokerOrderID string `gorm:"type:varchar(50);"`
	// StopLossOrderID is the BrokerOrderID of the resting STOP_MARKET order
	// (Spec 03); empty if none/not-yet-triggered/cancelled.
	StopLossOrderID string     `gorm:"type:varchar(50);"`
	StopLossPrice   float32    `gorm:"default:0"`
	EntryPrice      float32    `gorm:"not null"`
	ExitPrice       float32    `gorm:"not null"`
	Quantity        float32    `gorm:"not null"`
	InvestedAmount  float32    `gorm:"not null"`
	MarginType      MarginType `gorm:"type:varchar(10);not null"`
	EntryFee        float32    `gorm:"not null"`
	ExitFee         float32    `gorm:"not null"`
	Leverage        float32    `gorm:"not null"`
	ExecutedQty     float32    `gorm:"not null"`
	IsClosing       bool       `gorm:"default:false"`
	Profit          float32    `gorm:"not null"`
	// SimStopEvaluatedAt is the OpenTime of the last closed candle the
	// dryrun simulated-stop evaluator (app/engine.SimulatedStopEvaluator)
	// checked against StopLossPrice, so a candle is never evaluated twice
	// across cycles/replicas (fix-01). Always nil for live/paper orders.
	SimStopEvaluatedAt *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
