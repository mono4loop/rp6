package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"fyne.io/fyne/v2"
	"github.com/mono4loop/rp6/internal/ui/components"
	"github.com/mono4loop/rp6/p6"
)

const banksPerPage = p6.NumBanks / 2 // 4

// padLayout selects how the 48 pads are paged across the grid.
type padLayout int

const (
	// layoutPaged shows 4 banks per page across 2 tabs (A–D, E–H). Default.
	layoutPaged padLayout = iota
	// layoutTwoBank shows 2 banks per page across 4 tabs (A–B, C–D, E–F, G–H).
	layoutTwoBank
	// layoutDense shows all 8 banks on one page with half-size pads.
	layoutDense
	// layoutTempoPad mirrors a Synido TempoPAD C16: a 4×4 grid paged across four
	// bank buttons (A–D). It maps the C16's note-linear layout onto RP6's pads
	// (bank A→ids 0-15, B→16-31, C→32-47); bank D has no RP6 pad and is empty.
	layoutTempoPad
)

// numLayouts is the number of pad layouts the layout selector cycles through.
const numLayouts = 4

// TempoPad grid geometry: a 4×4 pad grid, 16 pads per bank page.
const (
	tempoRows     = 4
	tempoCols     = 4
	tempoBankPads = tempoRows * tempoCols // 16
)

// banksForLayout reports how many banks are shown per page in a layout.
func banksForLayout(l padLayout) int {
	switch l {
	case layoutTwoBank:
		return 2
	case layoutDense:
		return p6.NumBanks
	default:
		return banksPerPage
	}
}

// Connection status LED colors.
var (
	ledGreen = color.NRGBA{R: 0x37, G: 0xD6, B: 0x5A, A: 0xFF}
	ledRed   = color.NRGBA{R: 0xE1, G: 0x4B, B: 0x4B, A: 0xFF}
)

// Device-badge accent colors: amber signals real P-6 hardware, cyan signals the
// software emulator (a clear "this isn't the metal" cue).
var (
	deviceHwAccent  = color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}
	deviceEmuAccent = color.NRGBA{R: 0x3B, G: 0xC9, B: 0xE1, A: 0xFF}
	// storeAccent tints the pad-rack store toggle — a cool blue, kin to the
	// emulator badge's cyan, marking the "download more packs" action.
	storeAccent = color.NRGBA{R: 0x3B, G: 0x82, B: 0xE1, A: 0xFF}
	// p6PlateColor is the P-6's yellow chassis color, used to tint the P-6-only
	// rack plate so it echoes the physical unit (black controls on yellow).
	p6PlateColor = color.NRGBA{R: 0xE0, G: 0xB0, B: 0x2C, A: 0xFF}
)

// bankColors gives each bank (A-H) a distinct, high-contrast color.
var bankColors = [p6.NumBanks]color.Color{
	color.NRGBA{R: 0xE1, G: 0x4B, B: 0x4B, A: 0xFF}, // A  red
	color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}, // B  orange
	color.NRGBA{R: 0xC9, G: 0xA2, B: 0x27, A: 0xFF}, // C  gold
	color.NRGBA{R: 0x4C, G: 0xAF, B: 0x50, A: 0xFF}, // D  green
	color.NRGBA{R: 0x26, G: 0xA6, B: 0x9A, A: 0xFF}, // E  teal
	color.NRGBA{R: 0x44, G: 0x77, B: 0xDD, A: 0xFF}, // F  blue
	color.NRGBA{R: 0x7E, G: 0x57, B: 0xC2, A: 0xFF}, // G  purple
	color.NRGBA{R: 0xD2, G: 0x4B, B: 0x9C, A: 0xFF}, // H  magenta
}

// cellBankPad maps a grid (page, row, col) to a P-6 0-based bank and 1-based pad
// for the given layout. Each page holds banksForLayout(layout) banks stacked as
// rows, so bank = page*banksForLayout(l) + row and pad = col+1. (Not used for
// layoutTempoPad, which maps cells to pad ids directly — see cellPadID.)
func cellBankPad(l padLayout, page, row, col int) (bank, number int) {
	return page*banksForLayout(l) + row, col + 1
}

// gridDims returns the pad grid's (rows, cols) for a layout.
func gridDims(l padLayout) (rows, cols int) {
	if l == layoutTempoPad {
		return tempoRows, tempoCols
	}
	return banksForLayout(l), p6.PadsPerBank
}

// cellPadID maps a grid cell to a 0-based RP6 pad id, or -1 for an empty cell
// (only the TempoPad view's bank D produces empties, since RP6 has just 48 pads).
func cellPadID(l padLayout, page, row, col int) int {
	if l == layoutTempoPad {
		// Mirror the Synido C16 note layout: the bottom-left pad is the lowest
		// note, rows stack upward, banks are 16 pads each (A:0-15, B:16-31,
		// C:32-47). This matches the synido-c16.midimap note→pad mapping.
		id := page*tempoBankPads + (tempoRows-1-row)*tempoCols + col
		if id >= p6.NumPads {
			return -1
		}
		return id
	}
	bank, number := cellBankPad(l, page, row, col)
	return padID(bank, number)
}

// padID gives each of the 48 pads a stable 0-based id (used by the effects
// engine); number is 1-based.
func padID(bank, number int) int { return bank*p6.PadsPerBank + (number - 1) }

// padBankNumber is the inverse of padID.
func padBankNumber(id int) (bank, number int) {
	return id / p6.PadsPerBank, id%p6.PadsPerBank + 1
}

// padLabelForID returns the "A1".."H6" label for a pad id, or "—" if unassigned.
func padLabelForID(id int) string {
	if id < 0 {
		return "—"
	}
	bank, number := padBankNumber(id)
	return p6.PadLabel(bank, number)
}

// bankColorForID returns the bank color for a pad id (nil if unassigned).
func bankColorForID(id int) color.Color {
	if id < 0 {
		return nil
	}
	bank, _ := padBankNumber(id)
	return bankColors[bank]
}

// bankNRGBAForID is bankColorForID as a concrete NRGBA (for accent-tinted
// widgets like RackToggle), falling back to the P-6 amber for an unassigned id.
func bankNRGBAForID(id int) color.NRGBA {
	if c, ok := bankColorForID(id).(color.NRGBA); ok {
		return c
	}
	return deviceHwAccent
}

// pagesForLayout returns the page-selector labels for a layout.
func pagesForLayout(l padLayout) []string {
	switch l {
	case layoutTwoBank:
		return []string{"A – B", "C – D", "E – F", "G – H"}
	case layoutDense:
		return []string{"Banks A – H"}
	case layoutTempoPad:
		return []string{"A", "B", "C", "D"}
	default:
		return []string{"Banks A – D", "Banks E – H"}
	}
}

// newPadGrid configures the generic pad grid for the P-6 in the given layout:
//   - layoutPaged:    4 banks/page, 2 tabs (A–D, E–H)
//   - layoutTwoBank:  2 banks/page, 4 tabs (A–B, C–D, E–F, G–H)
//   - layoutDense:    all 8 banks on one page with half-size pads
//   - layoutTempoPad: a 4×4 grid, 4 bank tabs (A–D), mirroring a Synido C16
//
// onTrigger receives a 0-based bank and 1-based pad number; badges returns the
// effect icons for a pad. Empty cells (TempoPad bank D) are dark and inert.
func newPadGrid(layout padLayout, onTrigger func(bank, number int), badges func(bank, number int) []image.Image) *components.PadGrid {
	rows, cols := gridDims(layout)
	emptyFill := color.NRGBA{R: 0x1E, G: 0x1E, B: 0x1E, A: 0xFF}
	cfg := components.PadGridConfig{
		Rows:       rows,
		Cols:       cols,
		Pages:      pagesForLayout(layout),
		PageAccent: deviceHwAccent, // match the rack toggles
		Cell: func(page, row, col int) (string, color.Color) {
			id := cellPadID(layout, page, row, col)
			if id < 0 {
				return "", emptyFill
			}
			bank, number := padBankNumber(id)
			return p6.PadLabel(bank, number), bankColors[bank]
		},
		Badges: func(page, row, col int) []image.Image {
			id := cellPadID(layout, page, row, col)
			if id < 0 {
				return nil
			}
			bank, number := padBankNumber(id)
			return badges(bank, number)
		},
		OnTrigger: func(page, row, col int) {
			id := cellPadID(layout, page, row, col)
			if id < 0 {
				return
			}
			bank, number := padBankNumber(id)
			onTrigger(bank, number)
		},
		CellMinPixels: 80,
		CellMaxPixels: 130,
	}
	if layout == layoutDense {
		// Dense mode uses a separate compact range so all
		// eight banks remain visible without scrolling.
		cfg.CellMinPixels = 65
		cfg.CellMaxPixels = 75
	}
	return components.NewPadGrid(cfg)
}

// layoutIcons holds one icon per padLayout (indexed by the layout value), used
// by the pad rack's RackCycle layout selector. Each icon is a small grid whose
// row count matches the banks-per-page of that layout (2 / 4 / 8), so denser
// layouts look denser.
var layoutIcons = buildLayoutIcons()

func buildLayoutIcons() []fyne.Resource {
	icons := make([]fyne.Resource, numLayouts)
	names := []string{"layout-paged.png", "layout-twobank.png", "layout-dense.png", "layout-tempopad.png"}
	for l := range numLayouts {
		if padLayout(l) == layoutTempoPad {
			icons[l] = gridIconResource(names[l], tempoRows, tempoCols) // 4×4
			continue
		}
		icons[l] = gridIconResource(names[l], banksForLayout(padLayout(l)), 3)
	}
	return icons
}

// gridIconResource renders a monochrome grid icon with rows×cols cells and wraps
// it as a Fyne resource (drawn oversized for clean downscaling).
func gridIconResource(name string, rows, cols int) fyne.Resource {
	const (
		size = 48 // canvas is square
		pad  = 5  // outer padding
		gap  = 3  // gap between cells
	)
	fg := color.NRGBA{R: 0xE0, G: 0xE0, B: 0xE0, A: 0xFF}

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	inner := float64(size - 2*pad)
	cellW := (inner - float64(cols-1)*gap) / float64(cols)
	cellH := (inner - float64(rows-1)*gap) / float64(rows)
	for r := range rows {
		for c := range cols {
			x0 := pad + float64(c)*(cellW+gap)
			y0 := pad + float64(r)*(cellH+gap)
			fillRect(img, x0, y0, cellW, cellH, fg)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return fyne.NewStaticResource(name, buf.Bytes())
}

// fillRect paints a solid rectangle onto img at the given float bounds.
func fillRect(img *image.NRGBA, x0, y0, w, h float64, c color.NRGBA) {
	x1, y1 := int(x0+w+0.5), int(y0+h+0.5)
	for y := int(y0 + 0.5); y < y1; y++ {
		for x := int(x0 + 0.5); x < x1; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

// keyboardIcon is a small piano-keyboard glyph for the pad rack's "route to
// keyboard" tool toggle (light-grey white keys with a few darker black keys).
var keyboardIcon = buildKeyboardIcon()

func buildKeyboardIcon() fyne.Resource {
	const (
		size  = 48
		pad   = 6
		white = 7 // white keys
	)
	fg := color.NRGBA{R: 0xE0, G: 0xE0, B: 0xE0, A: 0xFF} // white keys
	bk := color.NRGBA{R: 0x60, G: 0x60, B: 0x60, A: 0xFF} // black keys

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	inner := float64(size - 2*pad)
	keyW := inner / float64(white)
	// White keys as vertical bars with a thin transparent gap between them.
	for i := range white {
		x0 := pad + float64(i)*keyW
		fillRect(img, x0, float64(pad), keyW-1, inner, fg)
	}
	// Black keys sit on the top ~55%, between white keys (skip the E–F, B–C gaps).
	blackH := inner * 0.55
	for _, i := range []int{0, 1, 3, 4, 5} { // gaps after white keys 0,1,3,4,5
		cx := pad + float64(i+1)*keyW
		fillRect(img, cx-keyW*0.3, float64(pad), keyW*0.6, blackH, bk)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return fyne.NewStaticResource("keyboard.png", buf.Bytes())
}
