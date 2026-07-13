package components

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// Physical seq-key palette.
var (
	keyBody     = color.NRGBA{R: 0x1D, G: 0x1D, B: 0x22, A: 0xFF} // dark translucent cap
	keyEdge     = color.NRGBA{R: 0x0C, G: 0x0C, B: 0x10, A: 0xFF}
	keyBevel    = color.NRGBA{R: 0x5A, G: 0x5A, B: 0x66, A: 0xFF}
	keyBevelLit = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xAA}
	ledWhite    = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xF2, A: 0xFF}
	defaultAcc  = color.NRGBA{R: 0xFF, G: 0xC0, B: 0x50, A: 0xFF} // amber fallback
)

// StepButton is a sequencer step cell drawn as a physical, backlit key: a dark
// translucent cap lit from within by a central LED. Off it shows only a dim
// ember at the center; programmed it glows fully in its accent color; under the
// playhead it flares white-hot. The accent color is configurable (SetAccent) so
// each track can be tinted with its pad's bank color. Tapping toggles the
// programmed state and calls OnToggle.
type StepButton struct {
	widget.BaseWidget

	OnToggle func()

	on      bool
	playing bool
	accent  color.NRGBA

	body *canvas.Rectangle
	glow *canvas.RadialGradient
	hi   *canvas.Line
}

// NewStepButton returns an off step cell.
func NewStepButton(onToggle func()) *StepButton {
	s := &StepButton{OnToggle: onToggle, accent: defaultAcc}
	s.ExtendBaseWidget(s)
	return s
}

// SetAccent sets the "lit" color of the key (e.g. the track's pad bank color).
func (s *StepButton) SetAccent(c color.Color) {
	if c == nil {
		c = defaultAcc
	}
	s.accent = toNRGBA(c)
	if s.body != nil {
		s.Refresh()
	}
}

// SetOn sets the programmed state.
func (s *StepButton) SetOn(on bool) {
	if s.on == on {
		return
	}
	s.on = on
	if s.body != nil {
		s.Refresh()
	}
}

// On reports the programmed state.
func (s *StepButton) On() bool { return s.on }

// AccessibilityLabel describes whether this sequencer step is programmed.
func (s *StepButton) AccessibilityLabel() string {
	if s.on {
		return "Sequencer step on"
	}
	return "Sequencer step off"
}

// AccessibilityRole reports that a sequencer step is an actionable button.
func (s *StepButton) AccessibilityRole() fyne.AccessibleRole {
	return fyne.AccessibleRoleButton
}

// SetPlaying marks the cell as under the playhead.
func (s *StepButton) SetPlaying(p bool) {
	if s.playing == p {
		return
	}
	s.playing = p
	if s.body != nil {
		s.Refresh()
	}
}

// Tapped toggles the programmed state and fires OnToggle.
func (s *StepButton) Tapped(_ *fyne.PointEvent) {
	s.on = !s.on
	if s.body != nil {
		s.Refresh()
	}
	if s.OnToggle != nil {
		s.OnToggle()
	}
}

func (s *StepButton) CreateRenderer() fyne.WidgetRenderer {
	s.body = canvas.NewRectangle(keyBody)
	s.body.CornerRadius = 4
	s.body.StrokeColor = keyEdge
	s.body.StrokeWidth = 1

	s.glow = canvas.NewRadialGradient(color.Transparent, color.Transparent)

	s.hi = canvas.NewLine(keyBevel)
	s.hi.StrokeWidth = 1
	return &stepRenderer{s: s}
}

type stepRenderer struct{ s *StepButton }

func (r *stepRenderer) Destroy() {}

func (r *stepRenderer) MinSize() fyne.Size { return fyne.NewSize(32, 34) }

func (r *stepRenderer) Layout(size fyne.Size) {
	r.s.body.Resize(size)
	r.s.body.Move(fyne.NewPos(0, 0))

	// The glow sits just inside the cap so the dark rim always reads.
	const inset = 2
	r.s.glow.Resize(fyne.NewSize(size.Width-2*inset, size.Height-2*inset))
	r.s.glow.Move(fyne.NewPos(inset, inset))

	r.s.hi.Position1 = fyne.NewPos(4, 2.5)
	r.s.hi.Position2 = fyne.NewPos(size.Width-4, 2.5)
}

func (r *stepRenderer) Refresh() {
	a := r.s.accent
	switch {
	case r.s.playing && r.s.on:
		// white-hot: bright square with a white core bloom
		r.s.body.FillColor = lightenColor(a, 0.28)
		r.s.glow.StartColor = withAlpha(ledWhite, 0xD0)
		r.s.glow.EndColor = withAlpha(ledWhite, 0x10)
		r.s.hi.StrokeColor = keyBevelLit
	case r.s.playing:
		// playhead sweeping an empty step: a brighter ember over the dim cap
		r.s.body.FillColor = darkenTo(a, 0.30)
		r.s.glow.StartColor = withAlpha(lightenColor(a, 0.4), 0x88)
		r.s.glow.EndColor = withAlpha(a, 0x10)
		r.s.hi.StrokeColor = keyBevel
	case r.s.on:
		// fully lit: the whole square glows, with a hotter center for depth
		r.s.body.FillColor = darkenTo(a, 0.72)
		r.s.glow.StartColor = withAlpha(lightenColor(a, 0.55), 0x99)
		r.s.glow.EndColor = withAlpha(a, 0x0E)
		r.s.hi.StrokeColor = keyBevelLit
	default:
		// off: a dim tinted square, faintly hotter at the core
		r.s.body.FillColor = darkenTo(a, 0.20)
		r.s.glow.StartColor = withAlpha(a, 0x40)
		r.s.glow.EndColor = withAlpha(a, 0x0A)
		r.s.hi.StrokeColor = keyBevel
	}
	for _, o := range r.Objects() {
		o.Refresh()
	}
}

func (r *stepRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.s.body, r.s.glow, r.s.hi}
}

// toNRGBA converts any color to NRGBA (8-bit channels).
func toNRGBA(c color.Color) color.NRGBA {
	r, g, b, a := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}
