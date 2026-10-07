package components

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/stretchr/testify/assert"
)

// Captions must be sized as well as moved. Fyne draws a zero-size
// canvas.Text anyway, but tools that look for text on screen, such as the
// autopilot rp6 scripts demos with, skip objects with no size.
func TestCaptionsAreSized(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme()) // the knob's LCD needs bold monospace
	amber := color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}

	knob := NewKnob(KnobConfig{Label: "TEMPO", Value: 120, Min: 40, Max: 300, Step: 5})
	compact := NewKnob(KnobConfig{Label: "OCT", Value: 4, Min: 0, Max: 8, Step: 1, Compact: true})
	toggle := NewRackToggle("LOOP", amber, nil)
	badge := NewDeviceBadge("P-6", "USB MIDI", amber)

	for name, tc := range map[string]struct {
		obj   fyne.CanvasObject
		texts func() []*canvas.Text
	}{
		"knob":         {knob, func() []*canvas.Text { return []*canvas.Text{knob.label, knob.valTxt} }},
		"compact knob": {compact, func() []*canvas.Text { return []*canvas.Text{compact.label, compact.valTxt} }},
		"rack toggle":  {toggle, func() []*canvas.Text { return []*canvas.Text{toggle.txt} }},
		"device badge": {badge, func() []*canvas.Text { return []*canvas.Text{badge.nm, badge.tg} }},
	} {
		t.Run(name, func(t *testing.T) {
			w := test.NewWindow(tc.obj)
			defer w.Close()
			for _, txt := range tc.texts() {
				assert.Equal(t, txt.MinSize(), txt.Size(), "%q is laid out at its text size", txt.Text)
			}
		})
	}
}
