package strategies

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type noopStrategy struct{ name string }

func (s noopStrategy) Name() string                       { return s.name }
func (s noopStrategy) Before(ctx Context)                 {}
func (s noopStrategy) ShouldLong(ctx Context) bool        { return false }
func (s noopStrategy) GoLong(ctx Context) Signal          { return Signal{} }
func (s noopStrategy) ShouldShort(ctx Context) bool       { return false }
func (s noopStrategy) GoShort(ctx Context) Signal         { return Signal{} }
func (s noopStrategy) UpdatePosition(ctx Context) *Signal { return nil }
func (s noopStrategy) After(ctx Context)                  {}
func (s noopStrategy) Terminate(ctx Context)              {}

func TestRegister_And_Get(t *testing.T) {
	resetForTest()
	defer resetForTest()

	Register("noop-a", func() Strategy { return noopStrategy{name: "noop-a"} })

	got, ok := Get("noop-a")
	assert.True(t, ok)
	assert.Equal(t, "noop-a", got.Name())
}

func TestGet_NotFound(t *testing.T) {
	resetForTest()
	defer resetForTest()

	got, ok := Get("does-not-exist")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestExists(t *testing.T) {
	resetForTest()
	defer resetForTest()

	Register("noop-b", func() Strategy { return noopStrategy{name: "noop-b"} })
	assert.True(t, Exists("noop-b"))
	assert.False(t, Exists("nope"))
}

func TestRegister_DuplicatePanics(t *testing.T) {
	resetForTest()
	defer resetForTest()

	Register("dup", func() Strategy { return noopStrategy{name: "dup"} })
	assert.PanicsWithValue(t, `strategies: duplicate registration for "dup"`, func() {
		Register("dup", func() Strategy { return noopStrategy{name: "dup"} })
	})
}

func TestNames_SortedAndComplete(t *testing.T) {
	resetForTest()
	defer resetForTest()

	Register("scalping", func() Strategy { return noopStrategy{name: "scalping"} })
	Register("grid", func() Strategy { return noopStrategy{name: "grid"} })
	Register("bollinger", func() Strategy { return noopStrategy{name: "bollinger"} })

	assert.Equal(t, []string{"bollinger", "grid", "scalping"}, Names())
}
