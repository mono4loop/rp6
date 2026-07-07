package androidusb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecodeNoteOn(t *testing.T) {
	// CIN 0x9 (note on), cable 0: packet 09 90 3C 64 -> 90 3C 64.
	got := DecodeUSBMIDI([]byte{0x09, 0x90, 0x3C, 0x64})
	assert.Equal(t, []byte{0x90, 0x3C, 0x64}, got)
}

func TestDecodeCableIgnored(t *testing.T) {
	// Cable number (high nibble of byte 0) must not affect decoding.
	got := DecodeUSBMIDI([]byte{0x39, 0x90, 0x3C, 0x64}) // cable 3, CIN 9
	assert.Equal(t, []byte{0x90, 0x3C, 0x64}, got)
}

func TestDecodeRealtime(t *testing.T) {
	// CIN 0xF single byte: clock (F8), start (FA), stop (FC).
	got := DecodeUSBMIDI([]byte{
		0x0F, 0xF8, 0, 0,
		0x0F, 0xFA, 0, 0,
		0x0F, 0xFC, 0, 0,
	})
	assert.Equal(t, []byte{0xF8, 0xFA, 0xFC}, got)
}

func TestDecodeProgramChange(t *testing.T) {
	// CIN 0xC program change: 2 data bytes.
	got := DecodeUSBMIDI([]byte{0x0C, 0xC0, 0x05, 0x00})
	assert.Equal(t, []byte{0xC0, 0x05}, got)
}

func TestDecodeMultiplePackets(t *testing.T) {
	got := DecodeUSBMIDI([]byte{
		0x09, 0x99, 48, 100, // note on ch10 note48 vel100
		0x08, 0x89, 48, 0, // note off
		0x0F, 0xF8, 0, 0, // clock
	})
	assert.Equal(t, []byte{0x99, 48, 100, 0x89, 48, 0, 0xF8}, got)
}

func TestDecodeTrailingPartialIgnored(t *testing.T) {
	// 5 bytes: one full packet + a 1-byte fragment that must be dropped.
	got := DecodeUSBMIDI([]byte{0x09, 0x90, 0x3C, 0x64, 0x0F})
	assert.Equal(t, []byte{0x90, 0x3C, 0x64}, got)
}

func TestDecodeReservedCINDropped(t *testing.T) {
	// CIN 0x0 and 0x1 are reserved/misc — no MIDI bytes emitted.
	got := DecodeUSBMIDI([]byte{0x00, 0x11, 0x22, 0x33, 0x01, 0x44, 0x55, 0x66})
	assert.Empty(t, got)
}

func TestDecodeEmpty(t *testing.T) {
	assert.Empty(t, DecodeUSBMIDI(nil))
	assert.Empty(t, DecodeUSBMIDI([]byte{}))
}

func TestCINLengthTable(t *testing.T) {
	ones := []byte{0x5, 0xF}
	twos := []byte{0x2, 0x6, 0xC, 0xD}
	threes := []byte{0x3, 0x4, 0x7, 0x8, 0x9, 0xA, 0xB, 0xE}
	zeros := []byte{0x0, 0x1}
	for _, c := range ones {
		assert.Equal(t, 1, cinLength(c), "CIN %#x", c)
	}
	for _, c := range twos {
		assert.Equal(t, 2, cinLength(c), "CIN %#x", c)
	}
	for _, c := range threes {
		assert.Equal(t, 3, cinLength(c), "CIN %#x", c)
	}
	for _, c := range zeros {
		assert.Equal(t, 0, cinLength(c), "CIN %#x", c)
	}
}

func TestEncodeNoteOn(t *testing.T) {
	// Note On ch10 (0x99) note 48 vel 100 -> CIN 0x9 packet.
	got := EncodeUSBMIDI([]byte{0x99, 48, 100})
	assert.Equal(t, []byte{0x09, 0x99, 48, 100}, got)
}

func TestEncodeProgramChangeAndCC(t *testing.T) {
	// Program change (2 bytes, CIN 0xC) + control change (3 bytes, CIN 0xB).
	got := EncodeUSBMIDI([]byte{0xCF, 0x05, 0xBE, 0x07, 0x00})
	assert.Equal(t, []byte{0x0C, 0xCF, 0x05, 0x00, 0x0B, 0xBE, 0x07, 0x00}, got)
}

func TestEncodeRealtime(t *testing.T) {
	// Start / clock / stop -> CIN 0xF single-byte packets.
	got := EncodeUSBMIDI([]byte{0xFA, 0xF8, 0xFC})
	assert.Equal(t, []byte{0x0F, 0xFA, 0, 0, 0x0F, 0xF8, 0, 0, 0x0F, 0xFC, 0, 0}, got)
}

func TestEncodeDropsIncompleteAndStray(t *testing.T) {
	assert.Empty(t, EncodeUSBMIDI([]byte{0x99, 48}))   // incomplete note on
	assert.Empty(t, EncodeUSBMIDI([]byte{0x40, 0x50})) // stray data, no status
	assert.Empty(t, EncodeUSBMIDI([]byte{0xF0, 0x7E})) // SysEx: unsupported
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	// Encoding then decoding the messages rp6 actually sends is the identity.
	midi := []byte{
		0x9A, 48, 100, // note on
		0xBE, 0x5B, 0x40, // control change
		0xCF, 0x03, // program change
		0xF8, // clock
		0xFC, // stop
	}
	assert.Equal(t, midi, DecodeUSBMIDI(EncodeUSBMIDI(midi)))
}
