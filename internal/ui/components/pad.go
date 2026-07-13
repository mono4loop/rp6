package components

import (
	"image"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// maxBadges is the number of effect indicator icons a pad can show.
const maxBadges = 4

// padIdleBg is the pad plate's unlit background: a dark neutral gray (not pure
// black), matching the gallery pad's idle color so that lighting a pad reads as
// the plate itself filling with the bank color.
var padIdleBg = color.NRGBA{R: 0x20, G: 0x21, B: 0x24, A: 0xFF}

// blendNRGBA linearly interpolates from a to b by t (0..1) in 8-bit space.
func blendNRGBA(a, b color.NRGBA, t float64) color.NRGBA {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	lerp := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.NRGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: 0xFF}
}

// Pad is a large, touch-friendly, colored button with a centered label, a
// selection highlight, a brief tap flash, and up to four small badge icons
// along the bottom edge. It is display-agnostic: callers set its label/color
// and badges and handle taps via OnTapped.
type Pad struct {
	widget.BaseWidget

	OnTapped func()

	label    string
	baseCol  color.Color
	selected bool
	flashing bool
	flashSeq int // guards against stale flash timers
	badges   []image.Image
	minSize  fyne.Size

	rect *canvas.Rectangle
	glow *canvas.Rectangle
	text *canvas.Text
}

// NewPad returns a pad with the given label and fill color.
func NewPad(label string, fill color.Color, onTapped func()) *Pad {
	p := &Pad{label: label, baseCol: fill, OnTapped: onTapped, minSize: fyne.NewSize(44, 44)}
	p.ExtendBaseWidget(p)
	return p
}

// SetMinSize overrides the pad's minimum size (e.g. to pack more pads in a
// dense, all-banks layout). A zero size keeps the default.
func (p *Pad) SetMinSize(s fyne.Size) {
	if s.Width <= 0 || s.Height <= 0 {
		return
	}
	p.minSize = s
	p.Refresh()
}

// Set updates the pad's label and fill color, repainting if rendered.
func (p *Pad) Set(label string, fill color.Color) {
	p.label = label
	p.baseCol = fill
	if p.rect != nil {
		p.text.Text = label
		p.Refresh()
	}
}

// SetSelected toggles the selection highlight.
func (p *Pad) SetSelected(sel bool) {
	if p.selected == sel {
		return
	}
	p.selected = sel
	if p.rect != nil {
		p.Refresh()
	}
}

// Label returns the current label (useful for tests).
func (p *Pad) Label() string { return p.label }

// SetBadges sets the small icons shown along the bottom edge (up to four). Pass
// nil or empty to clear. The pad does not interpret the icons.
func (p *Pad) SetBadges(icons []image.Image) {
	if len(icons) > maxBadges {
		icons = icons[:maxBadges]
	}
	p.badges = icons
	if p.rect != nil {
		p.Refresh()
	}
}

// BadgeCount returns the number of badges currently shown (useful for tests).
func (p *Pad) BadgeCount() int { return len(p.badges) }

// Selected reports whether the pad is highlighted (useful for tests).
func (p *Pad) Selected() bool { return p.selected }

// AccessibilityLabel returns the pad's current caller-supplied label.
func (p *Pad) AccessibilityLabel() string { return p.label }

// AccessibilityRole reports that a pad is an actionable button.
func (p *Pad) AccessibilityRole() fyne.AccessibleRole { return fyne.AccessibleRoleButton }

// Flash briefly lights the pad (the same tap-flash used on a click), e.g. to
// reflect an external trigger.
func (p *Pad) Flash() { p.doFlash() }

// Tapped fires OnTapped and flashes the pad.
func (p *Pad) Tapped(_ *fyne.PointEvent) {
	if p.OnTapped != nil {
		p.OnTapped()
	}
	p.doFlash()
}

func (p *Pad) doFlash() {
	p.flashing = true
	p.flashSeq++
	seq := p.flashSeq
	p.Refresh()
	go func() {
		time.Sleep(110 * time.Millisecond)
		fyne.Do(func() {
			if p.flashSeq != seq {
				return // a newer tap took over
			}
			p.flashing = false
			p.Refresh()
		})
	}()
}

func (p *Pad) CreateRenderer() fyne.WidgetRenderer {
	// Modeled after the bottom-rack RackToggle: an inset plate with an accent
	// backlight glowing behind it and an accent-tinted caption. The pad's bank
	// color is the accent; the plate blends from a dark-gray idle toward that
	// color as it lights, flaring to full color on a tap flash (like the
	// gallery pads). Selection/flash raise the backlight.
	p.glow = canvas.NewRectangle(color.Transparent)
	p.glow.CornerRadius = 8

	p.rect = canvas.NewRectangle(padIdleBg)
	p.rect.CornerRadius = 6
	p.rect.StrokeColor = badgeStroke
	p.rect.StrokeWidth = 1.5

	p.text = canvas.NewText(p.label, color.White)
	p.text.Alignment = fyne.TextAlignCenter
	p.text.TextStyle = fyne.TextStyle{Bold: true}
	p.text.TextSize = 28

	r := &padRenderer{pad: p}
	for i := range r.badges {
		img := canvas.NewImageFromImage(nil)
		img.FillMode = canvas.ImageFillContain
		img.ScaleMode = canvas.ImageScaleSmooth
		img.Hide()
		r.badges[i] = img
	}
	return r
}

type padRenderer struct {
	pad    *Pad
	badges [maxBadges]*canvas.Image
}

func (r *padRenderer) Destroy() {}

func (r *padRenderer) Layout(size fyne.Size) {
	// Backlight glow fills the footprint; the dark plate is inset so the glow
	// bleeds around its edges (like RackToggle).
	r.pad.glow.Resize(size)
	r.pad.glow.Move(fyne.NewPos(0, 0))

	const inset = 2
	r.pad.rect.Resize(fyne.NewSize(size.Width-2*inset, size.Height-2*inset))
	r.pad.rect.Move(fyne.NewPos(inset, inset))

	// Scale the label with the pad height (capped) so it fits both large pads
	// and the dense all-banks layout.
	tsize := size.Height * 0.30
	if tsize > 28 {
		tsize = 28
	}
	if tsize < 11 {
		tsize = 11
	}
	r.pad.text.TextSize = tsize

	ts := r.pad.text.MinSize()
	r.pad.text.Resize(ts)
	r.pad.text.Move(fyne.NewPos((size.Width-ts.Width)/2, (size.Height-ts.Height)/2))

	// Badges: a centered row along the bottom edge.
	n := len(r.pad.badges)
	if n > 0 {
		bs := size.Height * 0.16
		if bs < 12 {
			bs = 12
		}
		if bs > 20 {
			bs = 20
		}
		gap := bs * 0.35
		rowW := float32(n)*bs + float32(n-1)*gap
		x0 := (size.Width - rowW) / 2
		y := size.Height - bs - 6
		for i := range r.badges {
			if i < n {
				r.badges[i].Move(fyne.NewPos(x0+float32(i)*(bs+gap), y))
				r.badges[i].Resize(fyne.NewSize(bs, bs))
			}
		}
	}
}

// MinSize keeps a modest floor so the grid can shrink to fit a small window
// (pads still grow to fill available space when there's room).
func (r *padRenderer) MinSize() fyne.Size {
	if r.pad.minSize.Width > 0 && r.pad.minSize.Height > 0 {
		return r.pad.minSize
	}
	return fyne.NewSize(44, 44)
}

func (r *padRenderer) Refresh() {
	a := toNRGBA(r.pad.baseCol)
	switch {
	case r.pad.flashing:
		// tap: the plate flares to full bank color over a hot backlight
		r.pad.glow.FillColor = withAlpha(a, 0xCC)
		r.pad.rect.FillColor = blendNRGBA(padIdleBg, a, 1.0)
		r.pad.rect.StrokeColor = withAlpha(lightenColor(a, 0.40), 0xF0)
		r.pad.rect.StrokeWidth = 2
		r.pad.text.Color = lightenColor(a, 0.9)
	case r.pad.selected:
		// selected: plate strongly lit in the bank color, bright backlight
		r.pad.glow.FillColor = withAlpha(a, 0x9E)
		r.pad.rect.FillColor = blendNRGBA(padIdleBg, a, 0.78)
		r.pad.rect.StrokeColor = withAlpha(lightenColor(a, 0.2), 0xE0)
		r.pad.rect.StrokeWidth = 2
		r.pad.text.Color = lightenColor(a, 0.8)
	default:
		// idle: a vibrant plate saturated in the bank color, soft accent backlight
		r.pad.glow.FillColor = withAlpha(a, 0x64)
		r.pad.rect.FillColor = blendNRGBA(padIdleBg, a, 0.46)
		r.pad.rect.StrokeColor = withAlpha(a, 0xB0)
		r.pad.rect.StrokeWidth = 1.5
		r.pad.text.Color = lightenColor(a, 0.7)
	}
	r.pad.glow.Refresh()
	r.pad.rect.Refresh()

	r.pad.text.Text = r.pad.label
	r.pad.text.Refresh()

	for i := range r.badges {
		if i < len(r.pad.badges) {
			r.badges[i].Image = r.pad.badges[i]
			r.badges[i].Show()
		} else {
			r.badges[i].Image = nil
			r.badges[i].Hide()
		}
		r.badges[i].Refresh()
	}
	// Reposition badges for the current count.
	r.Layout(r.pad.Size())
}

func (r *padRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.pad.glow, r.pad.rect, r.pad.text}
	for i := range r.badges {
		objs = append(objs, r.badges[i])
	}
	return objs
}
