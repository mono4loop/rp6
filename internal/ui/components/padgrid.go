package components

import (
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// PadGridConfig configures a PadGrid.
type PadGridConfig struct {
	Rows int
	Cols int

	// Pages holds one page-selector button label per page (length = page
	// count, must be >= 1). If a single page is given, no selector is shown.
	Pages []string

	// PageAccent tints the backlit page-selector toggles (default amber).
	PageAccent color.NRGBA

	// Cell returns the label and fill color for a cell on a given page.
	Cell func(page, row, col int) (label string, fill color.Color)

	// Badges optionally returns the badge icons for a cell (nil for none).
	Badges func(page, row, col int) []image.Image

	// OnTrigger fires when a pad is tapped (after it becomes selected).
	OnTrigger func(page, row, col int)

	// CellMinSize optionally overrides each pad's minimum size (default 44x44),
	// e.g. smaller for a dense all-banks layout.
	CellMinSize fyne.Size
}

// PadGrid is a generic grid of pads with optional paging and a single-selection
// highlight. It knows nothing about MIDI or any specific device; the caller
// supplies per-cell labels/colors and handles taps via the config callbacks.
type PadGrid struct {
	cfg      PadGridConfig
	pads     []*Pad
	pageBtns []*RackToggle
	page     int

	hasSel                  bool
	selPage, selRow, selCol int

	obj fyne.CanvasObject
}

// NewPadGrid builds a pad grid from cfg.
func NewPadGrid(cfg PadGridConfig) *PadGrid {
	if cfg.Rows <= 0 {
		cfg.Rows = 1
	}
	if cfg.Cols <= 0 {
		cfg.Cols = 1
	}
	if len(cfg.Pages) == 0 {
		cfg.Pages = []string{""}
	}
	if cfg.PageAccent == (color.NRGBA{}) {
		cfg.PageAccent = lcdText
	}

	g := &PadGrid{cfg: cfg}
	g.pads = make([]*Pad, cfg.Rows*cfg.Cols)
	objs := make([]fyne.CanvasObject, len(g.pads))
	for i := range g.pads {
		p := NewPad("", color.Black, nil)
		if cfg.CellMinSize.Width > 0 && cfg.CellMinSize.Height > 0 {
			p.SetMinSize(cfg.CellMinSize)
		}
		g.pads[i] = p
		objs[i] = p
	}
	grid := container.NewGridWithColumns(cfg.Cols, objs...)

	if len(cfg.Pages) > 1 {
		btns := make([]fyne.CanvasObject, len(cfg.Pages))
		g.pageBtns = make([]*RackToggle, len(cfg.Pages))
		for i, label := range cfg.Pages {
			idx := i
			b := NewRackToggle(label, cfg.PageAccent, func() { g.ShowPage(idx) })
			g.pageBtns[i] = b
			btns[i] = b
		}
		pageBar := container.NewGridWithColumns(len(cfg.Pages), btns...)
		g.obj = container.NewBorder(pageBar, nil, nil, nil, grid)
	} else {
		g.obj = grid
	}

	g.ShowPage(0)
	return g
}

// Object returns the CanvasObject to place in a layout.
func (g *PadGrid) Object() fyne.CanvasObject { return g.obj }

// Page returns the currently shown page index.
func (g *PadGrid) Page() int { return g.page }

// Pads returns the pads in row-major order (useful for tests).
func (g *PadGrid) Pads() []*Pad { return g.pads }

// Select selects a cell (switching to its page if needed) without firing
// OnTrigger and without the tap-flash — cheap enough to reflect an external
// trigger in a large/secondary window. Returns the newly selected pad and the
// previously selected one (nil if none / different page), so the caller can
// refresh just those two on its canvas.
func (g *PadGrid) Select(page, row, col int) (now, prev *Pad) {
	if page < 0 || page >= len(g.cfg.Pages) || row < 0 || row >= g.cfg.Rows || col < 0 || col >= g.cfg.Cols {
		return nil, nil
	}
	if g.hasSel && g.selPage == g.page {
		prev = g.pads[g.selRow*g.cfg.Cols+g.selCol]
	}
	if g.page != page {
		g.ShowPage(page)
		prev = nil // page switch already refreshed everything
	}
	g.hasSel = true
	g.selPage, g.selRow, g.selCol = page, row, col
	now = g.pads[row*g.cfg.Cols+col]
	now.SetSelected(true)
	if prev != nil && prev != now {
		prev.SetSelected(false)
	}
	return now, prev
}

// Highlight selects and flashes a cell (switching to its page if needed) without
// firing OnTrigger — used to reflect an external trigger (e.g. a hardware pad
// press) in the UI.
func (g *PadGrid) Highlight(page, row, col int) {
	if now, _ := g.Select(page, row, col); now != nil {
		now.Flash()
	}
}

// FlashPad briefly lights a cell (the tap-flash) to reflect an external trigger
// WITHOUT changing the current page or the selection — the caller only sees a
// blink. If the cell is not on the currently shown page it is a no-op (there is
// nothing visible to flash). Use this (rather than Highlight) when a remote
// event should be visible but must not disturb the local UI state.
func (g *PadGrid) FlashPad(page, row, col int) {
	if page != g.page || row < 0 || row >= g.cfg.Rows || col < 0 || col >= g.cfg.Cols {
		return
	}
	g.pads[row*g.cfg.Cols+col].Flash()
}

// RefreshBadges re-pulls each visible pad's badges from the Badges config.
func (g *PadGrid) RefreshBadges() {
	if g.cfg.Badges == nil {
		return
	}
	for r := 0; r < g.cfg.Rows; r++ {
		for c := 0; c < g.cfg.Cols; c++ {
			g.pads[r*g.cfg.Cols+c].SetBadges(g.cfg.Badges(g.page, r, c))
		}
	}
}

// ShowPage switches to the given page, refreshing labels, colors, tap handlers,
// the page-selector highlight, and the selection.
func (g *PadGrid) ShowPage(page int) {
	if page < 0 || page >= len(g.cfg.Pages) {
		return
	}
	g.page = page
	for r := 0; r < g.cfg.Rows; r++ {
		for c := 0; c < g.cfg.Cols; c++ {
			label := ""
			var fill color.Color = color.Black
			if g.cfg.Cell != nil {
				label, fill = g.cfg.Cell(page, r, c)
			}
			p := g.pads[r*g.cfg.Cols+c]
			p.Set(label, fill)
			if g.cfg.Badges != nil {
				p.SetBadges(g.cfg.Badges(page, r, c))
			}
			pr, pc := r, c
			p.OnTapped = func() { g.handleTap(page, pr, pc) }
		}
	}
	for i, b := range g.pageBtns {
		b.SetOn(i == page)
	}
	g.refreshSelection()
}

func (g *PadGrid) handleTap(page, row, col int) {
	g.hasSel = true
	g.selPage, g.selRow, g.selCol = page, row, col
	g.refreshSelection()
	if g.cfg.OnTrigger != nil {
		g.cfg.OnTrigger(page, row, col)
	}
}

func (g *PadGrid) refreshSelection() {
	for r := 0; r < g.cfg.Rows; r++ {
		for c := 0; c < g.cfg.Cols; c++ {
			sel := g.hasSel && g.selPage == g.page && g.selRow == r && g.selCol == c
			g.pads[r*g.cfg.Cols+c].SetSelected(sel)
		}
	}
}
