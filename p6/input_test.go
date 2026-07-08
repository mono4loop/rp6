package p6

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// collect parses the whole byte slice and returns the emitted events.
func collect(t *testing.T, data []byte) []Event {
	t.Helper()
	var evs []Event
	err := parseMIDI(bufio.NewReader(bytes.NewReader(data)), func(e Event) { evs = append(evs, e) })
	// parseMIDI returns io.EOF at the end of the buffer, which is expected here.
	assert.Error(t, err)
	return evs
}

func TestParseNoteOnPad(t *testing.T) {
	// Note On ch11 (status 0x9A), note 48 (bank A pad 1), vel 100.
	evs := collect(t, []byte{0x9A, 48, 100})
	if assert.Len(t, evs, 1) {
		e := evs[0]
		assert.Equal(t, EventNoteOn, e.Type)
		assert.Equal(t, 11, e.Channel)
		assert.EqualValues(t, 48, e.Data1)
		assert.EqualValues(t, 100, e.Data2)
		bank, pad, err := PadForNote(e.Data1)
		assert.NoError(t, err)
		assert.Equal(t, 0, bank)
		assert.Equal(t, 1, pad)
	}
}

func TestParseNoteOnZeroVelIsNoteOff(t *testing.T) {
	evs := collect(t, []byte{0x9A, 60, 0})
	if assert.Len(t, evs, 1) {
		assert.Equal(t, EventNoteOff, evs[0].Type)
	}
}

func TestParseRunningStatus(t *testing.T) {
	// One status byte then two note pairs (running status).
	evs := collect(t, []byte{0x9A, 48, 100, 53, 90})
	if assert.Len(t, evs, 2) {
		assert.Equal(t, EventNoteOn, evs[0].Type)
		assert.EqualValues(t, 48, evs[0].Data1)
		assert.Equal(t, EventNoteOn, evs[1].Type)
		assert.EqualValues(t, 53, evs[1].Data1)
		assert.EqualValues(t, 90, evs[1].Data2)
	}
}

func TestParseRealtimeInterleaved(t *testing.T) {
	// A clock byte (0xF8) between the status and data must not corrupt the note.
	evs := collect(t, []byte{0x9A, 0xF8, 48, 100})
	// Expect: a realtime event and a note-on.
	var rt, note int
	for _, e := range evs {
		switch e.Type {
		case EventRealTime:
			rt++
			assert.EqualValues(t, 0xF8, e.Status)
		case EventNoteOn:
			note++
			assert.EqualValues(t, 48, e.Data1)
		}
	}
	assert.Equal(t, 1, rt)
	assert.Equal(t, 1, note)
}

func TestParseControlChangeAndSkips(t *testing.T) {
	// CC ch15 (0xBE) cc7 val64; then a sysex that must be skipped; then a PC.
	data := []byte{0xBE, 7, 64, 0xF0, 1, 2, 3, 0xF7, 0xCF, 5}
	evs := collect(t, data)
	var cc, pc int
	for _, e := range evs {
		switch e.Type {
		case EventControlChange:
			cc++
			assert.Equal(t, 15, e.Channel)
			assert.EqualValues(t, 7, e.Data1)
			assert.EqualValues(t, 64, e.Data2)
		case EventProgramChange:
			pc++
			assert.Equal(t, 16, e.Channel)
			assert.EqualValues(t, 5, e.Data1)
		}
	}
	assert.Equal(t, 1, cc)
	assert.Equal(t, 1, pc)
}

// TestParseRealtimeInSystemCommon checks a realtime byte interleaved within a
// system-common message's data bytes is still emitted (not swallowed as data).
// Song Position Pointer (0xF2) carries two data bytes; a clock (0xF8) sits
// between them (nb91).
func TestParseRealtimeInSystemCommon(t *testing.T) {
	data := []byte{0xF2, 0x10, 0xF8, 0x20, 0x9A, 48, 100}
	evs := collect(t, data)
	var rt, note int
	for _, e := range evs {
		switch e.Type {
		case EventRealTime:
			rt++
			assert.EqualValues(t, 0xF8, e.Status)
		case EventNoteOn:
			note++
			assert.EqualValues(t, 48, e.Data1)
			assert.EqualValues(t, 100, e.Data2)
		}
	}
	assert.Equal(t, 1, rt, "interleaved realtime must be emitted, not swallowed")
	assert.Equal(t, 1, note, "the trailing note must still parse")
}

// TestParseResyncOnNewStatus checks that when a new status byte arrives before a
// message's data bytes are complete (a truncated/corrupt message), the parser
// resynchronizes on the new status instead of consuming the status byte as data
// and emitting a corrupt event (nb91).
func TestParseResyncOnNewStatus(t *testing.T) {
	// Note On ch11, note 48, then a NEW status byte instead of the velocity.
	data := []byte{0x9A, 48, 0x9A, 53, 100}
	evs := collect(t, data)
	if assert.Len(t, evs, 1, "the truncated first note must be dropped, not emitted corrupt") {
		assert.Equal(t, EventNoteOn, evs[0].Type)
		assert.EqualValues(t, 53, evs[0].Data1)
		assert.EqualValues(t, 100, evs[0].Data2)
	}
}

// TestParseExplicitNoteOff covers a real 0x80 note-off status (distinct from a
// note-on with zero velocity).
func TestParseExplicitNoteOff(t *testing.T) {
	evs := collect(t, []byte{0x8A, 48, 64}) // note off ch11, note 48, vel 64
	if assert.Len(t, evs, 1) {
		assert.Equal(t, EventNoteOff, evs[0].Type)
		assert.Equal(t, 11, evs[0].Channel)
		assert.EqualValues(t, 48, evs[0].Data1)
		assert.EqualValues(t, 64, evs[0].Data2)
	}
}

// TestParseIgnoredChannelMessagesConsumeData ensures messages the parser emits
// no Event for (poly aftertouch 0xA0 = 2 bytes, channel aftertouch 0xD0 = 1
// byte, pitch bend 0xE0 = 2 bytes) still consume the right number of data bytes,
// so a following message parses cleanly rather than being misaligned.
func TestParseIgnoredChannelMessagesConsumeData(t *testing.T) {
	cases := [][]byte{
		{0xAA, 48, 100, 0x9A, 60, 100}, // poly aftertouch then note-on
		{0xDA, 100, 0x9A, 60, 100},     // channel aftertouch (1 byte) then note-on
		{0xEA, 0, 64, 0x9A, 60, 100},   // pitch bend then note-on
	}
	for _, data := range cases {
		evs := collect(t, data)
		if assert.Len(t, evs, 1, "%#v", data) {
			assert.Equal(t, EventNoteOn, evs[0].Type)
			assert.EqualValues(t, 60, evs[0].Data1)
			assert.EqualValues(t, 100, evs[0].Data2)
		}
	}
}
