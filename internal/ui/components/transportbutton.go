package components

import (
	"embed"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// footSVGs holds the left/right footprint vector art used by the walker toggle.
//
//go:embed assets/footL.svg assets/footR.svg
var footSVGs embed.FS

// Transport button palette.
var (
	transBase  = color.NRGBA{R: 0x18, G: 0x18, B: 0x1C, A: 0xFF}
	transBevel = color.NRGBA{R: 0x52, G: 0x52, B: 0x5A, A: 0xFF}
	playAccent = color.NRGBA{R: 0x4C, G: 0xD9, B: 0x64, A: 0xFF} // green
	stopAccent = color.NRGBA{R: 0xE1, G: 0x4B, B: 0x4B, A: 0xFF} // red
)

type transportKind int

const (
	kindGlyph transportKind = iota
	kindTriangle
)

// TransportButton is an illuminated, hardware-style transport key: an icon on a
// dark beveled base that glows in its accent color when "lit" (e.g. Play while
// the sequencer is running), with a brief press flash.
type TransportButton struct {
	widget.BaseWidget

	kind    transportKind
	accent  color.NRGBA
	lit     bool
	pressed bool
	toggle  bool // play/stop toggle: shows Play when stopped, Stop when running
	running bool
	seq     int
	onTap   func()

	// walker mode: instead of a triangle/square the key shows a top-down trail of
	// footprints (alternating left/right); while running, a glowing footstep
	// travels along the trail (a "tracking" gait); stopped, it's a faint trail.
	walker    bool
	walkPhase float64
	walkAnim  *fyne.Animation
	prints    []*canvas.Image
	printCol  color.NRGBA // last color the footprint images were rendered in

	base    *canvas.Rectangle
	glow    *canvas.Rectangle
	stopImg *canvas.Image // drawn ■ (an image, not a font glyph, so it renders on web)
	iconImg *canvas.Image // drawn play triangle
}

// numPrints is the number of footprints in the walker (a left/right pair).
const numPrints = 2

// NewPlayButton returns a green Play key with a crisp drawn triangle.
func NewPlayButton(onTap func()) *TransportButton {
	b := &TransportButton{kind: kindTriangle, accent: playAccent, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

// NewStopButton returns a red ■ transport key.
func NewStopButton(onTap func()) *TransportButton {
	b := &TransportButton{kind: kindGlyph, accent: stopAccent, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

// NewTransportToggle returns a single key that toggles between Play (green
// triangle, when stopped) and Stop (red ■, when running). onToggle is called
// with the new running state each tap.
func NewTransportToggle(onToggle func(running bool)) *TransportButton {
	b := &TransportButton{kind: kindTriangle, accent: playAccent, toggle: true}
	b.onTap = func() {
		b.running = !b.running
		if b.base != nil {
			b.Refresh()
		}
		if onToggle != nil {
			onToggle(b.running)
		}
	}
	b.ExtendBaseWidget(b)
	return b
}

// NewWalkerToggle returns a play/stop toggle whose icon is a top-down trail of
// footprints: while running a glowing footstep travels along the trail (as if
// something is walking/being tracked); stopped, it's a faint static trail.
// Otherwise it behaves exactly like NewTransportToggle.
func NewWalkerToggle(onToggle func(running bool)) *TransportButton {
	b := &TransportButton{accent: playAccent, toggle: true, walker: true}
	b.onTap = func() {
		b.running = !b.running
		b.updateWalk()
		if b.base != nil {
			b.Refresh()
		}
		if onToggle != nil {
			onToggle(b.running)
		}
	}
	b.ExtendBaseWidget(b)
	return b
}

// updateWalk starts/stops the walking animation to match the running state.
func (b *TransportButton) updateWalk() {
	if !b.walker {
		return
	}
	if b.running {
		if b.walkAnim == nil {
			a := fyne.NewAnimation(1100*time.Millisecond, func(f float32) {
				b.walkPhase = float64(f)
				if b.base != nil {
					b.Refresh()
				}
			})
			a.RepeatCount = fyne.AnimationRepeatForever
			a.Curve = fyne.AnimationLinear
			b.walkAnim = a
		}
		b.walkAnim.Start()
		return
	}
	if b.walkAnim != nil {
		b.walkAnim.Stop()
		b.walkAnim = nil
	}
	b.walkPhase = 0 // back to a faint, even trail
}

// SetRunning sets the play/stop visual state (toggle mode) without firing the
// callback. Use it to reflect externally-driven transport changes.
func (b *TransportButton) SetRunning(running bool) {
	if b.running == running {
		return
	}
	b.running = running
	b.updateWalk()
	if b.base != nil {
		b.Refresh()
	}
}

// SetLit turns the illuminated (active) state on or off.
func (b *TransportButton) SetLit(lit bool) {
	if b.lit == lit {
		return
	}
	b.lit = lit
	if b.base != nil {
		b.Refresh()
	}
}

// AccessibilityLabel describes the action represented by the current state.
func (b *TransportButton) AccessibilityLabel() string {
	if b.toggle {
		if b.running {
			return "Stop"
		}
		return "Play"
	}
	if b.kind == kindTriangle {
		return "Play"
	}
	return "Stop"
}

// AccessibilityRole reports that transport keys are actionable buttons.
func (b *TransportButton) AccessibilityRole() fyne.AccessibleRole {
	return fyne.AccessibleRoleButton
}

// Tapped fires the callback and flashes the key.
func (b *TransportButton) Tapped(_ *fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
	b.pressed = true
	b.seq++
	seq := b.seq
	b.Refresh()
	go func() {
		time.Sleep(110 * time.Millisecond)
		fyne.Do(func() {
			if b.seq != seq {
				return
			}
			b.pressed = false
			b.Refresh()
		})
	}()
}

func (b *TransportButton) CreateRenderer() fyne.WidgetRenderer {
	b.base = canvas.NewRectangle(transBase)
	b.base.CornerRadius = 8
	b.base.StrokeColor = transBevel
	b.base.StrokeWidth = 1

	b.glow = canvas.NewRectangle(color.Transparent)
	b.glow.CornerRadius = 8

	objects := []fyne.CanvasObject{b.base, b.glow}
	if b.walker {
		b.prints = make([]*canvas.Image, numPrints)
		for i := range b.prints {
			img := canvas.NewImageFromResource(footResource(i%2 == 1, b.accent))
			img.FillMode = canvas.ImageFillContain
			img.ScaleMode = canvas.ImageScaleSmooth
			b.prints[i] = img
			objects = append(objects, img)
		}
		b.printCol = b.accent
	} else {
		if b.kind == kindTriangle || b.toggle {
			img := canvas.NewImageFromImage(triangleImage(playAccent))
			img.FillMode = canvas.ImageFillContain
			img.ScaleMode = canvas.ImageScaleSmooth
			b.iconImg = img
			objects = append(objects, img)
		}
		if b.kind == kindGlyph || b.toggle {
			img := canvas.NewImageFromImage(squareImage(stopAccent))
			img.FillMode = canvas.ImageFillContain
			img.ScaleMode = canvas.ImageScaleSmooth
			b.stopImg = img
			objects = append(objects, img)
		}
	}

	r := &transportRenderer{b: b, objects: objects}
	r.apply()
	return r
}

type transportRenderer struct {
	b       *TransportButton
	objects []fyne.CanvasObject
}

func (r *transportRenderer) Destroy() {
	if r.b.walkAnim != nil {
		r.b.walkAnim.Stop()
		r.b.walkAnim = nil
	}
}

func (r *transportRenderer) MinSize() fyne.Size { return fyne.NewSize(74, 46) }

func (r *transportRenderer) Layout(size fyne.Size) {
	r.b.base.Resize(size)
	r.b.base.Move(fyne.NewPos(0, 0))
	r.b.glow.Resize(size)
	r.b.glow.Move(fyne.NewPos(0, 0))

	if r.b.walker {
		r.layoutPrints(size)
		return
	}
	if r.b.iconImg != nil {
		s := size.Height * 0.5
		is := fyne.NewSize(s, s)
		r.b.iconImg.Resize(is)
		r.b.iconImg.Move(fyne.NewPos((size.Width-is.Width)/2, (size.Height-is.Height)/2))
	}
	if r.b.stopImg != nil {
		s := size.Height * 0.42 // a touch smaller than the triangle reads as ■
		is := fyne.NewSize(s, s)
		r.b.stopImg.Resize(is)
		r.b.stopImg.Move(fyne.NewPos((size.Width-is.Width)/2, (size.Height-is.Height)/2))
	}
}

// layoutPrints centers the left/right foot pair inside the key. While running,
// the "stepping" foot lifts slightly (brightness is handled in applyPrints), so
// the pair marches in place; the prints always stay within the button bounds.
func (r *transportRenderer) layoutPrints(size fyne.Size) {
	n := len(r.b.prints)
	if n == 0 {
		return
	}
	h := size.Height * 0.60 // footprints are tall; FillContain keeps their aspect
	w := h * 0.5
	gap := w * 0.30
	totalW := float32(n)*w + float32(n-1)*gap
	x0 := (size.Width - totalW) / 2
	baseY := (size.Height - h) / 2
	lift := size.Height * 0.09
	for i, p := range r.b.prints {
		p.Resize(fyne.NewSize(w, h))
		y := baseY
		if r.b.running {
			y -= lift * float32(stepIntensity(i, r.b.walkPhase))
		}
		p.Move(fyne.NewPos(x0+float32(i)*(w+gap), y))
	}
}

// stepIntensity is the 0..1 "step" strength of foot i at the given phase; the
// feet peak half a cycle apart so they alternate (left, right, left, …).
func stepIntensity(i int, phase float64) float64 {
	return 0.5 * (1 + math.Cos(2*math.Pi*(phase-float64(i)*0.5)))
}

// apply sets colors for the current lit/pressed state.
func (r *transportRenderer) apply() {
	// In toggle mode the visible icon and accent follow the running state.
	if r.b.toggle {
		if r.b.running {
			r.b.accent = stopAccent
		} else {
			r.b.accent = playAccent
		}
		r.b.lit = r.b.running
		if r.b.iconImg != nil {
			setVisibleObj(r.b.iconImg, !r.b.running)
		}
		if r.b.stopImg != nil {
			setVisibleObj(r.b.stopImg, r.b.running)
		}
	}

	a := r.b.accent
	var iconColor color.NRGBA
	switch {
	case r.b.pressed:
		r.b.base.FillColor = darkenTo(a, 0.55)
		r.b.base.StrokeColor = a
		r.b.glow.FillColor = withAlpha(a, 0x55)
		iconColor = lightenColor(a, 0.4)
	case r.b.lit:
		r.b.base.FillColor = darkenTo(a, 0.30)
		r.b.base.StrokeColor = a
		r.b.glow.FillColor = withAlpha(a, 0x33)
		iconColor = lightenColor(a, 0.3)
	default:
		r.b.base.FillColor = transBase
		r.b.base.StrokeColor = transBevel
		r.b.glow.FillColor = color.Transparent
		iconColor = a
	}
	r.b.base.StrokeWidth = 1
	if r.b.lit || r.b.pressed {
		r.b.base.StrokeWidth = 2
	}

	if r.b.iconImg != nil {
		r.b.iconImg.Image = triangleImage(iconColor)
	}
	if r.b.stopImg != nil {
		r.b.stopImg.Image = squareImage(iconColor)
	}
	if r.b.walker {
		r.applyPrints(iconColor)
	}
}

// applyPrints recolors the footprints (only when the color changes) and sets
// each foot's brightness: stopped, both stand fully visible; while running they
// alternate (the stepping foot bright, the other faint) so the pair walks.
func (r *transportRenderer) applyPrints(c color.NRGBA) {
	if c != r.b.printCol {
		for i, p := range r.b.prints {
			p.Resource = footResource(i%2 == 1, c)
		}
		r.b.printCol = c
	}
	for i, p := range r.b.prints {
		if !r.b.running {
			p.Translucency = 0 // a clear standing pair at rest
			continue
		}
		s := stepIntensity(i, r.b.walkPhase)
		p.Translucency = (1 - s) * 0.6 // stepping foot bright, other foot faint
	}
}

func (r *transportRenderer) Refresh() {
	r.apply()
	if r.b.walker {
		r.layoutPrints(r.b.Size()) // reposition for the current step lift
	}
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *transportRenderer) Objects() []fyne.CanvasObject { return r.objects }

// triangleImage renders a right-pointing, antialiased "play" triangle in c.
func triangleImage(c color.NRGBA) image.Image {
	const s = 48
	const ss = 3 // supersampling for smooth edges
	img := image.NewNRGBA(image.Rect(0, 0, s, s))
	ax, ay := 0.16*s, 0.14*s
	bx, by := 0.16*s, 0.86*s
	cx, cy := 0.86*s, 0.50*s
	for y := range s {
		for x := range s {
			cov := 0
			for sy := range ss {
				for sx := range ss {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					if inTriangle(px, py, ax, ay, bx, by, cx, cy) {
						cov++
					}
				}
			}
			if cov > 0 {
				img.SetNRGBA(x, y, color.NRGBA{R: c.R, G: c.G, B: c.B, A: uint8(int(c.A) * cov / (ss * ss))})
			}
		}
	}
	return img
}

func inTriangle(px, py, ax, ay, bx, by, cx, cy float64) bool {
	d1 := edgeSign(px, py, ax, ay, bx, by)
	d2 := edgeSign(px, py, bx, by, cx, cy)
	d3 := edgeSign(px, py, cx, cy, ax, ay)
	hasNeg := d1 < 0 || d2 < 0 || d3 < 0
	hasPos := d1 > 0 || d2 > 0 || d3 > 0
	return !(hasNeg && hasPos)
}

// squareImage renders an antialiased filled "stop" square in c. Drawn as an
// image (not a ■ font glyph) so it renders identically on desktop and on web,
// where the wasm font may lack U+25A0.
func squareImage(c color.NRGBA) image.Image {
	const s = 48
	const ss = 3 // supersampling for smooth edges
	img := image.NewNRGBA(image.Rect(0, 0, s, s))
	lo, hi := 0.16*s, 0.84*s
	for y := range s {
		for x := range s {
			cov := 0
			for sy := range ss {
				for sx := range ss {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					if px >= lo && px <= hi && py >= lo && py <= hi {
						cov++
					}
				}
			}
			if cov > 0 {
				img.SetNRGBA(x, y, color.NRGBA{R: c.R, G: c.G, B: c.B, A: uint8(int(c.A) * cov / (ss * ss))})
			}
		}
	}
	return img
}

// footResource returns the (color-templated) footprint SVG for the left or
// right foot. The color is baked into the resource name so Fyne's raster cache
// keys each color separately.
func footResource(right bool, c color.NRGBA) fyne.Resource {
	name, raw := "footL", footLSVG()
	if right {
		name, raw = "footR", footRSVG()
	}
	hex := fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
	svg := strings.ReplaceAll(raw, "#000000", hex)
	return fyne.NewStaticResource(fmt.Sprintf("%s-%s.svg", name, hex[1:]), []byte(svg))
}

func footLSVG() string { b, _ := footSVGs.ReadFile("assets/footL.svg"); return string(b) }
func footRSVG() string { b, _ := footSVGs.ReadFile("assets/footR.svg"); return string(b) }

func edgeSign(px, py, ax, ay, bx, by float64) float64 {
	return (px-bx)*(ay-by) - (ax-bx)*(py-by)
}

// darkenTo scales an opaque color's channels toward black by factor f (0..1).
func darkenTo(c color.NRGBA, f float64) color.NRGBA {
	return color.NRGBA{R: uint8(float64(c.R) * f), G: uint8(float64(c.G) * f), B: uint8(float64(c.B) * f), A: 0xFF}
}

// lightenColor blends an opaque color toward white by amt (0..1).
func lightenColor(c color.NRGBA, amt float64) color.NRGBA {
	blend := func(v uint8) uint8 { return uint8(float64(v) + (255-float64(v))*amt) }
	return color.NRGBA{R: blend(c.R), G: blend(c.G), B: blend(c.B), A: 0xFF}
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA {
	return color.NRGBA{R: c.R, G: c.G, B: c.B, A: a}
}

// setVisibleObj shows or hides a canvas object.
func setVisibleObj(o fyne.CanvasObject, visible bool) {
	if visible {
		o.Show()
	} else {
		o.Hide()
	}
}
