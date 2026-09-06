package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFunctionKeyHelpers(t *testing.T) {
	assert.True(t, isFunctionKey("<F1>"))
	assert.True(t, isFunctionKey("<F6>"))
	assert.False(t, isFunctionKey("<F7>"))
	assert.False(t, isFunctionKey("q"))

	assert.Equal(t, 0, functionKeyIndex("<F1>"))
	assert.Equal(t, 5, functionKeyIndex("<F6>"))
	assert.Equal(t, -1, functionKeyIndex("<Escape>"))
}
