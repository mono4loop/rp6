package components

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// RackToggle is a backlit synth-panel control that toggles something on/off. It
// shares the DeviceBadge plate look (dark inset plate + accent backlight) but
// has no status LED: when on it lights up in its accent color, and when off it
// greys out. It shows either a bold caption or an icon.
//
// A plain tap fires OnTap; a Ctrl+click fires OnCtrlTap when set (e.g. an
// "assign" alternate on a mute toggle). The caller flips the underlying state
// and calls SetOn to reflect it (mirroring the old toggle-button pattern).
//
// Used for the bottom-bar section toggles (PADS/DLY-REV/FX/SEQ/VU, text), the
// pad rack's left tool strip (float/listen/density, icons), and the sequencer's
// per-track mute headers (pad-label text, tinted with the track's bank color).
type RackToggle struct {
	widget.BaseWidget
	label  string
	icon   fyne.Resource
	accent color.NRGBA
	on     bool

	onTap     func()
	onCtrlTap func()
	ctrl      bool // modifier captured at the last MouseDown
	hovered   bool // pointer is over the toggle (for the hover-glow affordance)
	armed     bool // lit hardest — an explicit "waiting for input" state
	disabled  bool // greyed and inert (e.g. a P-6-only rack with no P-6 connected)

	flashing bool // momentary confirmation blink (see Flash)
	flashOn  bool

	glow *canvas.Rectangle
	bg   *canvas.Rectangle
	txt  *canvas.Text
	img  *canvas.Image
}

const rackToggleIcon = 18 // icon square size (icon mode)

// NewRackToggle returns a text toggle captioned label, backlit in accent when on.
func NewRackToggle(label string, accent color.NRGBA, onTap func()) *RackToggle {
	t := &RackToggle{label: label, accent: accent, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

// NewRackToggleIcon returns an icon toggle: the icon shows full-strength (with a
// backlight) when on and faded when off.
func NewRackToggleIcon(icon fyne.Resource, accent color.NRGBA, onTap func()) *RackToggle {
	t := &RackToggle{icon: icon, accent: accent, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

// SetOnCtrlTap sets an alternate action fired on Ctrl+click.
func (t *RackToggle) SetOnCtrlTap(fn func()) { t.onCtrlTap = fn }

// Flash blinks the toggle a few times in its accent color — a momentary
// confirmation for action buttons (copy/clear/save) that have no lasting on
// state. No-op while a flash is already in progress. Call it from the UI thread.
func (t *RackToggle) Flash() {
	if t.flashing {
		return
	}
	t.flashing = true
	go func() {
		const phases = 6 // three quick blinks
		for i := range phases {
			on := i%2 == 0
			fyne.Do(func() {
				t.flashOn = on
				if t.bg != nil {
					t.Refresh()
				}
			})
			time.Sleep(80 * time.Millisecond)
		}
		fyne.Do(func() {
			t.flashing = false
			t.flashOn = false
			if t.bg != nil {
				t.Refresh()
			}
		})
	}()
}

// SetOn sets the lit (on) / greyed (off) state.
func (t *RackToggle) SetOn(on bool) {
	if t.on == on {
		return
	}
	t.on = on
	if t.bg != nil {
		t.Refresh()
	}
}

// On reports whether the toggle is lit.
func (t *RackToggle) On() bool { return t.on }

// SetArmed lights the toggle at its strongest ("armed" / waiting-for-input),
// standing out from the normal lit state. Used to signal an actionable toggle
// that's waiting for a follow-up gesture (e.g. a sequencer track waiting for a
// pad to assign).
func (t *RackToggle) SetArmed(on bool) {
	if t.armed == on {
		return
	}
	t.armed = on
	if t.bg != nil {
		t.Refresh()
	}
}

// Armed reports whether the toggle is in the armed state.
func (t *RackToggle) Armed() bool { return t.armed }

// SetDisabled greys the toggle out and makes it inert (taps and hover glow are
// ignored) — used when the control it toggles isn't available, e.g. a P-6-only
// rack while the emulator backend is active. A disabled toggle also drops its
// hover state so it doesn't linger lit.
func (t *RackToggle) SetDisabled(on bool) {
	if t.disabled == on {
		return
	}
	t.disabled = on
	if on {
		t.hovered = false
	}
	if t.bg != nil {
		t.Refresh()
	}
}

// Disabled reports whether the toggle is inert.
func (t *RackToggle) Disabled() bool { return t.disabled }

// SetLabel updates the caption (text mode).
func (t *RackToggle) SetLabel(s string) {
	t.label = s
	if t.txt != nil {
		t.Refresh()
	}
}

// SetIcon updates the icon (icon mode).
func (t *RackToggle) SetIcon(res fyne.Resource) {
	t.icon = res
	if t.img != nil {
		t.img.Resource = res
		t.Refresh()
	}
}

// SetAccent changes the lit backlight/caption color.
func (t *RackToggle) SetAccent(c color.NRGBA) {
	t.accent = c
	if t.bg != nil {
		t.Refresh()
	}
}

// MouseDown records whether Control was held (desktop.Mouseable).
func (t *RackToggle) MouseDown(e *desktop.MouseEvent) {
	t.ctrl = e.Modifier&fyne.KeyModifierControl != 0
}

// MouseUp satisfies desktop.Mouseable.
func (t *RackToggle) MouseUp(*desktop.MouseEvent) {}

// MouseIn/MouseMoved/MouseOut implement desktop.Hoverable: while the pointer is
// over an inactive toggle it glows faintly in its accent color, hinting that it
// can be activated.
func (t *RackToggle) MouseIn(*desktop.MouseEvent) {
	if t.disabled {
		return
	}
	t.hovered = true
	if t.bg != nil {
		t.Refresh()
	}
}

func (t *RackToggle) MouseMoved(*desktop.MouseEvent) {}

func (t *RackToggle) MouseOut() {
	t.hovered = false
	if t.bg != nil {
		t.Refresh()
	}
}

// Tapped fires OnCtrlTap on a Ctrl+click (if set), else OnTap.
func (t *RackToggle) Tapped(*fyne.PointEvent) {
	if t.disabled {
		return
	}
	ctrl := t.ctrl
	t.ctrl = false
	if ctrl && t.onCtrlTap != nil {
		t.onCtrlTap()
		return
	}
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *RackToggle) CreateRenderer() fyne.WidgetRenderer {
	t.glow = canvas.NewRectangle(color.Transparent)
	t.glow.CornerRadius = 6
	t.bg = canvas.NewRectangle(badgeBg)
	t.bg.CornerRadius = 5
	t.bg.StrokeColor = badgeStroke
	t.bg.StrokeWidth = 1.5

	objs := []fyne.CanvasObject{t.glow, t.bg}
	if t.icon != nil {
		t.img = canvas.NewImageFromResource(t.icon)
		t.img.FillMode = canvas.ImageFillContain
		objs = append(objs, t.img)
	} else {
		t.txt = canvas.NewText(t.label, badgeNameDim)
		t.txt.TextStyle = fyne.TextStyle{Bold: true}
		t.txt.TextSize = 13
		objs = append(objs, t.txt)
	}

	r := &rackToggleRenderer{t: t, objects: objs}
	r.apply()
	return r
}

type rackToggleRenderer struct {
	t       *RackToggle
	objects []fyne.CanvasObject
}

func (r *rackToggleRenderer) Destroy() {}

func (r *rackToggleRenderer) MinSize() fyne.Size {
	if r.t.img != nil {
		return fyne.NewSize(rackToggleIcon+14, rackToggleIcon+14)
	}
	ts := r.t.txt.MinSize()
	return fyne.NewSize(ts.Width+26, ts.Height+16)
}

func (r *rackToggleRenderer) Layout(size fyne.Size) {
	r.t.glow.Resize(size)
	r.t.glow.Move(fyne.NewPos(0, 0))

	inset := float32(2)
	r.t.bg.Resize(fyne.NewSize(size.Width-2*inset, size.Height-2*inset))
	r.t.bg.Move(fyne.NewPos(inset, inset))

	if r.t.img != nil {
		s := float32(rackToggleIcon)
		r.t.img.Resize(fyne.NewSize(s, s))
		r.t.img.Move(fyne.NewPos((size.Width-s)/2, (size.Height-s)/2))
		return
	}
	ts := r.t.txt.MinSize()
	r.t.txt.Move(fyne.NewPos((size.Width-ts.Width)/2, (size.Height-ts.Height)/2))
}

func (r *rackToggleRenderer) apply() {
	t := r.t
	lit := t.on
	if t.flashing {
		lit = t.flashOn
	}
	switch {
	case t.disabled:
		// Inert: greyed harder than the plain "off" state, no glow, so it
		// visibly reads as unavailable rather than merely toggled off.
		t.glow.FillColor = color.Transparent
		t.bg.FillColor = badgeBg
		t.bg.StrokeColor = badgeStroke
		if t.txt != nil {
			t.txt.Color = withAlpha(badgeNameDim, 0x55)
		}
		if t.img != nil {
			t.img.Translucency = 0.75
		}
	case t.armed:
		// Armed: the whole plate floods with the accent color (an inverted,
		// solid look) plus a strong halo — unmistakably different from the
		// subtle glow of the lit/hover states, signalling "waiting for input".
		t.glow.FillColor = withAlpha(t.accent, 0xC8)
		t.bg.FillColor = t.accent
		t.bg.StrokeColor = lightenColor(t.accent, 0.6)
		if t.txt != nil {
			t.txt.Color = badgeBg // dark text reads on the bright plate
		}
		if t.img != nil {
			t.img.Translucency = 0
		}
	case lit:
		// Lit (on). Since a rack toggle is always actionable, hovering brightens
		// the backlight a touch to reinforce that it's clickable.
		glowA, strokeA := uint8(0x40), uint8(0x88)
		if t.hovered {
			glowA, strokeA = 0x6A, 0xC0
		}
		t.glow.FillColor = withAlpha(t.accent, glowA)
		t.bg.FillColor = badgeBg
		t.bg.StrokeColor = withAlpha(t.accent, strokeA)
		if t.txt != nil {
			c := 0.5
			if t.hovered {
				c = 0.7
			}
			t.txt.Color = lightenColor(t.accent, c)
		}
		if t.img != nil {
			t.img.Translucency = 0
		}
	case t.hovered:
		// inactive but hovered: a faint accent glow hints it can be activated
		t.glow.FillColor = withAlpha(t.accent, 0x1E)
		t.bg.FillColor = badgeBg
		t.bg.StrokeColor = withAlpha(t.accent, 0x55)
		if t.txt != nil {
			t.txt.Color = lightenColor(badgeNameDim, 0.30)
		}
		if t.img != nil {
			t.img.Translucency = 0.3
		}
	default:
		t.glow.FillColor = color.Transparent
		t.bg.FillColor = badgeBg
		t.bg.StrokeColor = badgeStroke
		if t.txt != nil {
			t.txt.Color = badgeNameDim
		}
		if t.img != nil {
			t.img.Translucency = 0.55 // faded/greyed when off
		}
	}
	if t.txt != nil {
		t.txt.Text = t.label
	}
}

func (r *rackToggleRenderer) Refresh() {
	r.apply()
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *rackToggleRenderer) Objects() []fyne.CanvasObject { return r.objects }
