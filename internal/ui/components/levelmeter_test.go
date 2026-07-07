package components

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
)

func TestLevelMeterClampAndPeak(t *testing.T) {
	test.NewApp()
	m := NewLevelMeter()

	m.SetLevel(2.0) // clamps to 1
	assert.Equal(t, 1.0, m.level)
	assert.Equal(t, 1.0, m.peak)

	m.SetLevel(-1) // clamps to 0
	assert.Equal(t, 0.0, m.level)
	// peak holds above the new level (falls slowly, not instantly to 0)
	assert.Greater(t, m.peak, 0.0)
}
