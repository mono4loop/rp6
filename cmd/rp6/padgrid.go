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
)

// numLayouts is the number of pad layouts the layout selector cycles through.
const numLayouts = 3

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
// rows, so bank = page*banksPerPage_layout + row and pad = col+1.
func cellBankPad(l padLayout, page, row, col int) (bank, number int) {
	return page*banksForLayout(l) + row, col + 1
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
	default:
		return []string{"Banks A – D", "Banks E – H"}
	}
}

// newPadGrid configures the generic pad grid for the P-6 in the given layout:
//   - layoutPaged:   4 banks/page, 2 tabs (A–D, E–H)
//   - layoutTwoBank: 2 banks/page, 4 tabs (A–B, C–D, E–F, G–H)
//   - layoutDense:   all 8 banks on one page with half-size pads
//
// onTrigger receives a 0-based bank and 1-based pad number; badges returns the
// effect icons for a pad.
func newPadGrid(layout padLayout, onTrigger func(bank, number int), badges func(bank, number int) []image.Image) *components.PadGrid {
	cell := func(page, row, col int) (bank, number int) {
		return cellBankPad(layout, page, row, col)
	}
	cfg := components.PadGridConfig{
		Rows:       banksForLayout(layout),
		Cols:       p6.PadsPerBank, // 6 pads
		Pages:      pagesForLayout(layout),
		PageAccent: deviceHwAccent, // match the rack toggles
		Cell: func(page, row, col int) (string, color.Color) {
			bank, number := cell(page, row, col)
			return p6.PadLabel(bank, number), bankColors[bank]
		},
		Badges: func(page, row, col int) []image.Image {
			bank, number := cell(page, row, col)
			return badges(bank, number)
		},
		OnTrigger: func(page, row, col int) {
			bank, number := cell(page, row, col)
			onTrigger(bank, number)
		},
	}
	if layout == layoutDense {
		cfg.CellMinSize = fyne.NewSize(22, 22) // half of the normal 44 floor
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
	names := []string{"layout-paged.png", "layout-twobank.png", "layout-dense.png"}
	for l := range numLayouts {
		icons[l] = gridIconResource(names[l], banksForLayout(padLayout(l)))
	}
	return icons
}

// gridIconResource renders a monochrome grid icon with rows rows × 3 columns of
// cells and wraps it as a Fyne resource (drawn oversized for clean downscaling).
func gridIconResource(name string, rows int) fyne.Resource {
	const (
		size = 48 // canvas is square
		cols = 3
		pad  = 5 // outer padding
		gap  = 3 // gap between cells
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
