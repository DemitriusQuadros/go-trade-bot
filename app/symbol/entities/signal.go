package entities

import (
	"time"

	"github.com/google/uuid"
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
	ID                uuid.UUID
	Symbol            string
	StrategyExecution StrategyExecution
	CreatedAt         time.Time
	Status            StrategyStatus
	Orders            []Order
}

type Order struct {
	ID             uuid.UUID
	Price          float32
	OrderOperation OrderOperation
	CreatedAt      time.Time
}
