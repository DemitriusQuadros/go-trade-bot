package entities_test

import (
	"go-trade-bot/app/entities"
	"testing"

	"github.com/magiconair/properties/assert"
)

func TestValidCycle(t *testing.T) {
	// 10 was dropped: Binance has no 10m kline interval.
	assert.Equal(t, false, entities.IsValidCycle(10))
}

func TestInvalidCycle(t *testing.T) {
	assert.Equal(t, false, entities.IsValidCycle(22))
}
