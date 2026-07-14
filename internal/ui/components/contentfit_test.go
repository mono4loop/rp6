package components

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	"github.com/stretchr/testify/assert"
)

// TestContentFitDefaultShrinksToFitSize verifies the wrapper sizes its content to
// FitSize (here a fixed natural size), centered horizontally, leaving slack.
func TestContentFitDefaultShrinksToFitSize(t *testing.T) {
	natural := fyne.NewSize(200, 100)
	content := canvas.NewRectangle(color.White)
	fit := NewContentFit(content, func(fyne.Size) fyne.Size { return natural })
	fit.Resize(fyne.NewSize(600, 400))
	fit.Refresh()

	assert.Equal(t, natural, content.Size(), "content stays at its natural size")
	assert.Equal(t, float32((600-200)/2), content.Position().X, "content is horizontally centered")
}

// TestContentFitExpand makes the content fill the requested axis while leaving the
// other at its natural size.
func TestContentFitExpand(t *testing.T) {
	natural := fyne.NewSize(200, 100)
	content := canvas.NewRectangle(color.White)
	fit := NewContentFit(content, func(fyne.Size) fyne.Size { return natural })
	available := fyne.NewSize(600, 400)

	fit.SetExpand(true, false)
	fit.Resize(available)
	fit.Refresh()
	assert.Equal(t, available.Width, content.Size().Width, "content fills width when expanding horizontally")
	assert.Equal(t, natural.Height, content.Size().Height, "height stays natural")

	fit.SetExpand(true, true)
	fit.Refresh()
	assert.Equal(t, available.Width, content.Size().Width)
	assert.Equal(t, available.Height, content.Size().Height, "content fills height when expanding both")

	fit.SetExpand(false, false)
	fit.Refresh()
	assert.Equal(t, natural, content.Size(), "clearing expand returns to the natural size")
}

// TestContentFitExpandDoesNotChangeMinSize guards that expanding never inflates
// the wrapper's minimum (so it can't force the parent/window larger).
func TestContentFitExpandDoesNotChangeMinSize(t *testing.T) {
	content := canvas.NewRectangle(color.White)
	content.SetMinSize(fyne.NewSize(120, 60))
	fit := NewContentFit(content, func(fyne.Size) fyne.Size { return content.MinSize() })
	fit.SetContentMin(true)
	before := fit.MinSize()
	fit.SetExpand(true, true)
	assert.Equal(t, before, fit.MinSize(), "expand does not change the advertised minimum")
}
