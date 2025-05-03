package entities

import "time"

type Account struct {
	ID              int64     `gorm:"primaryKey"`
	Amount          float32   `gorm:"not null"`
	AvailableOrders int64     `gorm:"not null"`
	Currency        string    `gorm:"not null"`
	CreatedAt       time.Time `gorm:"autoCreateTime"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime"`
}
