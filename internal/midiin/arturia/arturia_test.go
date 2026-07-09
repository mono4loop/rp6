package arturia

import (
	"bytes"
	"io"
	"testing"

	"github.com/mono4loop/rp6/internal/midiin"
	"github.com/stretchr/testify/assert"
)

type note struct {
	note uint8
	vel  uint8
}

// runBytes feeds raw MIDI bytes through a device and returns the PlayNote,
// TriggerPad and Transport calls it produced.
func runBytes(t *testing.T, data []byte) (notes []note, pads int, transports []bool) {
	t.Helper()
	d := &device{name: "Arturia KeyStep 37", path: "test", rc: io.NopCloser(bytes.NewReader(data))}
	h := midiin.Handlers{
		PlayNote:   func(n, v uint8) { notes = append(notes, note{n, v}) },
		TriggerPad: func(int, uint8) { pads++ },
		Transport:  func(playing bool) { transports = append(transports, playing) },
	}
	// Run returns the read error (io.EOF at end of buffer) — expected.
	assert.Error(t, d.Run(h))
	return notes, pads, transports
}

func TestNotesDriveKeyboardNotPads(t *testing.T) {
	// Middle C (60) vel 100 and a high note (84) vel 40 on channel 1 (0x90).
	notes, pads, transports := runBytes(t, []byte{0x90, 60, 100, 0x90, 84, 40})
	assert.Zero(t, pads, "keyboard notes must not trigger pads")
	assert.Empty(t, transports)
	if assert.Len(t, notes, 2) {
		assert.Equal(t, note{60, 100}, notes[0])
		assert.Equal(t, note{84, 40}, notes[1])
	}
}

func TestNoteOffIgnored(t *testing.T) {
	// Velocity-0 note-on and an explicit 0x80 note-off are both dropped.
	notes, _, _ := runBytes(t, []byte{0x90, 60, 0, 0x80, 60, 64})
	assert.Empty(t, notes)
}

func TestTransportStartStop(t *testing.T) {
	notes, _, transports := runBytes(t, []byte{midiStart, midiStop})
	assert.Empty(t, notes)
	assert.Equal(t, []bool{true, false}, transports)
}

func TestNilHandlersAreSafe(t *testing.T) {
	d := &device{path: "test", rc: io.NopCloser(bytes.NewReader([]byte{0x90, 60, 100, midiStart}))}
	assert.Error(t, d.Run(midiin.Handlers{})) // io.EOF, no panic
}
