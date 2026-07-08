package components

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// Level meter colors: green → amber → red LED zones on a dark panel.
var (
	meterPanel = color.NRGBA{R: 0x0A, G: 0x0C, B: 0x0E, A: 0xFF}
	meterGreen = color.NRGBA{R: 0x37, G: 0xD6, B: 0x5A, A: 0xFF}
	meterAmber = color.NRGBA{R: 0xE1, G: 0xB9, B: 0x3B, A: 0xFF}
	meterRed   = color.NRGBA{R: 0xE1, G: 0x4B, B: 0x4B, A: 0xFF}
)

const meterSegments = 16

// LevelMeter is a segmented LED level meter (0..1) with a falling peak-hold
// indicator. It lays out vertically by default (segment 0 at the bottom) or
// horizontally when SetHorizontal(true) is set (segment 0 at the left) — the
// latter suits a compact/phone layout where the meter sits along the bottom.
type LevelMeter struct {
	widget.BaseWidget
	level      float64
	peak       float64
	horizontal bool

	panel *canvas.Rectangle
	cells [meterSegments]*canvas.Rectangle
}

// NewLevelMeter returns an empty meter.
func NewLevelMeter() *LevelMeter {
	m := &LevelMeter{}
	m.ExtendBaseWidget(m)
	return m
}

// SetHorizontal switches the meter between the default vertical orientation and
// a horizontal one (segments running left→right). Safe to call before or after
// the renderer exists.
func (m *LevelMeter) SetHorizontal(h bool) {
	if m.horizontal == h {
		return
	}
	m.horizontal = h
	m.Refresh()
}

// SetLevel sets the current level (clamped to 0..1) and updates the peak-hold,
// which falls slowly toward the level on subsequent calls.
func (m *LevelMeter) SetLevel(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	m.level = v
	if v >= m.peak {
		m.peak = v
	} else {
		m.peak = maxF(v, m.peak-0.015)
	}
	if m.panel != nil {
		m.Refresh()
	}
}

// segColor returns the lit color for the segment at fractional height f.
func segColor(f float64) color.NRGBA {
	switch {
	case f >= 0.85:
		return meterRed
	case f >= 0.60:
		return meterAmber
	default:
		return meterGreen
	}
}

func (m *LevelMeter) CreateRenderer() fyne.WidgetRenderer {
	m.panel = canvas.NewRectangle(meterPanel)
	m.panel.CornerRadius = 5
	objs := []fyne.CanvasObject{m.panel}
	for i := range meterSegments {
		c := canvas.NewRectangle(dimColor(segColor((float64(i) + 0.5) / meterSegments)))
		c.CornerRadius = 2
		m.cells[i] = c
		objs = append(objs, c)
	}
	return &meterRenderer{m: m, objects: objs}
}

type meterRenderer struct {
	m       *LevelMeter
	objects []fyne.CanvasObject
}

func (r *meterRenderer) Destroy() {}

func (r *meterRenderer) MinSize() fyne.Size {
	if r.m.horizontal {
		return fyne.NewSize(180, 22)
	}
	return fyne.NewSize(22, 180)
}

func (r *meterRenderer) Layout(size fyne.Size) {
	r.m.panel.Resize(size)
	r.m.panel.Move(fyne.NewPos(0, 0))

	const padX, padY, gap = 6, 6, 3
	n := float32(meterSegments)
	if r.m.horizontal {
		cellW := (size.Width - 2*padX - gap*(n-1)) / n
		h := size.Height - 2*padY
		for i := range meterSegments {
			// segment 0 sits at the left
			x := padX + float32(i)*(cellW+gap)
			r.m.cells[i].Resize(fyne.NewSize(cellW, h))
			r.m.cells[i].Move(fyne.NewPos(x, padY))
		}
		return
	}
	cellH := (size.Height - 2*padY - gap*(n-1)) / n
	w := size.Width - 2*padX
	for i := range meterSegments {
		// segment 0 sits at the bottom
		y := size.Height - padY - float32(i+1)*cellH - float32(i)*gap
		r.m.cells[i].Resize(fyne.NewSize(w, cellH))
		r.m.cells[i].Move(fyne.NewPos(padX, y))
	}
}

func (r *meterRenderer) Refresh() {
	peakIdx := int(r.m.peak * meterSegments)
	for i := range meterSegments {
		f := (float64(i) + 0.5) / meterSegments
		lit := f <= r.m.level
		base := segColor(f)
		switch {
		case i == peakIdx && r.m.peak > 0:
			r.m.cells[i].FillColor = base // peak-hold dot stays bright
		case lit:
			r.m.cells[i].FillColor = base
		default:
			r.m.cells[i].FillColor = dimColor(base)
		}
		r.m.cells[i].Refresh()
	}
	r.m.panel.Refresh()
}

func (r *meterRenderer) Objects() []fyne.CanvasObject { return r.objects }

// dimColor returns a darkened version of an LED color for unlit segments.
func dimColor(c color.NRGBA) color.NRGBA {
	return color.NRGBA{R: c.R / 6, G: c.G / 6, B: c.B / 6, A: 0xFF}
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
