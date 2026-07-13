package components

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// Piano key colors: light "ivory" whites and near-black sharps, with a dark
// stroke so adjacent keys read as separate. A pressed key blends toward the
// accent color (the tap-flash).
var (
	pianoWhite     = color.NRGBA{R: 0xDA, G: 0xDD, B: 0xE2, A: 0xFF}
	pianoBlack     = color.NRGBA{R: 0x1B, G: 0x1C, B: 0x20, A: 0xFF}
	pianoKeyStroke = color.NRGBA{R: 0x0E, G: 0x0F, B: 0x12, A: 0xFF}
	pianoWhiteText = color.NRGBA{R: 0x33, G: 0x36, B: 0x3B, A: 0xFF}
)

const (
	pianoFlashMs      = 110
	pianoMinWhiteW    = 22   // per white-key minimum width
	pianoMinHeight    = 44   // compact strip; keeps the window's min height low so it can still maximize
	pianoBlackWFrac   = 0.62 // black-key width as a fraction of a white key
	pianoBlackHFrac   = 0.60 // black-key height as a fraction of the keyboard
	pianoLabelPadding = 4
)

// isBlackKey reports whether the chromatic semitone offset i (0 = the leftmost
// key, taken as C) is a black key (C#, D#, F#, G#, A#).
func isBlackKey(i int) bool {
	switch ((i % 12) + 12) % 12 {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

// PianoConfig configures a PianoKeyboard.
type PianoConfig struct {
	// MinWhite / MaxWhite bound how many white keys are shown; the keyboard
	// grows the count to fill the available width (default 9..36). MaxWhite caps
	// how many key widgets are created.
	MinWhite int
	MaxWhite int
	// WhiteW is the target white-key width used to pick the key count for a
	// given width (default 54). Keys then stretch to fill the width exactly.
	WhiteW float32
	// Accent tints a key while it is pressed (the tap-flash).
	Accent color.NRGBA
	// OnNote fires when a key is tapped, with its 0-based semitone index from C.
	OnNote func(index int)
	// Label optionally captions each key (rendered on white keys only). Called
	// on build and on Refresh, so callers can relabel when an octave changes.
	Label func(index int) string
}

// PianoKeyboard is a generic, touch-friendly chromatic piano keyboard: a row of
// white keys with raised black keys, laid out like a real keyboard. It fills the
// available width, growing the number of keys as the window widens (down to a
// minimum). It is device-agnostic — the caller maps a key's index to a MIDI note
// via OnNote.
type PianoKeyboard struct {
	widget.BaseWidget
	cfg     PianoConfig
	keys    []*pianoKey
	minH    float32 // minimum keyboard height (keys fill it); see SetMinHeight
	visible int     // number of keys currently shown (updated on Layout)
}

// chromaticForWhites returns the number of chromatic keys (starting at C) whose
// white-key count is exactly w, ending on a white key (so the keyboard fills its
// width with no trailing black-key overhang).
func chromaticForWhites(w int) int {
	if w <= 0 {
		return 0
	}
	whites := 0
	for i := 0; ; i++ {
		if !isBlackKey(i) {
			whites++
			if whites == w {
				return i + 1
			}
		}
	}
}

// NewPianoKeyboard builds a keyboard from cfg.
func NewPianoKeyboard(cfg PianoConfig) *PianoKeyboard {
	if cfg.MinWhite <= 0 {
		cfg.MinWhite = 9
	}
	if cfg.MaxWhite < cfg.MinWhite {
		cfg.MaxWhite = 36
	}
	if cfg.WhiteW <= 0 {
		cfg.WhiteW = 54
	}
	p := &PianoKeyboard{cfg: cfg, minH: pianoMinHeight}
	p.visible = chromaticForWhites(cfg.MinWhite)
	for i := range chromaticForWhites(cfg.MaxWhite) {
		p.keys = append(p.keys, newPianoKey(i, isBlackKey(i), cfg.Accent))
	}
	p.ExtendBaseWidget(p)
	p.updateLabels()
	return p
}

// AccessibilityLabel names the composite keyboard. Individual keys are drawn
// inside this widget, so the current Fyne accessibility API cannot expose them
// as separately actionable nodes.
func (p *PianoKeyboard) AccessibilityLabel() string { return "Piano keyboard" }

// AccessibilityRole reports that the keyboard is an actionable control.
func (p *PianoKeyboard) AccessibilityRole() fyne.AccessibleRole {
	return fyne.AccessibleRoleButton
}

// Tapped plays the key under the tap. The keyboard hit-tests taps itself (rather
// than making each key a tappable widget) so overlapping black keys reliably
// take precedence over the white keys beneath them — the standard piano
// behavior, and it fixes stray/half-height hits from sibling-widget overlap.
// fyne.Tappable.
func (p *PianoKeyboard) Tapped(e *fyne.PointEvent) {
	if k := p.keyAt(e.Position); k != nil {
		if p.cfg.OnNote != nil {
			p.cfg.OnNote(k.index)
		}
		k.flash()
	}
}

// keyAt returns the visible key at pos, or nil. Black keys (drawn on top, in the
// upper region) are tested first so a tap in their area hits them rather than
// the white key beneath. Keys are positioned by the renderer's Layout, so this
// reads their live geometry.
func (p *PianoKeyboard) keyAt(pos fyne.Position) *pianoKey {
	hit := func(k *pianoKey) bool {
		if !k.Visible() {
			return false
		}
		kp, ks := k.Position(), k.Size()
		return pos.X >= kp.X && pos.X < kp.X+ks.Width &&
			pos.Y >= kp.Y && pos.Y < kp.Y+ks.Height
	}
	for _, k := range p.keys {
		if k.black && hit(k) {
			return k
		}
	}
	for _, k := range p.keys {
		if !k.black && hit(k) {
			return k
		}
	}
	return nil
}

// Refresh re-applies the key labels (e.g. after an octave change) and repaints.
func (p *PianoKeyboard) Refresh() {
	p.updateLabels()
	p.BaseWidget.Refresh()
}

// SetMinHeight sets the keyboard's minimum height; the keys stretch to fill it
// while the caller's surrounding controls (e.g. an octave knob) keep their size.
// A value <= 0 resets to the default compact height. Used to make the keyboard
// taller where there's room (the console/full-screen layout).
func (p *PianoKeyboard) SetMinHeight(h float32) {
	if h <= 0 {
		h = pianoMinHeight
	}
	if h == p.minH {
		return
	}
	p.minH = h
	p.Refresh()
}

// VisibleKeys reports how many chromatic keys are currently shown (it grows with
// width). Callers use it to keep an external note within the on-screen range.
func (p *PianoKeyboard) VisibleKeys() int { return p.visible }

// FlashKey briefly lights the key at the given semitone index (0 = the leftmost
// C), if that key is currently visible — used to echo an external MIDI note
// press on the on-screen keyboard. Out-of-range/hidden indices are ignored.
func (p *PianoKeyboard) FlashKey(index int) {
	if index < 0 || index >= len(p.keys) {
		return
	}
	if k := p.keys[index]; k.Visible() {
		k.flash()
	}
}

func (p *PianoKeyboard) updateLabels() {
	for i, k := range p.keys {
		if k.black || p.cfg.Label == nil {
			k.setLabel("") // labels only on white keys
			continue
		}
		k.setLabel(p.cfg.Label(i))
	}
}

func (p *PianoKeyboard) CreateRenderer() fyne.WidgetRenderer {
	// White keys first, black keys last: later objects render on top, so the
	// raised black keys both draw over and receive taps ahead of the whites in
	// their overlapping region.
	var whites, blacks []fyne.CanvasObject
	for _, k := range p.keys {
		if k.black {
			blacks = append(blacks, k)
		} else {
			whites = append(whites, k)
		}
	}
	return &pianoRenderer{p: p, objects: append(whites, blacks...)}
}

type pianoRenderer struct {
	p       *PianoKeyboard
	objects []fyne.CanvasObject
}

func (r *pianoRenderer) Destroy() {}

func (r *pianoRenderer) MinSize() fyne.Size {
	return fyne.NewSize(float32(r.p.cfg.MinWhite)*pianoMinWhiteW, r.p.minH)
}

// whiteCount picks how many white keys to show at the given width: as many as
// fit at the target white-key width, clamped to [MinWhite, MaxWhite].
func (r *pianoRenderer) whiteCount(width float32) int {
	w := int(width / r.p.cfg.WhiteW)
	w = max(w, r.p.cfg.MinWhite)
	w = min(w, r.p.cfg.MaxWhite)
	return w
}

func (r *pianoRenderer) Layout(size fyne.Size) {
	white := r.whiteCount(size.Width)
	n := chromaticForWhites(white) // visible chromatic keys (ends on a white key)
	if n == 0 {
		return
	}
	r.p.visible = n
	ww := size.Width / float32(white) // fills the width exactly
	bw := ww * pianoBlackWFrac
	bh := size.Height * pianoBlackHFrac

	whiteIdx := 0 // number of white keys placed so far
	for i, k := range r.p.keys {
		if i >= n {
			if k.Visible() {
				k.Hide()
			}
			continue
		}
		if !k.Visible() {
			k.Show()
		}
		if k.black {
			// The black key straddles the boundary after the last white key.
			cx := float32(whiteIdx) * ww
			k.Move(fyne.NewPos(cx-bw/2, 0))
			k.Resize(fyne.NewSize(bw, bh))
			continue
		}
		k.Move(fyne.NewPos(float32(whiteIdx)*ww, 0))
		k.Resize(fyne.NewSize(ww, size.Height))
		whiteIdx++
	}
}

func (r *pianoRenderer) Refresh() {
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *pianoRenderer) Objects() []fyne.CanvasObject { return r.objects }

// pianoKey is one key's visuals (background + label) plus its flash state. It is
// NOT tappable itself — the parent PianoKeyboard hit-tests taps by coordinate
// (see keyAt) so overlapping black/white keys resolve correctly. The parent
// positions/sizes it.
type pianoKey struct {
	widget.BaseWidget
	index  int
	black  bool
	accent color.NRGBA
	label  string

	pressed  bool
	flashSeq int

	bg  *canvas.Rectangle
	txt *canvas.Text
}

func newPianoKey(index int, black bool, accent color.NRGBA) *pianoKey {
	k := &pianoKey{index: index, black: black, accent: accent}
	k.ExtendBaseWidget(k)
	return k
}

func (k *pianoKey) setLabel(s string) {
	k.label = s
	if k.txt != nil {
		k.txt.Text = s
		k.txt.Refresh()
	}
}

func (k *pianoKey) flash() {
	k.flashSeq++
	seq := k.flashSeq
	k.pressed = true
	if k.bg != nil {
		k.Refresh()
	}
	go func() {
		time.Sleep(pianoFlashMs * time.Millisecond)
		fyne.Do(func() {
			if k.flashSeq != seq {
				return
			}
			k.pressed = false
			if k.bg != nil {
				k.Refresh()
			}
		})
	}()
}

func (k *pianoKey) idleColor() color.NRGBA {
	if k.black {
		return pianoBlack
	}
	return pianoWhite
}

func (k *pianoKey) labelColor() color.NRGBA {
	if k.black {
		return color.NRGBA{R: 0xC8, G: 0xCC, B: 0xD2, A: 0xFF}
	}
	return pianoWhiteText
}

func (k *pianoKey) CreateRenderer() fyne.WidgetRenderer {
	k.bg = canvas.NewRectangle(k.idleColor())
	k.bg.CornerRadius = 3
	k.bg.StrokeColor = pianoKeyStroke
	k.bg.StrokeWidth = 1
	k.txt = canvas.NewText(k.label, k.labelColor())
	k.txt.TextSize = 10
	k.txt.Alignment = fyne.TextAlignCenter
	r := &pianoKeyRenderer{k: k, objects: []fyne.CanvasObject{k.bg, k.txt}}
	r.apply()
	return r
}

type pianoKeyRenderer struct {
	k       *pianoKey
	objects []fyne.CanvasObject
}

func (r *pianoKeyRenderer) Destroy() {}

func (r *pianoKeyRenderer) MinSize() fyne.Size {
	if r.k.black {
		return fyne.NewSize(pianoMinWhiteW*pianoBlackWFrac, pianoMinHeight*pianoBlackHFrac)
	}
	return fyne.NewSize(pianoMinWhiteW, pianoMinHeight)
}

func (r *pianoKeyRenderer) Layout(size fyne.Size) {
	r.k.bg.Resize(size)
	r.k.bg.Move(fyne.NewPos(0, 0))
	ts := r.k.txt.MinSize()
	r.k.txt.Resize(fyne.NewSize(size.Width, ts.Height))
	r.k.txt.Move(fyne.NewPos(0, size.Height-ts.Height-pianoLabelPadding))
}

func (r *pianoKeyRenderer) apply() {
	base := r.k.idleColor()
	if r.k.pressed {
		base = blendNRGBA(base, r.k.accent, 0.75)
	}
	r.k.bg.FillColor = base
	r.k.txt.Color = r.k.labelColor()
	r.k.txt.Text = r.k.label
}

func (r *pianoKeyRenderer) Refresh() {
	r.apply()
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *pianoKeyRenderer) Objects() []fyne.CanvasObject { return r.objects }
