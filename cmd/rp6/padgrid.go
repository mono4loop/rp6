package main

import (
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"github.com/mono4loop/rp6/internal/ui/components"
	"github.com/mono4loop/rp6/p6"
)

const banksPerPage = p6.NumBanks / 2 // 4

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

// bankPad maps a grid (page, row, col) to a P-6 0-based bank and 1-based pad.
func bankPad(page, row, col int) (bank, number int) {
	return page*banksPerPage + row, col + 1
}

// densePad maps a single-page (row, col) to bank/pad when all 8 banks are shown
// at once (row = bank A..H, col = pad 1..6).
func densePad(row, col int) (bank, number int) {
	return row, col + 1
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

// newPadGrid configures the generic pad grid for the P-6: a 4x6 grid paged into
// banks A-D and E-H. onTrigger receives a 0-based bank and 1-based pad number;
// badges returns the effect icons for a pad. When dense is true, all 8 banks are
// shown on a single page with half-size pads.
func newPadGrid(dense bool, onTrigger func(bank, number int), badges func(bank, number int) []image.Image) *components.PadGrid {
	cell := func(page, row, col int) (bank, number int) {
		if dense {
			return densePad(row, col)
		}
		return bankPad(page, row, col)
	}
	cfg := components.PadGridConfig{
		Rows:       banksPerPage,   // 4 banks per page
		Cols:       p6.PadsPerBank, // 6 pads
		Pages:      []string{"Banks A – D", "Banks E – H"},
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
	if dense {
		cfg.Rows = p6.NumBanks // all 8 banks A-H at once
		cfg.Pages = []string{"Banks A – H"}
		cfg.CellMinSize = fyne.NewSize(22, 22) // half of the normal 44 floor
	}
	return components.NewPadGrid(cfg)
}
