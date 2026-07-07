package components

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// ModifierButton is a button with a secondary "modifier" action: a plain click
// runs OnTap, a Ctrl+click runs OnAlt, and while hovered *with Ctrl held* it
// swaps its label for AltIcon to advertise the alternate action (e.g. copy or
// delete). Because the GLFW driver doesn't report modifiers on hover events,
// the "Ctrl held" state is fed in by the app via SetModifierActive.
//
// Its min size is reserved for the label so swapping to the icon never resizes
// the button or reflows the row.
type ModifierButton struct {
	widget.Button

	baseText   string
	altIcon    fyne.Resource
	onTap      func()
	onAlt      func()
	reserved   fyne.Size
	ctrl       bool // Ctrl captured at the last MouseDown (for the click action)
	hovered    bool
	modActive  bool // Ctrl currently held (fed by the app)
	showingAlt bool
}

// NewModifierButton returns a button showing text that runs onTap on a plain
// click and onAlt on a Ctrl+click, revealing altIcon while hovered with Ctrl.
func NewModifierButton(text string, altIcon fyne.Resource, onTap, onAlt func()) *ModifierButton {
	b := &ModifierButton{baseText: text, altIcon: altIcon, onTap: onTap, onAlt: onAlt}
	b.Text = text
	b.ExtendBaseWidget(b)
	b.reserved = b.Button.MinSize() // size in the label state; stays fixed
	return b
}

func (b *ModifierButton) MinSize() fyne.Size {
	s := b.reserved
	if s.Width < 42 {
		s.Width = 42
	}
	if s.Height < 37 {
		s.Height = 37
	}
	return s
}

// SetModifierActive tells the button whether Ctrl is currently held so it can
// reveal the alt icon while hovered.
func (b *ModifierButton) SetModifierActive(on bool) {
	if on == b.modActive {
		return
	}
	b.modActive = on
	b.refreshAlt()
}

func (b *ModifierButton) refreshAlt() {
	show := b.altIcon != nil && b.hovered && b.modActive
	if show == b.showingAlt {
		return
	}
	b.showingAlt = show
	if show {
		b.SetIcon(b.altIcon)
		b.SetText("")
	} else {
		b.SetIcon(nil)
		b.SetText(b.baseText)
	}
}

// --- desktop.Hoverable ---

func (b *ModifierButton) MouseIn(e *desktop.MouseEvent) {
	b.hovered = true
	b.refreshAlt()
	b.Button.MouseIn(e)
}

func (b *ModifierButton) MouseOut() {
	b.hovered = false
	b.refreshAlt()
	b.Button.MouseOut()
}

// --- desktop.Mouseable: capture the Ctrl modifier before Tapped fires ---

func (b *ModifierButton) MouseDown(e *desktop.MouseEvent) {
	b.ctrl = e.Modifier&fyne.KeyModifierControl != 0
}

func (b *ModifierButton) MouseUp(*desktop.MouseEvent) {}

func (b *ModifierButton) Tapped(e *fyne.PointEvent) {
	ctrl := b.ctrl
	b.ctrl = false
	b.Button.Tapped(e) // keep the press animation
	if ctrl && b.onAlt != nil {
		b.onAlt()
		return
	}
	if b.onTap != nil {
		b.onTap()
	}
}
