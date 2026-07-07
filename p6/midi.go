package p6

// MIDI message helpers. Channels are expressed 1-based (1..16) at the API
// boundary to match the numbers shown on the P-6, and encoded 0-based in the
// status byte.

const (
	statusNoteOn        = 0x90
	statusControlChange = 0xB0
	statusProgramChange = 0xC0

	// System realtime messages (single byte).
	msgClock    = 0xF8
	msgStart    = 0xFA
	msgContinue = 0xFB
	msgStop     = 0xFC
)

// clampChannel converts a 1-based MIDI channel into the 0-based nibble used in
// a channel-voice status byte, clamping to the valid 1..16 range.
func clampChannel(ch int) byte {
	if ch < 1 {
		ch = 1
	}
	if ch > 16 {
		ch = 16
	}
	return byte(ch - 1)
}

func noteOnBytes(ch int, note, vel uint8) []byte {
	return []byte{statusNoteOn | clampChannel(ch), note & 0x7f, vel & 0x7f}
}

func controlChangeBytes(ch int, cc, val uint8) []byte {
	return []byte{statusControlChange | clampChannel(ch), cc & 0x7f, val & 0x7f}
}

func programChangeBytes(ch int, program uint8) []byte {
	return []byte{statusProgramChange | clampChannel(ch), program & 0x7f}
}
