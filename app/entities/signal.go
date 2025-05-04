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
	Orders     []Order      `gorm:"foreignKey:SignalID"`
}

type Order struct {
	ID             uint       `gorm:"primaryKey"`
	SignalID       uint       `gorm:"not null"`
	BrokerOrderID  string     `gorm:"type:varchar(50);"`
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
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
