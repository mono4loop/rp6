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

	// Face palette (top→bottom gradient + bevel highlight). Defaults to gunmetal;
	// NewRackPanelTinted derives them from a base color (e.g. the P-6 yellow).
	top, bottom, hi color.NRGBA

	// nameplate, if non-empty, is silkscreened bold on the right of the plate in
	// the ink color — a device brand mark (e.g. "P-6"). nameLead, if set, is a
	// small accessory (e.g. a status LED) placed immediately to its left.
	nameplate string
	ink       color.NRGBA
	nameLead  fyne.CanvasObject
}

// InspectionChildren exposes the semantic content embedded in this composite
// widget. Fyne's accessibility collectors only recurse through containers, and
// its public API has no semantic-tree walker, so RP6's inspection package uses
// this narrow hook without depending on renderer internals.
func (p *RackPanel) InspectionChildren() []fyne.CanvasObject {
	children := []fyne.CanvasObject{p.content}
	if p.nameLead != nil {
		children = append(children, p.nameLead)
	}
	return children
}

// AccessibilityLabel names the rack panel for assistive technologies. Named
// device panels use their nameplate; generic panels are exposed as a rack.
func (p *RackPanel) AccessibilityLabel() string {
	if p.nameplate != "" {
		return p.nameplate + " rack"
	}
	return "Rack panel"
}

// AccessibilityRole reports that a rack panel groups related controls.
func (p *RackPanel) AccessibilityRole() fyne.AccessibleRole {
	return fyne.AccessibleRoleContainer
}

// NewRackPanel wraps content in a rack panel.
func NewRackPanel(content fyne.CanvasObject) *RackPanel {
	return newRackPanel(content, rackPadY)
}

// NewRackPanelThin wraps content in a rack panel with reduced vertical padding —
// for slim single-row strips (e.g. the status bar) where minimizing height
// matters. The frame/screws are unchanged; only the top/bottom padding shrinks.
func NewRackPanelThin(content fyne.CanvasObject) *RackPanel {
	return newRackPanel(content, rackPadYThin)
}

func newRackPanel(content fyne.CanvasObject, padY float32) *RackPanel {
	p := &RackPanel{content: content, padY: padY, top: rackTop, bottom: rackBottom, hi: rackHi}
	p.ExtendBaseWidget(p)
	return p
}

// NewRackPanelTinted wraps content in a rack panel whose brushed face is tinted
// from base — a lit top→darker bottom gradient — instead of gunmetal (e.g. the
// P-6's yellow chassis), while keeping the beveled frame, highlight and corner
// screws so it still reads as a rack unit. If nameplate is non-empty it's
// silkscreened bold on the right in a dark ink derived from base; lead (optional,
// may be nil) is a small accessory shown just to the left of that mark — e.g. a
// status LED, so it reads as part of the nameplate cluster.
func NewRackPanelTinted(content fyne.CanvasObject, base color.NRGBA, nameplate string, lead fyne.CanvasObject) *RackPanel {
	p := &RackPanel{
		content:   content,
		padY:      rackPadY,
		top:       lightenColor(base, 0.12),
		bottom:    darkenTo(base, 0.42),
		hi:        lightenColor(base, 0.55),
		nameplate: nameplate,
		ink:       darkenTo(base, 0.16),
		nameLead:  lead,
	}
	p.ExtendBaseWidget(p)
	return p
}

func (p *RackPanel) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewVerticalGradient(p.top, p.bottom)
	frame := canvas.NewRectangle(color.Transparent)
	frame.CornerRadius = 6
	frame.StrokeColor = rackFrame
	frame.StrokeWidth = 2
	hi := canvas.NewLine(p.hi)
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
	if p.nameplate != "" {
		if p.nameLead != nil {
			objs = append(objs, p.nameLead)
		}
		// Faux-bold: Fyne's Bold is binary, so layer the text at sub-pixel
		// offsets to thicken the strokes into a chunky silkscreen mark.
		for _, off := range nameplateOffsets {
			t := canvas.NewText(p.nameplate, p.ink)
			t.TextStyle = fyne.TextStyle{Bold: true}
			t.TextSize = 30
			r.name = append(r.name, textLayer{t: t, off: off})
			objs = append(objs, t)
		}
	}
	r.objects = objs
	return r
}

// nameplateOffsets thicken the nameplate: the base glyph plus copies nudged by a
// fraction of a pixel in each direction (a lightweight faux-bold).
var nameplateOffsets = []fyne.Position{
	{X: 0, Y: 0},
	{X: 0.7, Y: 0},
	{X: 0, Y: 0.7},
	{X: 0.7, Y: 0.7},
}

// textLayer is one nudged copy of the nameplate text (for faux-bold layering).
type textLayer struct {
	t   *canvas.Text
	off fyne.Position
}

type rackRenderer struct {
	p       *RackPanel
	bg      *canvas.LinearGradient
	frame   *canvas.Rectangle
	hi      *canvas.Line
	name    []textLayer // optional nameplate (right side), layered for faux-bold
	screws  [4]*canvas.Circle
	slots   [4]*canvas.Line
	objects []fyne.CanvasObject
}

func (r *rackRenderer) Destroy() {}

// nameplateWidth is the horizontal space (lead + label + margins) the nameplate
// reserves on the right of the plate, or 0 when there's no nameplate.
func (r *rackRenderer) nameplateWidth() float32 {
	if len(r.name) == 0 {
		return 0
	}
	w := r.name[0].t.MinSize().Width + 20
	if r.p.nameLead != nil {
		w += r.p.nameLead.MinSize().Width + nameLeadGap
	}
	return w
}

// nameLeadGap is the space between the nameplate lead (e.g. an LED) and the mark.
const nameLeadGap = 8

func (r *rackRenderer) MinSize() fyne.Size {
	cs := r.p.content.MinSize()
	return fyne.NewSize(cs.Width+2*rackPadX+r.nameplateWidth(), cs.Height+2*r.p.padY)
}

func (r *rackRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))
	r.frame.Resize(size)
	r.frame.Move(fyne.NewPos(0, 0))
	r.hi.Position1 = fyne.NewPos(3, 1.5)
	r.hi.Position2 = fyne.NewPos(size.Width-3, 1.5)

	nameW := r.nameplateWidth()
	r.p.content.Move(fyne.NewPos(rackPadX, r.p.padY))
	r.p.content.Resize(fyne.NewSize(size.Width-2*rackPadX-nameW, size.Height-2*r.p.padY))
	if len(r.name) > 0 {
		ns := r.name[0].t.MinSize()
		// Right-aligned (just inside the right padding) and vertically centered,
		// clear of the corner screws.
		base := fyne.NewPos(size.Width-rackPadX-ns.Width, (size.Height-ns.Height)/2)
		for _, l := range r.name {
			l.t.Resize(ns)
			l.t.Move(base.Add(l.off))
		}
		// The lead accessory (LED) hugs the left of the mark, vertically centered.
		if r.p.nameLead != nil {
			ls := r.p.nameLead.MinSize()
			r.p.nameLead.Resize(ls)
			r.p.nameLead.Move(fyne.NewPos(base.X-nameLeadGap-ls.Width, (size.Height-ls.Height)/2))
		}
	}

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
