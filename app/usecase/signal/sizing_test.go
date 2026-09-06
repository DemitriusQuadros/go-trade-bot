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
