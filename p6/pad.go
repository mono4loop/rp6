// Package p6 provides a small, dependency-free client for controlling a
// Roland P-6 (AIRA Compact sampler) over its class-compliant USB MIDI port.
//
// The P-6 exposes 48 sample pads arranged as 8 banks (A-H) of 6 pads each.
// Every pad has a fixed MIDI note number in the range 48-95 (C3-B6) on the
// "Sampler" MIDI channel (channel 11 by default), regardless of which bank is
// currently selected on the hardware. There is no MIDI message to change the
// hardware's selected bank, so callers address pads absolutely by (bank, pad).
package p6

import "fmt"

const (
	// NumBanks is the number of sample-pad banks (A-H).
	NumBanks = 8
	// PadsPerBank is the number of pads in each bank.
	PadsPerBank = 6
	// NumPads is the total number of addressable sample pads.
	NumPads = NumBanks * PadsPerBank

	firstPadNote = 48 // C3 -> bank A, pad 1
	lastPadNote  = 95 // B6 -> bank H, pad 6
)

// BankName returns the P-6 bank letter (A-H) for a 0-based bank index.
// It returns "?" for out-of-range indexes.
func BankName(bank int) string {
	if bank < 0 || bank >= NumBanks {
		return "?"
	}
	return string(rune('A' + bank))
}

// NoteFor returns the MIDI note number that triggers the given pad.
//
// bank is 0-based (0=A ... 7=H) and pad is 1-based (1..6). The returned note is
// in the range 48-95 and is sent on the Sampler MIDI channel to trigger a pad.
func NoteFor(bank, pad int) (uint8, error) {
	if bank < 0 || bank >= NumBanks {
		return 0, fmt.Errorf("p6: bank out of range [0,%d): %d", NumBanks, bank)
	}
	if pad < 1 || pad > PadsPerBank {
		return 0, fmt.Errorf("p6: pad out of range [1,%d]: %d", PadsPerBank, pad)
	}
	return uint8(firstPadNote + bank*PadsPerBank + (pad - 1)), nil
}

// PadForNote is the inverse of NoteFor: it maps a pad note number (48-95) back
// to a 0-based bank index and 1-based pad number.
func PadForNote(note uint8) (bank, pad int, err error) {
	if note < firstPadNote || note > lastPadNote {
		return 0, 0, fmt.Errorf("p6: note %d is not a pad note [%d,%d]", note, firstPadNote, lastPadNote)
	}
	off := int(note) - firstPadNote
	return off / PadsPerBank, off%PadsPerBank + 1, nil
}

// PadLabel returns a human-friendly label for a pad, e.g. "A1" or "E6".
func PadLabel(bank, pad int) string {
	return fmt.Sprintf("%s%d", BankName(bank), pad)
}
