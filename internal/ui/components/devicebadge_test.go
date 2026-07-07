package components

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
)

func TestDeviceBadgeStateAndName(t *testing.T) {
	test.NewApp()
	amber := color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}

	b := NewDeviceBadge("P-6", "USB MIDI", amber)
	assert.Equal(t, DeviceOffline, b.State())

	// Realize the widget so the renderer (and its canvas objects) exist.
	w := test.NewWindow(b)
	defer w.Close()

	b.SetState(DeviceOnline)
	assert.Equal(t, DeviceOnline, b.State())
	// Online lights the LED in the accent color.
	assert.Equal(t, amber, b.led.Color())
	assert.True(t, b.led.lit)

	b.SetState(DeviceOffline)
	assert.False(t, b.led.lit, "offline dims the LED")

	b.SetName("EMULATOR", "SOFTWARE")
	assert.Equal(t, "EMULATOR", b.nm.Text)
	assert.Equal(t, "SOFTWARE", b.tg.Text)
}

func TestDeviceBadgeFixedSize(t *testing.T) {
	test.NewApp()
	amber := color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}

	short := NewDeviceBadge("P-6", "USB MIDI", amber)
	long := NewDeviceBadge("EMULATOR", "SOFTWARE", amber)

	// Footprint is constant regardless of the (much longer) emulator label.
	assert.Equal(t, fyne.NewSize(badgeWidth, badgeHeight), short.MinSize())
	assert.Equal(t, short.MinSize(), long.MinSize())
}

func TestDeviceBadgeTapToggles(t *testing.T) {
	test.NewApp()
	amber := color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}
	b := NewDeviceBadge("P-6", "USB MIDI", amber)

	toggles, settings := 0, 0
	b.OnToggle(func() { toggles++ })
	b.OnSettings(func() { settings++ })

	// A plain tap on the plate switches the backend.
	b.Tapped(&fyne.PointEvent{})
	assert.Equal(t, 1, toggles, "a tap toggles the backend")

	b.Tapped(&fyne.PointEvent{})
	assert.Equal(t, 2, toggles, "each tap toggles again")

	assert.Equal(t, 0, settings, "toggling never fires the settings action")
}
