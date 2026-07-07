package components

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/stretchr/testify/assert"
)

func TestTransportButtonTapAndLit(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme())

	tapped := 0
	b := NewPlayButton(func() { tapped++ })
	w := test.NewWindow(b)
	defer w.Close()
	w.Resize(fyne.NewSize(120, 60))

	test.Tap(b)
	assert.Equal(t, 1, tapped)

	assert.False(t, b.lit)
	b.SetLit(true)
	assert.True(t, b.lit)
	b.SetLit(true) // idempotent
	assert.True(t, b.lit)
}

func TestWalkerToggle(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme())

	var running bool
	b := NewWalkerToggle(func(r bool) { running = r })
	w := test.NewWindow(b)
	defer w.Close()
	w.Resize(fyne.NewSize(120, 60))

	// It renders a footprint trail rather than a triangle/square.
	assert.Len(t, b.prints, numPrints)
	assert.Nil(t, b.iconImg)
	assert.Nil(t, b.stopImg)

	// Tapping toggles the running state and (only then) walks.
	test.Tap(b)
	assert.True(t, running)
	assert.True(t, b.running)
	assert.NotNil(t, b.walkAnim, "trail animates while running")

	test.Tap(b)
	assert.False(t, running)
	assert.False(t, b.running)
	assert.Nil(t, b.walkAnim, "trail rests when stopped")
	assert.Zero(t, b.walkPhase, "phase reset to a faint even trail")
}
