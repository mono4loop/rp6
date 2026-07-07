package components

import (
	"fmt"
	"image/color"
	"testing"

	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
)

func TestPadGridPagingAndSelection(t *testing.T) {
	test.NewApp()

	var triggered [3]int // page,row,col of last trigger
	got := false
	g := NewPadGrid(PadGridConfig{
		Rows: 2, Cols: 3,
		Pages: []string{"P1", "P2"},
		Cell: func(page, row, col int) (string, color.Color) {
			return fmt.Sprintf("%d:%d,%d", page, row, col), color.Black
		},
		OnTrigger: func(page, row, col int) {
			triggered = [3]int{page, row, col}
			got = true
		},
	})

	// Page 0 labels.
	assert.Equal(t, "0:0,0", g.Pads()[0].Label())
	assert.Equal(t, "0:1,2", g.Pads()[5].Label())

	g.ShowPage(1)
	assert.Equal(t, 1, g.Page())
	assert.Equal(t, "1:0,0", g.Pads()[0].Label())

	// Tap the last pad on page 1 -> row 1, col 2.
	g.Pads()[5].OnTapped()
	assert.True(t, got)
	assert.Equal(t, [3]int{1, 1, 2}, triggered)
	assert.True(t, g.Pads()[5].Selected())

	// Selection is per (page,row,col): switching pages hides the highlight.
	g.ShowPage(0)
	assert.False(t, g.Pads()[5].Selected())
	g.ShowPage(1)
	assert.True(t, g.Pads()[5].Selected())
}

func TestPadGridHighlight(t *testing.T) {
	test.NewApp()

	fired := false
	g := NewPadGrid(PadGridConfig{
		Rows: 2, Cols: 3,
		Pages: []string{"P1", "P2"},
		Cell: func(page, row, col int) (string, color.Color) {
			return fmt.Sprintf("%d:%d,%d", page, row, col), color.Black
		},
		OnTrigger: func(page, row, col int) { fired = true },
	})

	// Highlight switches to the target page and selects the cell, without
	// firing OnTrigger (it reflects an external trigger).
	g.Highlight(1, 1, 2)
	assert.Equal(t, 1, g.Page(), "switched to the pad's page")
	assert.True(t, g.Pads()[5].Selected())
	assert.False(t, fired, "Highlight must not fire OnTrigger")

	// Out-of-range is ignored.
	g.Highlight(9, 9, 9)
	assert.Equal(t, 1, g.Page())
}
