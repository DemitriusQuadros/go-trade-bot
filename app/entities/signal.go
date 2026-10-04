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
	StopLossOrderID string  `gorm:"type:varchar(50);"`
	StopLossPrice   float32 `gorm:"default:0"`
	// TakeProfitPrice is the strategy-supplied take-profit (0 = none). It is
	// NOT an exchange order: the engine checks it at the start of every
	// cycle and closes the position at market once price reaches it.
	TakeProfitPrice float32 `gorm:"default:0"`
	// ExitReason records why the position closed (take_profit, stop_loss,
	// simulated_stop_loss, strategy_exit, manual, ...). Empty on open orders
	// and on rows closed before this column existed.
	ExitReason     string     `gorm:"type:varchar(40)"`
	EntryPrice     float32    `gorm:"not null"`
	ExitPrice      float32    `gorm:"not null"`
	Quantity       float32    `gorm:"not null"`
	InvestedAmount float32    `gorm:"not null"`
	MarginType     MarginType `gorm:"type:varchar(10);not null"`
	EntryFee       float32    `gorm:"not null"`
	ExitFee        float32    `gorm:"not null"`
	Leverage       float32    `gorm:"not null"`
	ExecutedQty    float32    `gorm:"not null"`
	IsClosing      bool       `gorm:"default:false"`
	Profit         float32    `gorm:"not null"`
	// SimStopEvaluatedAt is the OpenTime of the last closed candle the
	// dryrun simulated-stop evaluator (app/engine.SimulatedStopEvaluator)
	// checked against StopLossPrice, so a candle is never evaluated twice
	// across cycles/replicas (fix-01). Always nil for live/paper orders.
	SimStopEvaluatedAt *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
