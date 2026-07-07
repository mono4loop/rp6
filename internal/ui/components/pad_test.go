package components

import (
	"image"
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
)

func TestPadBadges(t *testing.T) {
	test.NewApp()
	p := NewPad("A1", color.NRGBA{R: 0xE1, G: 0x4B, B: 0x4B, A: 0xFF}, nil)
	w := test.NewWindow(p)
	defer w.Close()
	w.Resize(fyne.NewSize(120, 120))

	assert.Equal(t, 0, p.BadgeCount())

	icon := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	p.SetBadges([]image.Image{icon, icon})
	assert.Equal(t, 2, p.BadgeCount())

	// capped at four
	p.SetBadges([]image.Image{icon, icon, icon, icon, icon, icon})
	assert.Equal(t, 4, p.BadgeCount())

	p.SetBadges(nil)
	assert.Equal(t, 0, p.BadgeCount())
}
