package main

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFillRowsLayoutDistributesExtraHeight(t *testing.T) {
	a := canvas.NewRectangle(color.White)
	b := canvas.NewRectangle(color.White)
	gap := canvas.NewRectangle(color.Transparent)
	a.SetMinSize(fyne.NewSize(10, 20))
	b.SetMinSize(fyne.NewSize(10, 20))
	gap.SetMinSize(fyne.NewSize(0, 10))
	layout := &fillRowsLayout{fixedLast: true}
	layout.Layout([]fyne.CanvasObject{a, b, gap}, fyne.NewSize(100, 100))

	assert.Equal(t, float32(41), a.Size().Height)
	assert.Equal(t, float32(41), b.Size().Height)
	assert.Equal(t, float32(10), gap.Size().Height)
	assert.Equal(t, float32(45), b.Position().Y)
	assert.Equal(t, float32(90), gap.Position().Y)
}

func TestSequencerRowsGrowInTallRack(t *testing.T) {
	u := newTestUI(t)
	u.setConsole(true)
	u.onSeqDock(true)
	u.seqRack.Object().Resize(fyne.NewSize(700, 900))
	u.seqRack.Object().Refresh()

	first := u.seqRack.cells[0][0]
	require.True(t, first.Visible())
	assert.Greater(t, first.Size().Height, first.MinSize().Height, "step row should use available rack height")
	assert.Equal(t, u.seqRack.blocks[0].Size().Height, u.seqRack.trackBtns[0].Size().Height, "assign key fills its track row")
}
