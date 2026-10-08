package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSequencerRowsStaySquareInTallRack(t *testing.T) {
	u := newTestUI(t)
	u.setConsole(true)
	u.onSeqDock(true)
	u.seqRack.Object().Resize(fyne.NewSize(700, 900))
	u.seqRack.Object().Refresh()

	first := u.seqRack.cells[0][0]
	require.True(t, first.Visible())
	assert.InDelta(t, first.Size().Width, first.Size().Height, 0.01, "step remains square")
	assert.LessOrEqual(t, first.Size().Width, float32(50), "step does not stretch beyond its physical cap at scale 1")
	assert.Equal(t, u.seqRack.blocks[0].Size().Height, u.seqRack.trackBtns[0].Size().Height, "assign key fills its track row")
}

func TestSixOneBarTracksDoNotScroll(t *testing.T) {
	u := newTestUI(t)
	u.setConsole(true) // the console reserves six rows (seq(rows: 6))
	u.setVisible(u.seqRack.Object(), u.seqBtn, true)
	u.seqRack.SetTrackCount(6)
	// Two passes: the reserved height follows the width the rows were laid out at.
	for range 2 {
		minimum := u.seqRack.Object().MinSize()
		u.seqRack.Object().Resize(fyne.NewSize(1000, minimum.Height))
		u.seqRack.Object().Refresh()
	}

	assert.LessOrEqual(t, u.seqRack.tracks.Size().Height, u.seqRack.trackBox.Size().Height,
		"natural sequencer height includes all tracks, gaps and trailing spacer")
	assert.True(t, u.seqRack.blocks[5].Visible(), "sixth track remains visible")
}

// TestSequencerRowCapScrolls: the P-6 window reserves three step rows
// (seq(rows: 3)), so a fourth track scrolls inside the rack instead of raising
// the window's minimum, and the scroller can reach the real bottom row.
func TestSequencerRowCapScrolls(t *testing.T) {
	u := newTestUI(t) // the P-6 backend, windowed
	require.Equal(t, "window", u.activeVariant)
	require.Equal(t, 3, u.seqRack.reservedRows)
	u.seqRack.SetTrackCount(4)
	capped := u.seqRack.Object().MinSize().Height

	u.seqRack.SetTrackCount(8)
	assert.Equal(t, capped, u.seqRack.Object().MinSize().Height, "more tracks don't raise the minimum")

	u.seqRack.Object().Resize(fyne.NewSize(850, capped))
	u.seqRack.Object().Refresh()
	u.seqRack.Object().Refresh() // the scroll content height follows the laid-out width
	last := u.seqRack.blocks[7]
	bottom := last.Position().Y + last.Size().Height
	assert.Greater(t, bottom, u.seqRack.trackBox.Size().Height, "tracks past the cap overflow the viewport")
	assert.GreaterOrEqual(t, u.seqRack.tracks.MinSize().Height, bottom, "the scroller can reach the last track")

	u.seqRack.SetReservedRows(0)
	assert.Greater(t, u.seqRack.Object().MinSize().Height, capped, "uncapped, every row is reserved")
}

func TestSequencerMultipleBarsStaySquareAndScroll(t *testing.T) {
	u := newTestUI(t)
	u.setConsole(true)
	u.onSeqDock(true)
	for track := range u.seq.Tracks() {
		u.seqRack.applyBars(track, u.seq.MaxBars())
	}
	u.seqRack.Object().Resize(fyne.NewSize(800, 500))
	u.seqRack.Object().Refresh()

	for _, index := range []int{0, 15, 16, 63} {
		cell := u.seqRack.cells[0][index]
		assert.InDelta(t, cell.Size().Width, cell.Size().Height, 0.01, "step %d remains square", index)
		assert.GreaterOrEqual(t, cell.Size().Width, float32(40))
		assert.LessOrEqual(t, cell.Size().Width, float32(50))
	}
	assert.Greater(t, u.seqRack.tracks.Size().Height, u.seqRack.trackBox.Size().Height, "multi-bar tracks overflow into the vertical scroller")
}

func TestDensePadsUseSmallerSquareCells(t *testing.T) {
	u := newTestUI(t)
	u.setLayout(layoutDense)
	u.padRackObj.Resize(fyne.NewSize(900, 760))
	u.padRackObj.Refresh()

	pad := u.grid.Pads()[0]
	assert.InDelta(t, pad.Size().Width, pad.Size().Height, 0.01)
	assert.GreaterOrEqual(t, pad.Size().Width, float32(65))
	assert.LessOrEqual(t, pad.Size().Width, float32(75))
}

func TestPadRackAdvertisesEightyPixelFloor(t *testing.T) {
	u := newTestUI(t)
	minimum := u.padRackObj.MinSize()
	// Six 80px cells + five 4px gaps + 44px rack frame.
	assert.GreaterOrEqual(t, minimum.Width, float32(544))
}

func TestFittedRacksExpandHorizontally(t *testing.T) {
	u := newTestUI(t)
	u.setVisible(u.seqRack.Object(), u.seqBtn, true)
	allocation := fyne.NewSize(1400, 900)
	u.padRackObj.Resize(allocation)
	u.seqRack.Object().Resize(allocation)
	u.padRackObj.Refresh()
	u.seqRack.Object().Refresh()

	padPanel := fittedContent(u.padRackObj)
	seqPanel := fittedContent(u.seqRack.Object())
	// Both fill vertically.
	assert.Equal(t, allocation.Height, padPanel.Size().Height)
	assert.Equal(t, allocation.Height, seqPanel.Size().Height)
	// The window variant sets both the pad rack and the sequencer to expand
	// horizontally (fill the width, no side gaps).
	assert.Equal(t, allocation.Width, padPanel.Size().Width, "pads expand horizontally in the window variant")
	assert.Equal(t, allocation.Width, seqPanel.Size().Width, "sequencer expands horizontally in the window variant")
}

func TestPadRackLeavesBottomInsetBelowGrid(t *testing.T) {
	u := newTestUI(t)
	u.padRackObj.Resize(fyne.NewSize(900, 760))
	u.padRackObj.Refresh()
	panel := fittedContent(u.padRackObj)
	gridBottom := u.padGridArea.Position().Y + u.padGridArea.Size().Height
	assert.GreaterOrEqual(t, panel.Size().Height-gridBottom, float32(20), "rack frame plus explicit bottom inset remain below pads")
}
