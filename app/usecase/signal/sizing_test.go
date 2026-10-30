package usecase

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPositionSizer_NilConfig(t *testing.T) {
	sizer := NewDefaultPositionSizer()
	amount, err := sizer.Size(nil, 300)
	require.NoError(t, err)
	assert.Equal(t, 300.0, amount)
}

func TestPositionSizer_FixedAmount_Clamped(t *testing.T) {
	sizer := NewDefaultPositionSizer()
	cfg := &PositionSizingConfig{
		Type:  SizingFixedAmount,
		Value: 500,
	}
	amount, err := sizer.Size(cfg, 300)
	require.NoError(t, err)
	assert.Equal(t, 300.0, amount)
}

func TestPositionSizer_FixedAmount_UnderAvailable(t *testing.T) {
	sizer := NewDefaultPositionSizer()
	cfg := &PositionSizingConfig{
		Type:  SizingFixedAmount,
		Value: 100,
	}
	amount, err := sizer.Size(cfg, 300)
	require.NoError(t, err)
	assert.Equal(t, 100.0, amount)
}

func TestPositionSizer_PctCapital_Valid(t *testing.T) {
	sizer := NewDefaultPositionSizer()
	cfg := &PositionSizingConfig{
		Type:  SizingPctCapital,
		Value: 10,
	}
	amount, err := sizer.Size(cfg, 1000)
	require.NoError(t, err)
	assert.Equal(t, 100.0, amount)
}

func TestPositionSizer_PctCapital_OutOfRange(t *testing.T) {
	sizer := NewDefaultPositionSizer()
	cfg := &PositionSizingConfig{
		Type:  SizingPctCapital,
		Value: 150,
	}
	_, err := sizer.Size(cfg, 1000)
	require.Error(t, err)

	cfgNegative := &PositionSizingConfig{
		Type:  SizingPctCapital,
		Value: -5,
	}
	_, err = sizer.Size(cfgNegative, 1000)
	require.Error(t, err)
}

func TestPositionSizer_ZeroAvailable(t *testing.T) {
	sizer := NewDefaultPositionSizer()
	cfg := &PositionSizingConfig{
		Type:  SizingFixedAmount,
		Value: 100,
	}
	amount, err := sizer.Size(cfg, 0)
	require.NoError(t, err)
	assert.Equal(t, 0.0, amount)
}

func TestResolveEntryQty(t *testing.T) {
	tests := []struct {
		name                          string
		req, price, fallback, ceiling float64
		wantQty                       float64
		wantClamped                   bool
	}{
		{"script qty within ceiling is honoured", 5, 100, 100, 1000, 5, false},
		{"script qty above ceiling is clamped", 20, 100, 100, 1000, 10, true},
		{"no script qty falls back", 0, 100, 250, 1000, 2.5, false},
		{"negative script qty falls back", -1, 100, 250, 1000, 2.5, false},
		{"zero ceiling clamps to zero", 5, 100, 100, 0, 0, true},
		{"bad price yields zero", 5, 0, 100, 1000, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveEntryQty(tc.req, tc.price, tc.fallback, tc.ceiling)
			if got.Qty != tc.wantQty || got.Clamped != tc.wantClamped {
				t.Fatalf("got %+v, want qty=%v clamped=%v", got, tc.wantQty, tc.wantClamped)
			}
		})
	}
}
