package components

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/assert"
)

func TestRackPanelPadsAroundContent(t *testing.T) {
	test.NewApp()
	content := widget.NewLabel("x")
	p := NewRackPanel(content)

	// Rendering the panel exercises CreateRenderer/Layout without panicking.
	w := test.NewWindow(p)
	defer w.Close()
	w.Resize(fyne.NewSize(300, 60))

	ps := p.MinSize()
	cs := content.MinSize()
	assert.Greater(t, ps.Width, cs.Width, "panel adds horizontal padding")
	assert.Greater(t, ps.Height, cs.Height, "panel adds vertical padding")
}
