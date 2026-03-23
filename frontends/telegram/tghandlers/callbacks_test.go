package tghandlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStepMonth_Forward(t *testing.T) {
	month, year := stepMonth(3, 2025, 1)
	assert.Equal(t, 4, month)
	assert.Equal(t, 2025, year)
}

func TestStepMonth_Backward(t *testing.T) {
	month, year := stepMonth(5, 2025, -1)
	assert.Equal(t, 4, month)
	assert.Equal(t, 2025, year)
}

func TestStepMonth_DecemberToJanuary(t *testing.T) {
	month, year := stepMonth(12, 2024, 1)
	assert.Equal(t, 1, month)
	assert.Equal(t, 2025, year)
}

func TestStepMonth_JanuaryToDecember(t *testing.T) {
	month, year := stepMonth(1, 2025, -1)
	assert.Equal(t, 12, month)
	assert.Equal(t, 2024, year)
}

func TestStepMonth_NoChange(t *testing.T) {
	month, year := stepMonth(6, 2025, 0)
	assert.Equal(t, 6, month)
	assert.Equal(t, 2025, year)
}

func TestStepMonth_LargeForwardDelta(t *testing.T) {
	// The function adds delta and only checks for a single rollover.
	// November + 3 = 14 > 12, so returns (1, next year).
	month, year := stepMonth(11, 2024, 3)
	assert.Equal(t, 1, month)
	assert.Equal(t, 2025, year)
}
