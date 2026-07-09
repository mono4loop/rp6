package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"

	"github.com/mono4loop/rp6/internal/ui/components"
	"github.com/mono4loop/rp6/p6"
)

// keyboardTallHeight is the keyboard's height in the console (full-screen)
// layout — about twice the compact windowed height, all of it going to the keys
// (the OCT knob keeps its size on top).
const keyboardTallHeight = 96

// keyboardRack is a P-6-style chromatic keyboard. Its piano keys play the
// currently-selected sample pitched (a Note On on the Auto channel on hardware,
// or pitched playback on the emulator — see ui.playNote), and an OCT knob shifts
// the octave like the P-6's OCT-/OCT+ keys.
type keyboardRack struct {
	onNote func(note uint8) // plays a MIDI note (wired to ui.playNote)
	piano  *components.PianoKeyboard
	oct    *components.Knob
	obj    fyne.CanvasObject
}

func newKeyboardRack(onNote func(note uint8)) *keyboardRack {
	k := &keyboardRack{onNote: onNote}

	// The octave knob is created before the piano because the piano's key labels
	// (keyLabel) read the current octave from it during construction.
	k.oct = components.NewKnob(components.KnobConfig{
		Label: "OCT", Value: 0, Min: -3, Max: 3, Step: 1,
		Compact: true, Width: 132,
		Accent: deviceHwAccent,
		Format: func(v int) string {
			if v > 0 {
				return fmt.Sprintf("+%d", v)
			}
			return fmt.Sprintf("%d", v)
		},
		OnChange: func(int) {
			if k.piano != nil {
				k.piano.Refresh() // relabel keys for the new octave
			}
		},
	})

	k.piano = components.NewPianoKeyboard(components.PianoConfig{
		Accent: deviceHwAccent,
		OnNote: k.play,
		Label:  k.keyLabel,
	})
	return k
}

// Object returns the CanvasObject to place in a layout.
func (k *keyboardRack) Object() fyne.CanvasObject { return k.obj }

// setTall makes the keys taller (in the console layout) or resets them to the
// compact windowed height. Only the keys grow; the OCT knob keeps its size.
func (k *keyboardRack) setTall(tall bool) {
	if tall {
		k.piano.SetMinHeight(keyboardTallHeight)
	} else {
		k.piano.SetMinHeight(0) // default compact height
	}
}

// note returns the MIDI note for key index i at the current octave. Key 0 is the
// C that plays the selected sample at its original pitch (p6.KeyboardCenterNote)
// at octave 0; each octave step shifts by 12 semitones. Clamped to 0..127.
func (k *keyboardRack) note(i int) int {
	n := p6.KeyboardCenterNote + i + 12*k.oct.Value()
	if n < 0 {
		n = 0
	}
	if n > 127 {
		n = 127
	}
	return n
}

func (k *keyboardRack) play(i int) {
	if k.onNote != nil {
		k.onNote(uint8(k.note(i)))
	}
}

func (k *keyboardRack) keyLabel(i int) string {
	return noteName(uint8(k.note(i)))
}

// defaultObject builds the rack's stock Go composition — used only when the
// layout file has no `rack keys` block (see ui.composeRack). The OCT knob sits
// above the keys, left-aligned.
func (k *keyboardRack) defaultObject() fyne.CanvasObject {
	top := container.NewHBox(k.oct.Object(), layout.NewSpacer())
	return components.NewRackPanel(container.NewBorder(top, nil, nil, nil, k.piano))
}
