package components

import (
	"image/color"
	"math"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// LED is a small round indicator light with a soft glow. It is color-agnostic:
// callers set the color (e.g. green/red for status) and lit state. It can
// optionally "breathe" (slowly pulse its glow) via StartPulse.
type LED struct {
	widget.BaseWidget
	col    color.NRGBA
	lit    bool
	pulse  float64 // glow intensity 0..1 (1 = full)
	border bool    // draw an opaque black outer bezel ring around the body

	mu      sync.Mutex
	pulsing bool
	stop    chan struct{}

	glow1 *canvas.Circle // outer, faintest
	glow2 *canvas.Circle // inner halo
	bezel *canvas.Circle // optional black outer border ring
	body  *canvas.Circle
	gloss *canvas.Circle
}

// NewLED returns a lit LED of color c.
func NewLED(c color.NRGBA) *LED {
	l := &LED{col: c, lit: true, pulse: 1}
	l.ExtendBaseWidget(l)
	return l
}

// NewLEDBordered returns a lit LED of color c ringed by an opaque black bezel —
// a mounted-in-the-panel look (e.g. seated in the P-6's yellow plate).
func NewLEDBordered(c color.NRGBA) *LED {
	l := &LED{col: c, lit: true, pulse: 1, border: true}
	l.ExtendBaseWidget(l)
	return l
}

// SetColor changes the LED color.
func (l *LED) SetColor(c color.NRGBA) {
	l.col = c
	if l.body != nil {
		l.Refresh()
	}
}

// Color returns the current LED color.
func (l *LED) Color() color.NRGBA { return l.col }

// StartPulse makes the LED breathe (slowly modulate its glow). Idempotent.
func (l *LED) StartPulse() {
	l.mu.Lock()
	if l.pulsing {
		l.mu.Unlock()
		return
	}
	l.pulsing = true
	l.stop = make(chan struct{})
	stop := l.stop
	l.mu.Unlock()

	go func() {
		phase := 0.0
		coalescedTicker(40*time.Millisecond, stop, func() {
			phase += 0.12 // ~2s per breath
			l.pulse = 0.4 + 0.6*(0.5+0.5*math.Sin(phase))
			if l.body != nil {
				l.Refresh()
			}
		})
	}()
}

// StopPulse stops the breathing animation and restores full glow.
func (l *LED) StopPulse() {
	l.mu.Lock()
	if l.pulsing {
		close(l.stop)
		l.pulsing = false
	}
	l.mu.Unlock()
	l.pulse = 1
	if l.body != nil {
		l.Refresh()
	}
}

// SetLit turns the glow on or off (off = dimmed).
func (l *LED) SetLit(on bool) {
	if l.lit == on {
		return
	}
	l.lit = on
	if l.body != nil {
		l.Refresh()
	}
}

func (l *LED) CreateRenderer() fyne.WidgetRenderer {
	l.glow1 = canvas.NewCircle(color.Transparent)
	l.glow2 = canvas.NewCircle(color.Transparent)
	l.body = canvas.NewCircle(l.col)
	l.body.StrokeColor = color.NRGBA{A: 0x99}
	l.body.StrokeWidth = 1
	l.gloss = canvas.NewCircle(color.Transparent)
	objs := []fyne.CanvasObject{l.glow1, l.glow2}
	if l.border {
		// A dark bezel behind the body with a faintly lit rim stroke, so it reads
		// as a molded ring the LED is seated in rather than a flat black disc. The
		// body sits inset inside it, showing a solid rim around the lit dot.
		l.bezel = canvas.NewCircle(color.NRGBA{R: 0x0A, G: 0x0A, B: 0x0C, A: 0xFF})
		l.bezel.StrokeColor = color.NRGBA{R: 0x55, G: 0x55, B: 0x5E, A: 0xFF}
		l.bezel.StrokeWidth = 1
		objs = append(objs, l.bezel)
	}
	objs = append(objs, l.body, l.gloss)
	r := &ledRenderer{l: l, objects: objs}
	r.apply()
	return r
}

type ledRenderer struct {
	l       *LED
	objects []fyne.CanvasObject
}

func (r *ledRenderer) Destroy() { r.l.StopPulse() }

func (r *ledRenderer) MinSize() fyne.Size { return fyne.NewSize(24, 24) }

func (r *ledRenderer) Layout(size fyne.Size) {
	d := min(size.Width, size.Height)
	cx, cy := size.Width/2, size.Height/2
	place := func(c *canvas.Circle, radius float32) {
		c.Move(fyne.NewPos(cx-radius, cy-radius))
		c.Resize(fyne.NewSize(2*radius, 2*radius))
	}
	bodyR := d * 0.28
	place(r.l.glow1, d*0.50) // soft outer halo
	place(r.l.glow2, d*0.38) // brighter inner halo
	if r.l.bezel != nil {
		place(r.l.bezel, bodyR+d*0.05) // thin dark rim, seated just outside the body
	}
	place(r.l.body, bodyR)

	glossR := bodyR * 0.4
	r.l.gloss.Move(fyne.NewPos(cx-bodyR*0.35-glossR, cy-bodyR*0.35-glossR))
	r.l.gloss.Resize(fyne.NewSize(2*glossR, 2*glossR))
}

func (r *ledRenderer) apply() {
	if r.l.lit {
		r.l.body.FillColor = r.l.col
		r.l.glow1.FillColor = withAlpha(r.l.col, scaleAlpha(0x22, r.l.pulse))
		r.l.glow2.FillColor = withAlpha(r.l.col, scaleAlpha(0x4D, r.l.pulse))
		r.l.gloss.FillColor = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x66}
	} else {
		r.l.body.FillColor = darkenTo(r.l.col, 0.30)
		r.l.glow1.FillColor = color.Transparent
		r.l.glow2.FillColor = color.Transparent
		r.l.gloss.FillColor = color.Transparent
	}
}

func scaleAlpha(a uint8, f float64) uint8 {
	return uint8(float64(a) * f)
}

func (r *ledRenderer) Refresh() {
	r.apply()
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *ledRenderer) Objects() []fyne.CanvasObject { return r.objects }
