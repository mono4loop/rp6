package components

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/stretchr/testify/assert"
)

func TestRackToggleStateAndTap(t *testing.T) {
	test.NewApp()
	amber := color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}

	taps := 0
	tg := NewRackToggle("PADS", amber, func() { taps++ })
	assert.False(t, tg.On(), "starts off")

	w := test.NewWindow(tg)
	defer w.Close()

	// Off: greyed caption, no backlight.
	assert.Equal(t, badgeNameDim, tg.txt.Color)
	assert.Equal(t, color.Transparent, tg.glow.FillColor)

	// On: lit accent caption + backlight glow.
	tg.SetOn(true)
	assert.True(t, tg.On())
	assert.Equal(t, lightenColor(amber, 0.5), tg.txt.Color)
	assert.NotEqual(t, color.Transparent, tg.glow.FillColor)

	tg.Tapped(&fyne.PointEvent{})
	assert.Equal(t, 1, taps, "tap fires the callback")
}

func TestRackToggleCtrlTap(t *testing.T) {
	test.NewApp()
	amber := color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}
	taps, alts := 0, 0
	tg := NewRackToggle("A1", amber, func() { taps++ })
	tg.SetOnCtrlTap(func() { alts++ })

	tg.Tapped(&fyne.PointEvent{}) // plain
	assert.Equal(t, 1, taps)
	assert.Equal(t, 0, alts)

	tg.MouseDown(&desktop.MouseEvent{Modifier: fyne.KeyModifierControl})
	tg.Tapped(&fyne.PointEvent{}) // ctrl
	assert.Equal(t, 1, taps, "ctrl+click must not fire the plain action")
	assert.Equal(t, 1, alts)
}

func TestRackToggleIconFades(t *testing.T) {
	test.NewApp()
	amber := color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}
	tg := NewRackToggleIcon(theme.GridIcon(), amber, nil)

	w := test.NewWindow(tg)
	defer w.Close()

	// Off: faded icon. On: full-strength icon.
	assert.Greater(t, tg.img.Translucency, 0.0, "icon fades when off")
	tg.SetOn(true)
	assert.Equal(t, 0.0, tg.img.Translucency, "icon full-strength when on")
}

func TestRackToggleFlash(t *testing.T) {
	test.NewApp()
	amber := color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}
	tg := NewRackToggle("SAVE", amber, nil)

	assert.False(t, tg.flashing)
	tg.Flash()
	assert.True(t, tg.flashing, "flash starts a momentary blink")
	tg.Flash() // idempotent while flashing
	assert.True(t, tg.flashing)
}

func TestRackToggleHoverGlow(t *testing.T) {
	test.NewApp()
	amber := color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}
	tg := NewRackToggle("FX", amber, func() {})
	w := test.NewWindow(tg)
	defer w.Close()

	// Off + not hovered: no backlight.
	assert.Equal(t, color.Transparent, tg.glow.FillColor)

	// Off + hovered: a faint accent glow appears (affordance).
	tg.MouseIn(&desktop.MouseEvent{})
	assert.NotEqual(t, color.Transparent, tg.glow.FillColor, "inactive toggle glows on hover")

	tg.MouseOut()
	assert.Equal(t, color.Transparent, tg.glow.FillColor, "glow clears when the pointer leaves")

	// Interface is satisfied so Fyne actually delivers hover events.
	var _ desktop.Hoverable = tg
}
