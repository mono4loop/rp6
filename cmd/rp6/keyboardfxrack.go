package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/mono4loop/rp6/internal/audiofx"
	"github.com/mono4loop/rp6/internal/ui/components"
)

// keyboardFXRack controls the emulator's host-side melodic instrument chain.
// The knobs are macros over the DSP parameters; zero is dry/off except TONE,
// whose center (0) is neutral.
type keyboardFXRack struct {
	onChange func(audiofx.Settings)
	settings audiofx.Settings

	tone   *components.Knob
	comp   *components.Knob
	chorus *components.Knob
	delay  *components.Knob
	reverb *components.Knob
	obj    fyne.CanvasObject
}

func newKeyboardFXRack(settings audiofx.Settings, onChange func(audiofx.Settings)) *keyboardFXRack {
	r := &keyboardFXRack{settings: settings, onChange: onChange}
	r.tone = r.knob("TONE", -100, 100, int(settings.Tone*100), func(v int) {
		r.settings.Tone = float32(v) / 100
	})
	r.comp = r.knob("COMP", 0, 100, int(settings.Comp*100), func(v int) {
		r.settings.Comp = float32(v) / 100
	})
	r.chorus = r.knob("CHORUS", 0, 100, int(settings.Chorus*100), func(v int) {
		r.settings.Chorus = float32(v) / 100
	})
	r.delay = r.knob("DELAY", 0, 100, int(settings.Delay*100), func(v int) {
		r.settings.Delay = float32(v) / 100
	})
	r.reverb = r.knob("REVERB", 0, 100, int(settings.Reverb*100), func(v int) {
		r.settings.Reverb = float32(v) / 100
	})
	return r
}

func (r *keyboardFXRack) knob(label string, min, max, value int, update func(int)) *components.Knob {
	return components.NewKnob(components.KnobConfig{
		Label: label, Value: value, Min: min, Max: max, Step: 5,
		Width: 128, Accent: deviceEmuAccent,
		Format: func(v int) string {
			if min < 0 && v > 0 {
				return fmt.Sprintf("+%d", v)
			}
			return fmt.Sprintf("%d", v)
		},
		OnChange: func(v int) {
			update(v)
			if r.onChange != nil {
				r.onChange(r.settings)
			}
		},
	})
}

// Object returns the CanvasObject to place in a layout.
func (r *keyboardFXRack) Object() fyne.CanvasObject { return r.obj }

// Settings returns the current macro settings.
func (r *keyboardFXRack) Settings() audiofx.Settings { return r.settings }

// defaultObject builds the stock single-row effects rack.
func (r *keyboardFXRack) defaultObject() fyne.CanvasObject {
	return components.NewRackPanel(container.NewHBox(
		r.tone.Object(), r.comp.Object(), r.chorus.Object(), r.delay.Object(), r.reverb.Object()))
}
