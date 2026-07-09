package components

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// Rack-unit panel palette: a brushed gunmetal face with a beveled edge and
// corner screws, evoking a commercial synth/effects rack.
var (
	rackTop    = color.NRGBA{R: 0x42, G: 0x42, B: 0x48, A: 0xFF}
	rackBottom = color.NRGBA{R: 0x1C, G: 0x1C, B: 0x20, A: 0xFF}
	rackFrame  = color.NRGBA{R: 0x0B, G: 0x0B, B: 0x0D, A: 0xFF}
	rackHi     = color.NRGBA{R: 0x66, G: 0x66, B: 0x70, A: 0xFF}
	screwFill  = color.NRGBA{R: 0x28, G: 0x28, B: 0x2C, A: 0xFF}
	screwEdge  = color.NRGBA{R: 0x5C, G: 0x5C, B: 0x64, A: 0xFF}
	screwSlot  = color.NRGBA{R: 0x0E, G: 0x0E, B: 0x12, A: 0xFF}
)

const (
	rackPadX     = 22
	rackPadY     = 12
	rackPadYThin = 5 // reduced vertical padding for slim single-row strips
	screwR       = 5
	screwInset   = 11
)

// RackPanel wraps content in a rack-unit style panel: a gunmetal gradient face
// with a beveled frame and four corner screws.
type RackPanel struct {
	widget.BaseWidget
	content fyne.CanvasObject
	padY    float32 // vertical padding (see NewRackPanelThin)
}

// NewRackPanel wraps content in a rack panel.
func NewRackPanel(content fyne.CanvasObject) *RackPanel {
	p := &RackPanel{content: content, padY: rackPadY}
	p.ExtendBaseWidget(p)
	return p
}

// NewRackPanelThin wraps content in a rack panel with reduced vertical padding —
// for slim single-row strips (e.g. the status bar) where minimizing height
// matters. The frame/screws are unchanged; only the top/bottom padding shrinks.
func NewRackPanelThin(content fyne.CanvasObject) *RackPanel {
	p := &RackPanel{content: content, padY: rackPadYThin}
	p.ExtendBaseWidget(p)
	return p
}

func (p *RackPanel) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewVerticalGradient(rackTop, rackBottom)
	frame := canvas.NewRectangle(color.Transparent)
	frame.CornerRadius = 6
	frame.StrokeColor = rackFrame
	frame.StrokeWidth = 2
	hi := canvas.NewLine(rackHi)
	hi.StrokeWidth = 1

	r := &rackRenderer{p: p, bg: bg, frame: frame, hi: hi}
	objs := []fyne.CanvasObject{bg, frame, hi}
	for i := range 4 {
		c := canvas.NewCircle(screwFill)
		c.StrokeColor = screwEdge
		c.StrokeWidth = 1
		slot := canvas.NewLine(screwSlot)
		slot.StrokeWidth = 1.5
		r.screws[i] = c
		r.slots[i] = slot
		objs = append(objs, c, slot)
	}
	objs = append(objs, p.content)
	r.objects = objs
	return r
}

type rackRenderer struct {
	p       *RackPanel
	bg      *canvas.LinearGradient
	frame   *canvas.Rectangle
	hi      *canvas.Line
	screws  [4]*canvas.Circle
	slots   [4]*canvas.Line
	objects []fyne.CanvasObject
}

func (r *rackRenderer) Destroy() {}

func (r *rackRenderer) MinSize() fyne.Size {
	cs := r.p.content.MinSize()
	return fyne.NewSize(cs.Width+2*rackPadX, cs.Height+2*r.p.padY)
}

func (r *rackRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))
	r.frame.Resize(size)
	r.frame.Move(fyne.NewPos(0, 0))
	r.hi.Position1 = fyne.NewPos(3, 1.5)
	r.hi.Position2 = fyne.NewPos(size.Width-3, 1.5)

	r.p.content.Move(fyne.NewPos(rackPadX, r.p.padY))
	r.p.content.Resize(fyne.NewSize(size.Width-2*rackPadX, size.Height-2*r.p.padY))

	centers := [4]fyne.Position{
		{X: screwInset, Y: screwInset},
		{X: size.Width - screwInset, Y: screwInset},
		{X: screwInset, Y: size.Height - screwInset},
		{X: size.Width - screwInset, Y: size.Height - screwInset},
	}
	for i, c := range centers {
		r.screws[i].Move(fyne.NewPos(c.X-screwR, c.Y-screwR))
		r.screws[i].Resize(fyne.NewSize(2*screwR, 2*screwR))
		r.slots[i].Position1 = fyne.NewPos(c.X-screwR+1, c.Y)
		r.slots[i].Position2 = fyne.NewPos(c.X+screwR-1, c.Y)
	}
}

func (r *rackRenderer) Refresh() {
	r.p.content.Refresh()
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *rackRenderer) Objects() []fyne.CanvasObject { return r.objects }
