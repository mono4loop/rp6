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

// Octave range for the OCT knob (semitone shift = 12 × octave): four octaves
// down and four up from the base.
const (
	keyboardOctMin = -4
	keyboardOctMax = 4
)

// keyboardBaseNote is the MIDI note of the on-screen keyboard's leftmost key at
// OCT 0 — C3 (48). It's chosen to match the default octave of common external
// MIDI keyboards (the Arturia MicroLab/KeyStep send C3 for their lowest key at
// their default octave), so plugging one in and playing reads as OCT 0. Unity
// pitch for the P-6 keyboard mode (p6.KeyboardCenterNote = C4 = 60) is an octave
// up from here (key 12 at OCT 0, or key 0 at OCT +1).
const keyboardBaseNote = p6.KeyboardCenterNote - 12

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
		Label: "OCT", Value: 0, Min: keyboardOctMin, Max: keyboardOctMax, Step: 1,
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
		MinWhite: 12, // ~1.7 octaves minimum; grows with width to fit a 25/37-key controller
		WhiteW:   32, // narrower keys than the default so more of the range shows
		Accent:   deviceHwAccent,
		OnNote:   k.play,
		Label:    k.keyLabel,
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

// note returns the MIDI note for key index i at the current octave. Key 0 at
// OCT 0 is keyboardBaseNote (C3); each octave step shifts by 12 semitones.
// Clamped to 0..127.
func (k *keyboardRack) note(i int) int {
	n := keyboardBaseNote + i + 12*k.oct.Value()
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

// reflectNote echoes an incoming external MIDI note on the on-screen keyboard.
// The on-screen keyboard is a window onto the note range: a note plays at its
// true position within the visible keys, so playing higher on the controller
// lights higher on-screen keys (the full 25/37-key range maps across the shown
// keys). The OCT knob's octave only shifts the window when a note would fall
// off-range — so the window scrolls to keep what you play visible, and the knob
// tracks the controller's octave buttons when they push notes past the edge.
// Arturia keyboards transpose their note numbers (no octave message), so this is
// all inferred from the notes. The note itself already sounds at its true pitch
// (played as-is); this only positions the window and lights the matching key.
// Call on the UI thread.
func (k *keyboardRack) reflectNote(note uint8) {
	base := func(oct int) int { return keyboardBaseNote + 12*oct }
	oct := k.oct.Value()
	vis := k.piano.VisibleKeys()
	idx := int(note) - base(oct)
	// Scroll the window (by whole octaves) only far enough to bring the note
	// back into the visible keys — clamped to the knob's range.
	for idx < 0 && oct > keyboardOctMin {
		oct--
		idx += 12
	}
	for idx >= vis && oct < keyboardOctMax {
		oct++
		idx -= 12
	}
	if oct != k.oct.Value() {
		k.oct.SetValue(oct) // reflect the shifted octave on the knob (and relabel the keys)
		idx = int(note) - base(oct)
	}
	// FlashKey ignores out-of-range/hidden indices (e.g. when the octave clamped).
	k.piano.FlashKey(idx)
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
