package components

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLEDSetColorAndLit(t *testing.T) {
	test.NewApp()
	red := color.NRGBA{R: 0xE1, G: 0x4B, B: 0x4B, A: 0xFF}
	green := color.NRGBA{R: 0x37, G: 0xD6, B: 0x5A, A: 0xFF}

	l := NewLED(red)
	assert.Equal(t, red, l.col)
	assert.True(t, l.lit)

	l.SetColor(green)
	assert.Equal(t, green, l.col)

	l.SetLit(false)
	assert.False(t, l.lit)
}

// TestLEDDestroyStopsPulse verifies the renderer's Destroy() tears down the
// breathing goroutine, so a widget torn down without an explicit StopPulse()
// doesn't leak a ticker forever (efhb).
func TestLEDDestroyStopsPulse(t *testing.T) {
	test.NewApp()
	l := NewLED(color.NRGBA{A: 0xFF})
	r := l.CreateRenderer()

	l.StartPulse()
	l.mu.Lock()
	pulsing := l.pulsing
	l.mu.Unlock()
	require.True(t, pulsing, "StartPulse should start the pulse")

	r.Destroy()
	l.mu.Lock()
	pulsing = l.pulsing
	l.mu.Unlock()
	assert.False(t, pulsing, "Destroy should stop the pulse goroutine")
}
