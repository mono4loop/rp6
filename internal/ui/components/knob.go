package components

import (
	"image"
	"image/color"
	"math"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// ledOff is the color of an unlit LED-ring segment (gray).
var ledOff = color.NRGBA{R: 0x4A, G: 0x4A, B: 0x52, A: 0xFF}

// The amber "LCD readout" palette used by the Knob's display (and re-used for
// the pad grid's page accent): a near-black inset panel, bright amber text, and
// a dim border. transparent is the zero (fully-transparent) fill for the
// unlit focus glow.
var (
	lcdPanel    = color.NRGBA{R: 0x12, G: 0x0A, B: 0x04, A: 0xFF}
	lcdText     = color.NRGBA{R: 0xFF, G: 0xA5, B: 0x2A, A: 0xFF}
	lcdBorder   = color.NRGBA{R: 0x3A, G: 0x22, B: 0x0C, A: 0xFF}
	transparent = color.NRGBA{}
)

// KnobConfig configures a Knob.
type KnobConfig struct {
	Label string // small caption shown inside the display

	Value int
	Min   int
	Max   int
	Step  int

	Width  float32     // overall min width (default 156)
	Accent color.NRGBA // lit-segment / focus color (default amber lcdText)

	// Compact lays the indicator, caption and value out in a single row (instead
	// of the caption-over-value stack), roughly halving the height — for narrow
	// strips like the keyboard's OCT knob.
	Compact bool

	// Indicator overrides how the value is visualized (default: the LED
	// segment ring). See BoltIndicator, LanesIndicator, GridIndicator.
	Indicator KnobIndicator

	Format   func(int) string // render value for display, e.g. "120 BPM"
	OnChange func(int)        // called whenever the value changes
}

// KnobIndicator renders a knob's value visualization in the square to the left
// of the LCD plate. Implementations own both the shape and its fill semantics
// (a magnitude level, a count of lit elements, or a highlighted index). The
// returned image is composited onto the plate, so pixels outside the shape must
// be transparent. See RingIndicator (default), BoltIndicator, LanesIndicator,
// GridIndicator.
type KnobIndicator interface {
	Image(px, value, min, max, step int, accent color.NRGBA) image.Image
}

// Knob is a rotary value control: an LED segment ring (full circle, lit up to
// the value in the accent color, unlit segments grey) on the left of a dark
// LCD-style plate, with a small amber caption and the formatted value to its
// right. The plate border lights in the accent color when the knob is focused.
// It responds to mouse drag (up/down), the scroll wheel, and the arrow keys
// (Up/Right increase, Down/Left decrease).
type Knob struct {
	widget.BaseWidget
	cfg     KnobConfig
	value   int
	focused bool
	dragAcc float64

	flashMu   sync.Mutex
	flashing  bool          // border flashing (a queued/pending change)
	flashOn   bool          // current blink phase
	flashStop chan struct{} // closed to stop the flash goroutine

	plate  *canvas.Rectangle
	glow   *canvas.Rectangle
	ring   *canvas.Image
	label  *canvas.Text
	valTxt *canvas.Text
}

// ringPx is the (logical) pixel size the LED ring image is rendered at.
const ringPx = 128

// NewKnob builds a Knob from cfg.
func NewKnob(cfg KnobConfig) *Knob {
	if cfg.Step == 0 {
		cfg.Step = 1
	}
	if cfg.Width == 0 {
		cfg.Width = 156
	}
	if cfg.Accent == (color.NRGBA{}) {
		cfg.Accent = lcdText
	}
	if cfg.Format == nil {
		cfg.Format = itoa
	}
	k := &Knob{cfg: cfg, value: clampInt(cfg.Value, cfg.Min, cfg.Max)}
	k.ExtendBaseWidget(k)
	return k
}

// Object returns the CanvasObject to place in a layout (the Knob itself).
func (k *Knob) Object() fyne.CanvasObject { return k }

// Value returns the current value.
func (k *Knob) Value() int { return k.value }

// SetValue sets the value (clamped), firing OnChange if it changed.
func (k *Knob) SetValue(v int) { k.set(v) }

// SetValueSilent sets the value (clamped) without firing OnChange.
func (k *Knob) SetValueSilent(v int) {
	k.value = clampInt(v, k.cfg.Min, k.cfg.Max)
	if k.plate != nil {
		k.Refresh()
	}
}

// Increment/Decrement step the value by the configured step.
func (k *Knob) Increment() { k.set(k.value + k.cfg.Step) }
func (k *Knob) Decrement() { k.set(k.value - k.cfg.Step) }

// SetPending starts (or stops) a flashing border that signals a queued change
// not yet applied — e.g. a sequencer slot change waiting for the next bar. The
// border blinks in the accent color until SetPending(false). Idempotent.
func (k *Knob) SetPending(on bool) {
	k.flashMu.Lock()
	if on == k.flashing {
		k.flashMu.Unlock()
		return
	}
	k.flashing = on
	if on {
		k.flashStop = make(chan struct{})
		stop := k.flashStop
		k.flashMu.Unlock()
		go k.flashLoop(stop)
		return
	}
	close(k.flashStop)
	k.flashMu.Unlock()
	k.flashOn = false
	if k.plate != nil {
		k.Refresh()
	}
}

func (k *Knob) flashLoop(stop <-chan struct{}) {
	coalescedTicker(180*time.Millisecond, stop, func() {
		k.flashOn = !k.flashOn
		if k.plate != nil {
			k.Refresh()
		}
	})
}

func (k *Knob) set(v int) {
	v = clampInt(v, k.cfg.Min, k.cfg.Max)
	changed := v != k.value
	k.value = v
	if k.plate != nil {
		k.Refresh()
	}
	if changed && k.cfg.OnChange != nil {
		k.cfg.OnChange(v)
	}
}

// frac maps a value to 0..1 across [min,max].
func frac(value, min, max int) float64 {
	if max == min {
		return 0
	}
	return float64(value-min) / float64(max-min)
}

// stepsIdx returns the number of discrete steps in [min,max] (by step) and the
// value's 0-based step index (clamped).
func stepsIdx(value, min, max, step int) (steps, idx int) {
	if step == 0 {
		step = 1
	}
	steps = (max-min)/step + 1
	if steps < 1 {
		steps = 1
	}
	idx = (value - min) / step
	if idx < 0 {
		idx = 0
	}
	if idx > steps-1 {
		idx = steps - 1
	}
	return steps, idx
}

// indicatorImage renders the value visualization via the configured indicator
// (defaulting to the LED segment ring).
func (k *Knob) indicatorImage() image.Image {
	ind := k.cfg.Indicator
	if ind == nil {
		ind = RingIndicator{}
	}
	return ind.Image(ringPx, k.value, k.cfg.Min, k.cfg.Max, k.cfg.Step, k.cfg.Accent)
}

// --- interaction ---

func (k *Knob) focus() {
	if c := fyne.CurrentApp().Driver().CanvasForObject(k); c != nil {
		c.Focus(k)
	}
}

// Tapped focuses the knob (fyne.Tappable).
func (k *Knob) Tapped(*fyne.PointEvent) { k.focus() }

// DoubleTapped resets the knob to zero (clamped to its configured range) and
// fires OnChange like any other user edit.
func (k *Knob) DoubleTapped(*fyne.PointEvent) {
	k.focus()
	k.set(0)
}

// Dragged turns the knob: dragging up increases, down decreases (fyne.Draggable).
func (k *Knob) Dragged(e *fyne.DragEvent) {
	if !k.focused {
		k.focus()
	}
	const pxPerStep = 6
	k.dragAcc += -float64(e.Dragged.DY)
	for k.dragAcc >= pxPerStep {
		k.Increment()
		k.dragAcc -= pxPerStep
	}
	for k.dragAcc <= -pxPerStep {
		k.Decrement()
		k.dragAcc += pxPerStep
	}
}

// DragEnd resets the drag accumulator (fyne.Draggable).
func (k *Knob) DragEnd() { k.dragAcc = 0 }

// Scrolled turns the knob with the mouse wheel (fyne.Scrollable).
func (k *Knob) Scrolled(e *fyne.ScrollEvent) {
	switch {
	case e.Scrolled.DY > 0:
		k.Increment()
	case e.Scrolled.DY < 0:
		k.Decrement()
	}
}

// --- fyne.Focusable ---

func (k *Knob) FocusGained()   { k.focused = true; k.Refresh() }
func (k *Knob) FocusLost()     { k.focused = false; k.Refresh() }
func (k *Knob) TypedRune(rune) {}

func (k *Knob) TypedKey(e *fyne.KeyEvent) {
	switch e.Name {
	case fyne.KeyUp, fyne.KeyRight:
		k.Increment()
	case fyne.KeyDown, fyne.KeyLeft:
		k.Decrement()
	}
}

func (k *Knob) CreateRenderer() fyne.WidgetRenderer {
	k.glow = canvas.NewRectangle(transparent)
	k.glow.CornerRadius = 6
	k.plate = canvas.NewRectangle(lcdPanel)
	k.plate.CornerRadius = 5
	k.plate.StrokeColor = lcdBorder
	k.plate.StrokeWidth = 1.5

	k.ring = canvas.NewImageFromImage(k.indicatorImage())
	k.ring.FillMode = canvas.ImageFillContain

	k.label = canvas.NewText(k.cfg.Label, lightenColor(k.cfg.Accent, 0.5))
	k.label.TextSize = 10
	k.label.TextStyle = fyne.TextStyle{Bold: true}

	k.valTxt = canvas.NewText(k.cfg.Format(k.value), lcdText)
	k.valTxt.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	k.valTxt.TextSize = 18

	return &knobRenderer{k: k, objects: []fyne.CanvasObject{k.glow, k.plate, k.ring, k.label, k.valTxt}}
}

type knobRenderer struct {
	k       *Knob
	objects []fyne.CanvasObject
}

func (r *knobRenderer) Destroy() { r.k.SetPending(false) }

func (r *knobRenderer) MinSize() fyne.Size {
	if r.k.cfg.Compact {
		return fyne.NewSize(r.k.cfg.Width, 34)
	}
	return fyne.NewSize(r.k.cfg.Width, 58)
}

func (r *knobRenderer) Layout(size fyne.Size) { r.place(size) }

// place lays out the plate, the LED ring (left) and the caption/value (right).
func (r *knobRenderer) place(size fyne.Size) {
	r.k.glow.Resize(size)
	r.k.glow.Move(fyne.NewPos(0, 0))
	inset := float32(2)
	r.k.plate.Resize(fyne.NewSize(size.Width-2*inset, size.Height-2*inset))
	r.k.plate.Move(fyne.NewPos(inset, inset))

	// Compact: indicator, caption and value all in one horizontal row.
	if r.k.cfg.Compact {
		rs := size.Height - 10
		r.k.ring.Resize(fyne.NewSize(rs, rs))
		r.k.ring.Move(fyne.NewPos(6, (size.Height-rs)/2))
		textX := 6 + rs + 8
		ls := r.k.label.MinSize()
		r.k.label.Resize(ls)
		r.k.label.Move(fyne.NewPos(textX, (size.Height-ls.Height)/2))
		vs := r.k.valTxt.MinSize()
		r.k.valTxt.Resize(vs)
		r.k.valTxt.Move(fyne.NewPos(textX+ls.Width+8, (size.Height-vs.Height)/2))
		return
	}

	// LED ring on the left, square and vertically centered, with spacing.
	rs := size.Height - 20
	r.k.ring.Resize(fyne.NewSize(rs, rs))
	r.k.ring.Move(fyne.NewPos(8, (size.Height-rs)/2))

	textX := 8 + rs + 12
	r.k.label.Move(fyne.NewPos(textX, 8))

	vs := r.k.valTxt.MinSize()
	r.k.valTxt.Resize(vs)
	r.k.valTxt.Move(fyne.NewPos(textX, 22+(size.Height-3-22-vs.Height)/2))
}

func (r *knobRenderer) Refresh() {
	k := r.k
	switch {
	case k.flashing: // queued change — blink the border in the accent color
		if k.flashOn {
			k.glow.FillColor = withAlpha(k.cfg.Accent, 0x40)
			k.plate.StrokeColor = k.cfg.Accent
		} else {
			k.glow.FillColor = transparent
			k.plate.StrokeColor = lcdBorder
		}
	case k.focused:
		k.glow.FillColor = withAlpha(k.cfg.Accent, 0x40)
		k.plate.StrokeColor = withAlpha(k.cfg.Accent, 0x88)
	default:
		k.glow.FillColor = transparent
		k.plate.StrokeColor = lcdBorder
	}
	k.label.Color = lightenColor(k.cfg.Accent, 0.5)
	k.valTxt.Color = lcdText // bright amber — most legible on the dark plate
	k.ring.Image = k.indicatorImage()
	k.label.Text = k.cfg.Label
	k.valTxt.Text = k.cfg.Format(k.value)
	r.place(k.Size())
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *knobRenderer) Objects() []fyne.CanvasObject { return r.objects }

// --- LED segment ring image (supersampled; transparent outside the segments so
// it composites onto the LCD plate) ---

func ringImage(px int, frac float64, accent color.NRGBA) image.Image {
	const ss = 3
	W := px * ss
	src := image.NewRGBA(image.Rect(0, 0, W, W))
	cx, cy := float64(W)/2, float64(W)/2
	R := float64(W)/2 - float64(ss)
	inner, outer := R*0.60, R*0.98
	for y := range W {
		for x := range W {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			dist := math.Hypot(dx, dy)
			if dist < inner || dist > outer {
				continue // transparent (plate shows through)
			}
			f := ringFrac(dx, dy)
			const n = 24.0
			seg := math.Floor(f * n)
			mid := (seg + 0.5) / n
			if math.Abs(f-mid)*n*2 >= 0.62 {
				continue // gap between segments
			}
			if mid <= frac+1e-9 {
				src.Set(x, y, accent)
			} else {
				src.Set(x, y, ledOff)
			}
		}
	}
	return downsampleRing(src, ss, px)
}

// ringFrac maps a pixel direction to 0..1 around a full circle, clockwise from
// the top (12 o'clock).
func ringFrac(dx, dy float64) float64 {
	theta := math.Atan2(dx, -dy)
	if theta < 0 {
		theta += 2 * math.Pi
	}
	return theta / (2 * math.Pi)
}

func downsampleRing(src *image.RGBA, ss, size int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	n := ss * ss
	for y := range size {
		for x := range size {
			var r, g, b, a int
			for j := range ss {
				for i := range ss {
					c := src.RGBAAt(x*ss+i, y*ss+j) // alpha-premultiplied
					r += int(c.R)
					g += int(c.G)
					b += int(c.B)
					a += int(c.A)
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), uint8(a / n)})
		}
	}
	return out
}

// --- lightning-bolt fill indicator (supersampled; transparent outside the bolt) ---

// boltPoly is a lightning-bolt outline in a normalized [0,1]^2 box (y down).
var boltPoly = [][2]float64{
	{0.54, 0.02}, {0.20, 0.55}, {0.44, 0.55},
	{0.30, 0.98}, {0.82, 0.42}, {0.56, 0.42}, {0.72, 0.02},
}

// boltImage renders a lightning bolt filled from the bottom up to frac of its
// height in the accent color; the unlit remainder is dim. Transparent outside
// the bolt so it composites onto the LCD plate.
func boltImage(px int, fill float64, accent color.NRGBA) image.Image {
	const ss = 3
	W := px * ss
	src := image.NewRGBA(image.Rect(0, 0, W, W))
	const pad = 0.08
	scale := func(v float64) float64 { return (pad + v*(1-2*pad)) * float64(W) }
	poly := make([][2]float64, len(boltPoly))
	for i, p := range boltPoly {
		poly[i] = [2]float64{scale(p[0]), scale(p[1])}
	}
	fillTop := 1 - fill // normalized y (0..1) above which the bolt is unlit
	for y := range W {
		for x := range W {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if !pointInPoly(fx, fy, poly) {
				continue
			}
			if fy/float64(W) >= fillTop {
				src.Set(x, y, accent)
			} else {
				src.Set(x, y, ledOff)
			}
		}
	}
	return downsampleRing(src, ss, px)
}

// pointInPoly reports whether (x,y) is inside the polygon (ray casting).
func pointInPoly(x, y float64, poly [][2]float64) bool {
	in := false
	n := len(poly)
	j := n - 1
	for i := range poly {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		if (yi > y) != (yj > y) {
			xint := (xj-xi)*(y-yi)/(yj-yi) + xi
			if x < xint {
				in = !in
			}
		}
		j = i
	}
	return in
}

// --- knob indicators ---

// RingIndicator is the default: an LED segment ring lit up to the value (a
// continuous magnitude).
type RingIndicator struct{}

func (RingIndicator) Image(px, value, min, max, _ int, accent color.NRGBA) image.Image {
	return ringImage(px, frac(value, min, max), accent)
}

// BoltIndicator visualizes the value as a lightning bolt that fills bottom-to-
// top in discrete, evenly-spaced levels (the lowest value shows one level, not
// an empty bolt). Good for a retrigger/roll rate.
type BoltIndicator struct{}

func (BoltIndicator) Image(px, value, min, max, step int, accent color.NRGBA) image.Image {
	steps, idx := stepsIdx(value, min, max, step)
	return boltImage(px, float64(idx+1)/float64(steps), accent)
}

// LanesIndicator visualizes the value as a stack of horizontal lanes, the first
// N (from the bottom) lit — N being the value's 1-based step. Reads as a count
// (e.g. the number of active sequencer tracks).
type LanesIndicator struct{}

func (LanesIndicator) Image(px, value, min, max, step int, accent color.NRGBA) image.Image {
	steps, idx := stepsIdx(value, min, max, step)
	return lanesImage(px, idx+1, steps, accent)
}

// GridIndicator visualizes the value as a Cols×Rows tile grid with the value's
// step tile lit (an index highlight) — a memory/slot selector. If Fill is set,
// all tiles up to and including the active one light instead (a progress feel).
type GridIndicator struct {
	Cols, Rows int
	Fill       bool
}

func (g GridIndicator) Image(px, value, min, max, step int, accent color.NRGBA) image.Image {
	_, idx := stepsIdx(value, min, max, step)
	cols, rows := g.Cols, g.Rows
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return gridImage(px, idx, cols, rows, g.Fill, accent)
}

// lanesImage draws `total` horizontal lanes stacked in a square; the bottom
// `lit` lanes glow in the accent color, the rest are dim. Transparent behind.
func lanesImage(px, lit, total int, accent color.NRGBA) image.Image {
	const ss = 3
	W := px * ss
	src := image.NewRGBA(image.Rect(0, 0, W, W))
	fw := float64(W)
	rowH := fw / float64(total)
	insetX := fw * 0.14
	barH := rowH * 0.62
	rad := barH * 0.5
	for r := range total {
		y0 := float64(r)*rowH + (rowH-barH)/2
		col := ledOff
		if r >= total-lit { // fill from the bottom up
			col = accent
		}
		fillRoundRect(src, insetX, y0, fw-insetX, y0+barH, rad, col)
	}
	return downsampleRing(src, ss, px)
}

// gridImage draws a cols×rows tile grid in a square. In highlight mode only the
// tile at active is lit; with fill, tiles 0..active are lit. Transparent behind.
func gridImage(px, active, cols, rows int, fill bool, accent color.NRGBA) image.Image {
	const ss = 3
	W := px * ss
	src := image.NewRGBA(image.Rect(0, 0, W, W))
	fw := float64(W)
	cellW := fw / float64(cols)
	cellH := fw / float64(rows)
	gap := math.Min(cellW, cellH) * 0.18
	rad := math.Min(cellW, cellH) * 0.22
	for i := range cols * rows {
		cx := i % cols
		cy := i / cols
		lit := i == active
		if fill {
			lit = i <= active
		}
		col := ledOff
		if lit {
			col = accent
		}
		x0 := float64(cx)*cellW + gap/2
		y0 := float64(cy)*cellH + gap/2
		fillRoundRect(src, x0, y0, x0+cellW-gap, y0+cellH-gap, rad, col)
	}
	return downsampleRing(src, ss, px)
}

// fillRoundRect fills a rounded rectangle [x0,y0]-[x1,y1] with corner radius r.
func fillRoundRect(img *image.RGBA, x0, y0, x1, y1, r float64, col color.Color) {
	cx, cy := (x0+x1)/2, (y0+y1)/2
	hx, hy := (x1-x0)/2, (y1-y0)/2
	if r > hx {
		r = hx
	}
	if r > hy {
		r = hy
	}
	for y := int(y0); y < int(math.Ceil(y1)); y++ {
		for x := int(x0); x < int(math.Ceil(x1)); x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			qx := math.Abs(px-cx) - (hx - r)
			qy := math.Abs(py-cy) - (hy - r)
			ox, oy := math.Max(qx, 0), math.Max(qy, 0)
			if math.Hypot(ox, oy)-r <= 0 {
				img.Set(x, y, col)
			}
		}
	}
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

// itoa is a small, allocation-light integer-to-string for the knob's default
// value formatter.
func itoa(v int) string {
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
