package components

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// RackCycle is a backlit rack-panel control that cycles through a fixed set of
// states on each tap, showing a per-state icon. It shares the RackToggle plate
// look (dark inset plate + accent backlight) but, unlike an on/off toggle, it is
// always lit: it's an active *selector* whose icon tells you which state you're
// in. Tapping advances to the next state (wrapping) and fires OnChange.
//
// Used for the pad rack's layout selector (paged / two-bank / dense grid icons).
type RackCycle struct {
	widget.BaseWidget
	icons  []fyne.Resource
	accent color.NRGBA
	state  int

	onChange func(int)
	hovered  bool

	glow *canvas.Rectangle
	bg   *canvas.Rectangle
	img  *canvas.Image
}

// NewRackCycle returns a cycle selector showing icons[state], backlit in accent.
// onChange fires with the new state index whenever the control is tapped.
func NewRackCycle(icons []fyne.Resource, accent color.NRGBA, onChange func(int)) *RackCycle {
	c := &RackCycle{icons: icons, accent: accent, onChange: onChange}
	c.ExtendBaseWidget(c)
	return c
}

// State returns the current state index.
func (c *RackCycle) State() int { return c.state }

// AccessibilityLabel identifies the active selector icon and state.
func (c *RackCycle) AccessibilityLabel() string {
	if icon := c.currentIcon(); icon != nil {
		return icon.Name()
	}
	return "Rack selector"
}

// AccessibilityRole reports that a cycle selector is an actionable button.
func (c *RackCycle) AccessibilityRole() fyne.AccessibleRole {
	return fyne.AccessibleRoleButton
}

// SetState sets the current state (clamped/wrapped into range) without firing
// OnChange.
func (c *RackCycle) SetState(i int) {
	if len(c.icons) == 0 {
		return
	}
	i %= len(c.icons)
	if i < 0 {
		i += len(c.icons)
	}
	c.state = i
	if c.img != nil {
		c.Refresh()
	}
}

// SetAccent changes the lit backlight color.
func (c *RackCycle) SetAccent(a color.NRGBA) {
	c.accent = a
	if c.bg != nil {
		c.Refresh()
	}
}

// MouseIn/MouseMoved/MouseOut implement desktop.Hoverable for the hover glow.
func (c *RackCycle) MouseIn(*desktop.MouseEvent) {
	c.hovered = true
	if c.bg != nil {
		c.Refresh()
	}
}

func (c *RackCycle) MouseMoved(*desktop.MouseEvent) {}

func (c *RackCycle) MouseOut() {
	c.hovered = false
	if c.bg != nil {
		c.Refresh()
	}
}

// Tapped advances to the next state and fires OnChange.
func (c *RackCycle) Tapped(*fyne.PointEvent) {
	if len(c.icons) == 0 {
		return
	}
	c.state = (c.state + 1) % len(c.icons)
	if c.img != nil {
		c.Refresh()
	}
	if c.onChange != nil {
		c.onChange(c.state)
	}
}

func (c *RackCycle) CreateRenderer() fyne.WidgetRenderer {
	c.glow = canvas.NewRectangle(color.Transparent)
	c.glow.CornerRadius = 6
	c.bg = canvas.NewRectangle(badgeBg)
	c.bg.CornerRadius = 5
	c.bg.StrokeColor = badgeStroke
	c.bg.StrokeWidth = 1.5

	c.img = canvas.NewImageFromResource(c.currentIcon())
	c.img.FillMode = canvas.ImageFillContain

	r := &rackCycleRenderer{c: c, objects: []fyne.CanvasObject{c.glow, c.bg, c.img}}
	r.apply()
	return r
}

func (c *RackCycle) currentIcon() fyne.Resource {
	if c.state >= 0 && c.state < len(c.icons) {
		return c.icons[c.state]
	}
	return nil
}

type rackCycleRenderer struct {
	c       *RackCycle
	objects []fyne.CanvasObject
}

func (r *rackCycleRenderer) Destroy() {}

func (r *rackCycleRenderer) MinSize() fyne.Size {
	return fyne.NewSize(rackToggleIcon+14, rackToggleIcon+14)
}

func (r *rackCycleRenderer) Layout(size fyne.Size) {
	r.c.glow.Resize(size)
	r.c.glow.Move(fyne.NewPos(0, 0))

	inset := float32(2)
	r.c.bg.Resize(fyne.NewSize(size.Width-2*inset, size.Height-2*inset))
	r.c.bg.Move(fyne.NewPos(inset, inset))

	s := float32(rackToggleIcon)
	r.c.img.Resize(fyne.NewSize(s, s))
	r.c.img.Move(fyne.NewPos((size.Width-s)/2, (size.Height-s)/2))
}

func (r *rackCycleRenderer) apply() {
	c := r.c
	// Always lit (an active selector); hovering brightens the backlight.
	glowA, strokeA := uint8(0x40), uint8(0x88)
	if c.hovered {
		glowA, strokeA = 0x6A, 0xC0
	}
	c.glow.FillColor = withAlpha(c.accent, glowA)
	c.bg.FillColor = badgeBg
	c.bg.StrokeColor = withAlpha(c.accent, strokeA)
	c.img.Resource = c.currentIcon()
	c.img.Translucency = 0
}

func (r *rackCycleRenderer) Refresh() {
	r.apply()
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *rackCycleRenderer) Objects() []fyne.CanvasObject { return r.objects }
