package macropad

import (
	"bytes"
	"io"
	"testing"

	"github.com/mono4loop/rp6/internal/midiin"
	"github.com/mono4loop/rp6/p6"
	"github.com/stretchr/testify/assert"
)

// runBytes feeds raw MIDI bytes through a device and returns the collected
// TriggerPad and Transport calls.
func runBytes(t *testing.T, data []byte) (pads []padHit, transports []bool) {
	t.Helper()
	d := &device{path: "test", rc: io.NopCloser(bytes.NewReader(data))}
	h := midiin.Handlers{
		TriggerPad: func(id int, vel uint8) { pads = append(pads, padHit{id, vel}) },
		Transport:  func(playing bool) { transports = append(transports, playing) },
	}
	// Run returns the read error (io.EOF at end of buffer) — expected.
	err := d.Run(h)
	assert.Error(t, err)
	return pads, transports
}

type padHit struct {
	id  int
	vel uint8
}

func TestTriggerPadFromNote(t *testing.T) {
	// Note On, note 48 (bank A pad 1) vel 100, and note 95 (bank H pad 6) vel 80.
	// Channel is irrelevant to the driver; use ch1 (0x90).
	pads, transports := runBytes(t, []byte{0x90, 48, 100, 0x90, 95, 80})
	assert.Empty(t, transports)
	if assert.Len(t, pads, 2) {
		assert.Equal(t, padHit{0, 100}, pads[0])             // A1 -> id 0
		assert.Equal(t, padHit{p6.NumPads - 1, 80}, pads[1]) // H6 -> id 47
	}
}

func TestNonPadNoteIgnored(t *testing.T) {
	// Note 36 is below the pad range (48..95); it must be dropped.
	pads, _ := runBytes(t, []byte{0x90, 36, 100})
	assert.Empty(t, pads)
}

func TestNoteOffIgnored(t *testing.T) {
	// Velocity 0 is a note-off in MIDI; the parser reports EventNoteOff, which
	// the driver ignores (no phantom trigger). A real 0x80 note-off likewise.
	pads, _ := runBytes(t, []byte{0x90, 48, 0, 0x80, 48, 64})
	assert.Empty(t, pads)
}

func TestTransportStartStop(t *testing.T) {
	// Realtime Start (0xFA) then Stop (0xFC).
	pads, transports := runBytes(t, []byte{midiStart, midiStop})
	assert.Empty(t, pads)
	assert.Equal(t, []bool{true, false}, transports)
}

func TestNilHandlersAreSafe(t *testing.T) {
	d := &device{path: "test", rc: io.NopCloser(bytes.NewReader([]byte{0x90, 48, 100, midiStart}))}
	assert.Error(t, d.Run(midiin.Handlers{})) // io.EOF, no panic
}
