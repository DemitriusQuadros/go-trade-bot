package entities

import (
	"time"
)

type StrategyStatus string

const (
	Open   StrategyStatus = "open"
	Closed StrategyStatus = "closed"
)

type OrderOperation string

const (
	Buy  OrderOperation = "buy"
	Sell OrderOperation = "sell"
)

type Signal struct {
	ID         uint `gorm:"primaryKey"`
	Symbol     string
	Strategy   Strategy `gorm:"foreignKey:StrategyID"`
	StrategyID uint
	CreatedAt  time.Time
	Status     StrategyStatus
	Orders     []Order `gorm:"foreignKey:SignalID"`
}

type Order struct {
	ID             uint `gorm:"primaryKey"`
	SignalID       uint
	Price          float32
	Quantity       float32
	OrderOperation OrderOperation
	CreatedAt      time.Time
}
