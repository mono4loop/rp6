package components

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// DeviceState is the connection lifecycle a DeviceBadge reflects.
type DeviceState int

const (
	// DeviceOffline: nothing connected (or lost). Red LED, dim plate.
	DeviceOffline DeviceState = iota
	// DeviceSearching: mid-connect / probing. Amber LED, dim accent plate.
	DeviceSearching
	// DeviceOnline: connected and live. Full accent backlight, bright name.
	DeviceOnline
)

// Fixed badge footprint — constant regardless of the device name/tag, so the
// toolbar never reflows when the device (and its label) changes.
const (
	badgeWidth  = 178
	badgeHeight = 46
)

// DeviceBadge is a backlit synth-panel "nameplate": a dark inset plate that
// names the connected device (e.g. "P-6" or "EMULATOR") with a small mode tag,
// a status LED, and a settings gear, backlit in the device's accent color when
// online.
//
// It is generic (no device knowledge): the caller supplies the name, tag and
// accent color, drives the connection state, and wires the actions. The accent
// color doubles as a device-identity cue (e.g. amber for hardware, cyan for the
// emulator). A tap on the gear runs OnSettings; a tap anywhere else on the plate
// runs OnToggle (used to switch backends).
type DeviceBadge struct {
	widget.BaseWidget
	name   string
	tag    string
	accent color.NRGBA
	state  DeviceState

	onSettings func()
	onToggle   func()

	glow     *canvas.Rectangle
	bg       *canvas.Rectangle
	led      *LED
	nm       *canvas.Text
	tg       *canvas.Text
	settings *widget.Button
}

// Badge plate palette (matches the SevenSeg/rack inset look).
var (
	badgeBg       = color.NRGBA{R: 0x10, G: 0x11, B: 0x14, A: 0xFF}
	badgeStroke   = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xCC}
	badgeNameDim  = color.NRGBA{R: 0x6A, G: 0x6A, B: 0x72, A: 0xFF}
	badgeTagColor = color.NRGBA{R: 0x8A, G: 0x8A, B: 0x92, A: 0xFF}
	badgeAmber    = color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}
	badgeRed      = color.NRGBA{R: 0xE1, G: 0x4B, B: 0x4B, A: 0xFF}
)

// NewDeviceBadge returns a badge showing name/tag with the given accent color,
// initially offline.
func NewDeviceBadge(name, tag string, accent color.NRGBA) *DeviceBadge {
	b := &DeviceBadge{name: name, tag: tag, accent: accent, state: DeviceOffline}
	b.ExtendBaseWidget(b)
	return b
}

// OnSettings sets the callback run when the gear is tapped (a device-specific
// settings window, eventually).
func (b *DeviceBadge) OnSettings(fn func()) { b.onSettings = fn }

// OnToggle sets the callback run on a tap of the plate (switch backend).
func (b *DeviceBadge) OnToggle(fn func()) { b.onToggle = fn }

// SetName updates the device name and mode tag (e.g. after a reconnect changes
// what's plugged in). The footprint stays fixed.
func (b *DeviceBadge) SetName(name, tag string) {
	b.name, b.tag = name, tag
	if b.nm != nil {
		b.Refresh()
	}
}

// SetAccent changes the device-identity backlight color.
func (b *DeviceBadge) SetAccent(c color.NRGBA) {
	b.accent = c
	if b.nm != nil {
		b.Refresh()
	}
}

// SetState updates the connection state (offline/searching/online).
func (b *DeviceBadge) SetState(s DeviceState) {
	if b.state == s {
		return
	}
	b.state = s
	if b.nm != nil {
		b.Refresh()
	}
}

// State returns the current connection state.
func (b *DeviceBadge) State() DeviceState { return b.state }

// Tapped runs the toggle action when the plate is tapped (switch backend). Taps
// on the settings gear are consumed by the gear button and never reach here.
func (b *DeviceBadge) Tapped(*fyne.PointEvent) {
	if b.onToggle != nil {
		b.onToggle()
	}
}

func (b *DeviceBadge) CreateRenderer() fyne.WidgetRenderer {
	b.glow = canvas.NewRectangle(color.Transparent)
	b.glow.CornerRadius = 6
	b.bg = canvas.NewRectangle(badgeBg)
	b.bg.CornerRadius = 5
	b.bg.StrokeColor = badgeStroke
	b.bg.StrokeWidth = 1.5
	b.led = NewLED(b.accent)
	b.nm = canvas.NewText(b.name, badgeNameDim)
	b.nm.TextStyle = fyne.TextStyle{Bold: true}
	b.nm.TextSize = 15
	b.tg = canvas.NewText(b.tag, badgeTagColor)
	b.tg.TextSize = 9
	b.settings = widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		if b.onSettings != nil {
			b.onSettings()
		}
	})
	b.settings.Importance = widget.LowImportance

	r := &deviceBadgeRenderer{b: b, objects: []fyne.CanvasObject{b.glow, b.bg, b.led, b.nm, b.tg, b.settings}}
	r.apply()
	return r
}

type deviceBadgeRenderer struct {
	b       *DeviceBadge
	objects []fyne.CanvasObject
}

func (r *deviceBadgeRenderer) Destroy() {}

func (r *deviceBadgeRenderer) MinSize() fyne.Size {
	return fyne.NewSize(badgeWidth, badgeHeight)
}

func (r *deviceBadgeRenderer) Layout(size fyne.Size) {
	r.b.glow.Resize(size)
	r.b.glow.Move(fyne.NewPos(0, 0))

	inset := float32(2)
	r.b.bg.Resize(fyne.NewSize(size.Width-2*inset, size.Height-2*inset))
	r.b.bg.Move(fyne.NewPos(inset, inset))

	led := float32(16)
	r.b.led.Resize(fyne.NewSize(led, led))
	r.b.led.Move(fyne.NewPos(8, (size.Height-led)/2))

	// Settings gear on the right.
	gear := float32(28)
	r.b.settings.Resize(fyne.NewSize(gear, gear))
	r.b.settings.Move(fyne.NewPos(size.Width-gear-6, (size.Height-gear)/2))

	textX := 8 + led + 8
	nmH := r.b.nm.MinSize().Height
	tgH := r.b.tg.MinSize().Height
	total := nmH + tgH - 2
	top := (size.Height - total) / 2
	r.b.nm.Move(fyne.NewPos(textX, top))
	r.b.tg.Move(fyne.NewPos(textX, top+nmH-2))
}

func (r *deviceBadgeRenderer) apply() {
	b := r.b
	switch b.state {
	case DeviceOnline:
		b.led.SetColor(b.accent)
		b.led.SetLit(true)
		b.nm.Color = lightenColor(b.accent, 0.45)
		b.glow.FillColor = withAlpha(b.accent, 0x40)
	case DeviceSearching:
		b.led.SetColor(badgeAmber)
		b.led.SetLit(true)
		b.nm.Color = lightenColor(badgeAmber, 0.25)
		b.glow.FillColor = withAlpha(badgeAmber, 0x1E)
	default: // DeviceOffline
		b.led.SetColor(badgeRed)
		b.led.SetLit(false)
		b.nm.Color = badgeNameDim
		b.glow.FillColor = withAlpha(badgeRed, 0x12)
	}
	b.nm.Text = b.name
	b.tg.Text = b.tag
}

func (r *deviceBadgeRenderer) Refresh() {
	r.apply()
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *deviceBadgeRenderer) Objects() []fyne.CanvasObject { return r.objects }
