package entities

import "time"

type Symbol struct {
	Symbol    string
	Price     float64
	CreatedAt time.Time
}
