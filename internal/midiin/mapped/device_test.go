package mapped

import (
	"bytes"
	"io"
	"testing"

	"github.com/mono4loop/rp6/internal/midiin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// run parses mapText, feeds raw MIDI bytes through a mapped device, and returns
// the intents forwarded to Dispatch.
func run(t *testing.T, mapText string, data []byte) []midiin.Intent {
	t.Helper()
	m, err := Parse(mapText)
	require.NoError(t, err)
	var got []midiin.Intent
	d := &Device{path: "test", rc: io.NopCloser(bytes.NewReader(data)), m: m}
	h := midiin.Handlers{Dispatch: func(in midiin.Intent) { got = append(got, in) }}
	assert.Error(t, d.Run(h)) // io.EOF at end of buffer
	return got
}

const padMap = `device "T" {
match "t"
channel 1
note 36..83 -> pad.trigger offset=36
}`

func TestPadTrigger(t *testing.T) {
	// note 36 (id 0) vel 100, note 51 (id 15) vel 64.
	got := run(t, padMap, []byte{0x90, 36, 100, 0x90, 51, 64})
	require.Len(t, got, 2)
	assert.Equal(t, "pad.trigger", got[0].Name)
	assert.Equal(t, 0, got[0].Pad)
	assert.Equal(t, uint8(100), got[0].Velocity)
	assert.Equal(t, 15, got[1].Pad)
}

func TestPadChannelFilterAndNoteOff(t *testing.T) {
	// ch2 note (0x91) is filtered by `channel 1`; note-off (vel 0) is ignored.
	got := run(t, padMap, []byte{0x91, 36, 100, 0x90, 40, 0})
	assert.Empty(t, got)
}

func TestPadOutOfRange(t *testing.T) {
	got := run(t, padMap, []byte{0x90, 35, 100, 0x90, 84, 100}) // below/above 36..83
	assert.Empty(t, got)
}

func TestTransportCCWhen(t *testing.T) {
	m := `device "T" {
match "t"
channel 1
cc 97 when value=127 -> transport.play
cc 98 when value=127 -> transport.stop
}`
	// press play (127) -> play; release (0) -> nothing; press stop -> stop.
	got := run(t, m, []byte{0xB0, 97, 127, 0xB0, 97, 0, 0xB0, 98, 127})
	require.Len(t, got, 2)
	assert.Equal(t, "transport.play", got[0].Name)
	assert.Equal(t, "transport.stop", got[1].Name)
}

func TestAbsValue(t *testing.T) {
	m := `device "T" {
match "t"
channel 1
cc 9 abs -> tempo.set
}`
	got := run(t, m, []byte{0xB0, 9, 0, 0xB0, 9, 127})
	require.Len(t, got, 2)
	assert.InDelta(t, 0.0, got[0].Value, 1e-9)
	assert.InDelta(t, 1.0, got[1].Value, 1e-9)
}

func TestAbsScale(t *testing.T) {
	m := `device "T" {
match "t"
cc 20 abs scale=10..110 -> delay.set
}`
	// below lo clamps to 0, at hi clamps to 1, midpoint ~0.5.
	got := run(t, m, []byte{0xB0, 20, 10, 0xB0, 20, 60, 0xB0, 20, 110})
	require.Len(t, got, 3)
	assert.InDelta(t, 0.0, got[0].Value, 1e-9)
	assert.InDelta(t, 0.5, got[1].Value, 1e-9)
	assert.InDelta(t, 1.0, got[2].Value, 1e-9)
}

func TestRelDelta(t *testing.T) {
	m := `device "T" {
match "t"
cc 16 rel=twoscomp -> tempo.delta
}`
	// +1, -1, 0 (zero delta is dropped).
	got := run(t, m, []byte{0xB0, 16, 1, 0xB0, 16, 127, 0xB0, 16, 0})
	require.Len(t, got, 2)
	assert.Equal(t, 1, got[0].Delta)
	assert.Equal(t, -1, got[1].Delta)
}

func TestRealtime(t *testing.T) {
	m := `device "T" {
match "t"
realtime start -> transport.play
realtime stop -> transport.stop
}`
	got := run(t, m, []byte{0xFA, 0xFC})
	require.Len(t, got, 2)
	assert.Equal(t, "transport.play", got[0].Name)
	assert.Equal(t, "transport.stop", got[1].Name)
}

func TestInputBankRel(t *testing.T) {
	m := `device "T" {
match "t"
channel 1
note 36..51 -> pad.trigger.rel offset=36
cc 50 when value=127 -> input.bank.next
}`
	// note 36 -> id 0; bump bank; note 36 -> id 16 (bank 1 * 16 + 0).
	got := run(t, m, []byte{0x90, 36, 100, 0xB0, 50, 127, 0x90, 36, 100})
	require.Len(t, got, 2) // the bank bump is interpreter-internal, not dispatched
	assert.Equal(t, 0, got[0].Pad)
	assert.Equal(t, 16, got[1].Pad)
}

func TestNilDispatchSafe(t *testing.T) {
	m, err := Parse(padMap)
	require.NoError(t, err)
	d := &Device{path: "t", rc: io.NopCloser(bytes.NewReader([]byte{0x90, 36, 100})), m: m}
	assert.Error(t, d.Run(midiin.Handlers{})) // io.EOF, no panic
}

func TestNotePlay(t *testing.T) {
	// A melodic keyboard map (Arturia-style): every note -> note.play, carrying
	// the raw note + velocity; a note-off (velocity 0) is ignored.
	m := `device "T" {
match "t"
note 0..127 -> note.play
}`
	got := run(t, m, []byte{0x90, 60, 100, 0x90, 72, 0})
	require.Len(t, got, 1)
	assert.Equal(t, "note.play", got[0].Name)
	assert.Equal(t, uint8(60), got[0].Note)
	assert.Equal(t, uint8(100), got[0].Velocity)
}
