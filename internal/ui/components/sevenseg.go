// Package components provides reusable Fyne widgets for the rp6 UI.
package components

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// 7-segment display colors, styled after the P-6's red LED.
var (
	segColorOn  = color.NRGBA{R: 0xFF, G: 0x33, B: 0x22, A: 0xFF}
	segColorOff = color.NRGBA{R: 0x3A, G: 0x0C, B: 0x0A, A: 0xFF}
	segColorBg  = color.NRGBA{R: 0x14, G: 0x08, B: 0x08, A: 0xFF}
)

// segmentsFor maps a character to its lit segments in order a,b,c,d,e,f,g:
//
//	 aaa
//	f   b
//	 ggg
//	e   c
//	 ddd
var segmentsFor = map[rune][7]bool{
	'0': {true, true, true, true, true, true, false},
	'1': {false, true, true, false, false, false, false},
	'2': {true, true, false, true, true, false, true},
	'3': {true, true, true, true, false, false, true},
	'4': {false, true, true, false, false, true, true},
	'5': {true, false, true, true, false, true, true},
	'6': {true, false, true, true, true, true, true},
	'7': {true, true, true, false, false, false, false},
	'8': {true, true, true, true, true, true, true},
	'9': {true, true, true, true, false, true, true},
	' ': {false, false, false, false, false, false, false},
	'-': {false, false, false, false, false, false, true},
}

// segText right-aligns value across n digit positions, padding with spaces and
// keeping the least-significant digits if it overflows.
func segText(value, n int) string {
	s := fmt.Sprintf("%d", value)
	if len(s) > n {
		s = s[len(s)-n:]
	}
	for len(s) < n {
		s = " " + s
	}
	return s
}

// SevenSeg is a red 7-segment numeric display of n digits, styled after the
// Roland P-6's LED.
type SevenSeg struct {
	widget.BaseWidget
	n     int
	value int

	bg   *canvas.Rectangle
	segs [][7]*canvas.Rectangle
}

// NewSevenSeg returns a display with n digit positions.
func NewSevenSeg(n int) *SevenSeg {
	s := &SevenSeg{n: n}
	s.ExtendBaseWidget(s)
	return s
}

// SetValue updates the displayed number and repaints if needed.
func (s *SevenSeg) SetValue(v int) {
	if s.value == v {
		return
	}
	s.value = v
	if s.bg != nil {
		s.Refresh()
	}
}

// Value returns the currently displayed number.
func (s *SevenSeg) Value() int { return s.value }

// applyColors sets each segment's fill based on the current value.
func (s *SevenSeg) applyColors() {
	runes := []rune(segText(s.value, s.n))
	for d := range s.n {
		on := segmentsFor[runes[d]]
		for i := range 7 {
			c := segColorOff
			if on[i] {
				c = segColorOn
			}
			s.segs[d][i].FillColor = c
		}
	}
}

// CreateRenderer implements fyne.Widget.
func (s *SevenSeg) CreateRenderer() fyne.WidgetRenderer {
	s.bg = canvas.NewRectangle(segColorBg)
	s.bg.CornerRadius = 4
	s.segs = make([][7]*canvas.Rectangle, s.n)
	objs := []fyne.CanvasObject{s.bg}
	for d := range s.n {
		for i := range 7 {
			r := canvas.NewRectangle(segColorOff)
			s.segs[d][i] = r
			objs = append(objs, r)
		}
	}
	s.applyColors()
	return &sevenSegRenderer{seg: s, objects: objs}
}

type sevenSegRenderer struct {
	seg     *SevenSeg
	objects []fyne.CanvasObject
}

func (r *sevenSegRenderer) Destroy() {}

func (r *sevenSegRenderer) MinSize() fyne.Size {
	return fyne.NewSize(float32(r.seg.n)*20+10, 42)
}

func (r *sevenSegRenderer) Layout(size fyne.Size) {
	r.seg.bg.Resize(size)
	r.seg.bg.Move(fyne.NewPos(0, 0))

	pad := float32(5)
	gap := float32(4)
	n := float32(r.seg.n)
	cellW := (size.Width - 2*pad - (n-1)*gap) / n
	cellH := size.Height - 2*pad
	if cellW > cellH*0.7 { // keep a tall digit aspect ratio
		cellW = cellH * 0.7
	}
	t := cellW * 0.16 // segment thickness
	if t < 2 {
		t = 2
	}
	lh := cellW - 2*t       // horizontal segment length
	lv := (cellH - 3*t) / 2 // vertical segment length

	for d := range r.seg.n {
		x0 := pad + float32(d)*(cellW+gap)
		y0 := pad
		set := func(i int, x, y, w, h float32) {
			seg := r.seg.segs[d][i]
			seg.Move(fyne.NewPos(x0+x, y0+y))
			seg.Resize(fyne.NewSize(w, h))
		}
		set(0, t, 0, lh, t)            // a  top
		set(1, cellW-t, t, t, lv)      // b  top-right
		set(2, cellW-t, 2*t+lv, t, lv) // c  bottom-right
		set(3, t, cellH-t, lh, t)      // d  bottom
		set(4, 0, 2*t+lv, t, lv)       // e  bottom-left
		set(5, 0, t, t, lv)            // f  top-left
		set(6, t, t+lv, lh, t)         // g  middle
	}
}

func (r *sevenSegRenderer) Refresh() {
	r.seg.applyColors()
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *sevenSegRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}
