package entities_test

import (
	"go-trade-bot/app/entities"
	"testing"

	"github.com/magiconair/properties/assert"
)

func TestValidAlgorithm(t *testing.T) {
	assert.Equal(t, true, entities.IsValidAlgorithm("grid"))
}

func TestInvalidAlgorithm(t *testing.T) {
	assert.Equal(t, false, entities.IsValidAlgorithm("test"))
}

func TestValidCycle(t *testing.T) {
	assert.Equal(t, true, entities.IsValidCycle(10))
}

func TestInvalidCycle(t *testing.T) {
	assert.Equal(t, false, entities.IsValidCycle(22))
}
