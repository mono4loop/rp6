package components

import (
	"image/color"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// LCD/OLED palette: warm amber phosphor text on a near-black panel.
var (
	lcdPanel    = color.NRGBA{R: 0x12, G: 0x0A, B: 0x04, A: 0xFF}
	lcdText     = color.NRGBA{R: 0xFF, G: 0xA5, B: 0x2A, A: 0xFF}
	lcdGlow     = color.NRGBA{R: 0xFF, G: 0xA5, B: 0x2A, A: 0x33}
	lcdBorder   = color.NRGBA{R: 0x3A, G: 0x22, B: 0x0C, A: 0xFF}
	lcdCursor   = color.NRGBA{R: 0xFF, G: 0xA5, B: 0x2A, A: 0xCC}
	transparent = color.NRGBA{}
)

// LCDStepperConfig configures an LCDStepper.
type LCDStepperConfig struct {
	Title string

	Value int
	Min   int
	Max   int
	Step  int

	// Width overrides the readout's minimum width in pixels (default 120).
	// Use a small value for short values like a track count or slot number.
	Width float32

	Format   func(int) string         // render value for display, e.g. "120 BPM"
	Parse    func(string) (int, bool) // parse typed text into a value
	OnChange func(int)                // called whenever the value changes

	// OnAltStep, if set, makes the + button modifier-aware: Ctrl-clicking +
	// advances the value and calls OnAltStep(newValue) *instead of* OnChange
	// (e.g. to duplicate rather than move to a fresh slot). While hovered, the +
	// button shows AltIcon to advertise the alternate action.
	OnAltStep func(int)
	AltIcon   fyne.Resource
}

// LCDStepper is a reusable numeric control: an always-editable LCD/OLED-style
// readout flanked by − and + buttons. Click the readout and type to change it
// (Enter or click-away commits, Esc cancels); the first keystroke replaces the
// shown value.
type LCDStepper struct {
	cfg     LCDStepperConfig
	value   int
	display *lcdDisplay
	plusAlt *ModifierButton // non-nil when OnAltStep is configured
	obj     *fyne.Container
}

// NewLCDStepper builds an LCDStepper from cfg.
func NewLCDStepper(cfg LCDStepperConfig) *LCDStepper {
	if cfg.Step == 0 {
		cfg.Step = 1
	}
	if cfg.Format == nil {
		cfg.Format = func(v int) string { return itoa(v) }
	}
	s := &LCDStepper{cfg: cfg, value: clampInt(cfg.Value, cfg.Min, cfg.Max)}

	s.display = newLCDDisplay(cfg.Format(s.value), s.commit)
	s.display.accept = func(r rune) bool { return unicode.IsDigit(r) || r == '-' }
	if cfg.Width > 0 {
		s.display.minWidth = cfg.Width
	}
	minus := widget.NewButton("−", s.Decrement)

	var plus fyne.CanvasObject
	if cfg.OnAltStep != nil {
		s.plusAlt = NewModifierButton("+", cfg.AltIcon, s.Increment, s.altStep)
		plus = s.plusAlt
	} else {
		plus = widget.NewButton("+", s.Increment)
	}

	items := []fyne.CanvasObject{}
	if cfg.Title != "" {
		items = append(items, widget.NewLabel(cfg.Title))
	}
	items = append(items, minus, s.display, plus)
	s.obj = container.NewHBox(items...)
	return s
}

// altStep advances the value like Increment but reports it through OnAltStep
// (used for the Ctrl-click "duplicate" action). No-op when already at Max.
func (s *LCDStepper) altStep() {
	v := clampInt(s.value+s.cfg.Step, s.cfg.Min, s.cfg.Max)
	if v == s.value {
		return
	}
	s.value = v
	s.display.SetText(s.cfg.Format(v))
	if s.cfg.OnAltStep != nil {
		s.cfg.OnAltStep(v)
	}
}

// Object returns the CanvasObject to place in a layout.
func (s *LCDStepper) Object() fyne.CanvasObject { return s.obj }

// SetAltHintActive tells the stepper whether the modifier (Ctrl) for its alt
// action is currently held, so the + button can reveal its AltIcon while
// hovered. No-op unless OnAltStep was configured.
func (s *LCDStepper) SetAltHintActive(on bool) {
	if s.plusAlt != nil {
		s.plusAlt.SetModifierActive(on)
	}
}

// Value returns the current value.
func (s *LCDStepper) Value() int { return s.value }

// SetValue sets the value (clamped), updating the display and firing OnChange
// if it changed.
func (s *LCDStepper) SetValue(v int) { s.set(v) }

// SetValueSilent sets the value (clamped) and display without firing OnChange.
// Use it to reflect externally-driven changes.
func (s *LCDStepper) SetValueSilent(v int) {
	s.value = clampInt(v, s.cfg.Min, s.cfg.Max)
	s.display.SetText(s.cfg.Format(s.value))
}

// Increment/Decrement step the value by the configured step.
func (s *LCDStepper) Increment() { s.set(s.value + s.cfg.Step) }
func (s *LCDStepper) Decrement() { s.set(s.value - s.cfg.Step) }

func (s *LCDStepper) set(v int) {
	v = clampInt(v, s.cfg.Min, s.cfg.Max)
	changed := v != s.value
	s.value = v
	s.display.SetText(s.cfg.Format(v))
	if changed && s.cfg.OnChange != nil {
		s.cfg.OnChange(v)
	}
}

// commit is called by the display when the user finishes typing.
func (s *LCDStepper) commit(text string) {
	if s.cfg.Parse != nil {
		if v, ok := s.cfg.Parse(text); ok {
			s.set(v)
		}
	}
	// Normalize the readout back to the formatted value regardless of input.
	s.display.SetText(s.cfg.Format(s.value))
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func itoa(v int) string {
	// small, allocation-light integer to string for the default formatter
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [12]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// lcdDisplay is a focusable, tappable LCD/OLED-styled text field. It edits an
// internal buffer while focused and reports the committed text via onCommit.
type lcdDisplay struct {
	widget.BaseWidget

	text     string // committed text shown when not editing
	editing  bool
	pristine bool // true until the first keystroke replaces the shown value
	buffer   string
	accept   func(rune) bool
	onCommit func(string)
	minWidth float32

	panel  *canvas.Rectangle
	glow   *canvas.Text
	txt    *canvas.Text
	cursor *canvas.Rectangle
}

func newLCDDisplay(text string, onCommit func(string)) *lcdDisplay {
	d := &lcdDisplay{text: text, onCommit: onCommit}
	d.ExtendBaseWidget(d)
	return d
}

// SetText updates the committed value shown when not editing.
func (d *lcdDisplay) SetText(s string) {
	d.text = s
	if !d.editing {
		d.Refresh()
	}
}

func (d *lcdDisplay) shown() string {
	if d.editing && !d.pristine {
		return d.buffer
	}
	return d.text
}

// --- fyne.Tappable ---

func (d *lcdDisplay) Tapped(_ *fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(d); c != nil {
		c.Focus(d)
	}
}

// --- fyne.Focusable ---

func (d *lcdDisplay) FocusGained() {
	d.editing = true
	d.pristine = true
	d.buffer = d.text
	d.Refresh()
}

func (d *lcdDisplay) FocusLost() {
	if d.editing {
		d.commit()
	}
}

func (d *lcdDisplay) TypedRune(r rune) {
	if !d.editing {
		return
	}
	if d.accept != nil && !d.accept(r) {
		return
	}
	if d.pristine {
		d.buffer = ""
		d.pristine = false
	}
	d.buffer += string(r)
	d.Refresh()
}

func (d *lcdDisplay) TypedKey(e *fyne.KeyEvent) {
	if !d.editing {
		return
	}
	switch e.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		d.commit()
		d.unfocus()
	case fyne.KeyBackspace:
		if d.pristine {
			d.buffer = ""
			d.pristine = false
		} else if len(d.buffer) > 0 {
			d.buffer = d.buffer[:len(d.buffer)-1]
		}
		d.Refresh()
	case fyne.KeyEscape:
		d.editing = false
		d.pristine = false
		d.Refresh()
		d.unfocus()
	}
}

func (d *lcdDisplay) commit() {
	editing, pristine, buf := d.editing, d.pristine, d.buffer
	d.editing = false
	d.pristine = false
	// Only commit if the user actually typed something.
	if editing && !pristine && d.onCommit != nil {
		d.onCommit(buf)
	}
	d.Refresh()
}

func (d *lcdDisplay) unfocus() {
	if c := fyne.CurrentApp().Driver().CanvasForObject(d); c != nil {
		c.Unfocus()
	}
}

func (d *lcdDisplay) CreateRenderer() fyne.WidgetRenderer {
	d.panel = canvas.NewRectangle(lcdPanel)
	d.panel.CornerRadius = 6
	d.panel.StrokeColor = lcdBorder
	d.panel.StrokeWidth = 1

	d.glow = canvas.NewText(d.shown(), lcdGlow)
	d.glow.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	d.glow.TextSize = 21

	d.txt = canvas.NewText(d.shown(), lcdText)
	d.txt.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	d.txt.TextSize = 20

	d.cursor = canvas.NewRectangle(transparent)

	return &lcdRenderer{d: d, objects: []fyne.CanvasObject{d.panel, d.glow, d.txt, d.cursor}}
}

type lcdRenderer struct {
	d       *lcdDisplay
	objects []fyne.CanvasObject
}

func (r *lcdRenderer) Destroy() {}

func (r *lcdRenderer) MinSize() fyne.Size {
	w := float32(120)
	if r.d.minWidth > 0 {
		w = r.d.minWidth
	}
	return fyne.NewSize(w, 38)
}

func (r *lcdRenderer) Layout(size fyne.Size) {
	r.d.panel.Resize(size)
	r.d.panel.Move(fyne.NewPos(0, 0))
	r.place(size)
}

// place positions the text and cursor for the current content.
func (r *lcdRenderer) place(size fyne.Size) {
	pad := float32(10)
	ts := r.d.txt.MinSize()
	y := (size.Height - ts.Height) / 2

	r.d.txt.Resize(ts)
	r.d.txt.Move(fyne.NewPos(pad, y))

	gs := r.d.glow.MinSize()
	r.d.glow.Resize(gs)
	r.d.glow.Move(fyne.NewPos(pad-0.5, y-0.5))

	ch := ts.Height * 0.82
	r.d.cursor.Resize(fyne.NewSize(9, ch))
	r.d.cursor.Move(fyne.NewPos(pad+ts.Width+2, (size.Height-ch)/2))
}

func (r *lcdRenderer) Refresh() {
	s := r.d.shown()
	r.d.txt.Text = s
	r.d.glow.Text = s
	if r.d.editing {
		r.d.cursor.FillColor = lcdCursor
	} else {
		r.d.cursor.FillColor = transparent
	}
	r.place(r.d.Size())
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *lcdRenderer) Objects() []fyne.CanvasObject { return r.objects }
