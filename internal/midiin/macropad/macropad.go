// Package macropad is the rp6 MIDI-input driver for the Adafruit MacroPad
// RP2040 (the "DigiKey macropad", product 5100) running the rp6 CircuitPython
// firmware from docs/hardware/macropad.
//
// The firmware does the device-specific work on the pad itself: its rotary
// encoder pages through the P-6 banks and it sends absolute P-6-style pad notes
// (48..95 — the same numbers the P-6 uses, note = 48 + bank*6 + (pad-1)), so
// this driver only has to translate incoming MIDI into midiin.Handlers calls:
//
//   - Note On (48..95)  -> TriggerPad(padID, velocity)
//   - Realtime Start/Stop (encoder press) -> Transport(true/false)
//
// Because it triggers pads absolutely, rp6 routes each hit through its normal
// fire path, so the MacroPad drives the real P-6 or the emulator identically.
//
// The MacroPad reaches rp6 differently per platform, so discovery/open live in
// build-tagged files: an ALSA rawmidi node on the desktop (macropad_alsa.go),
// and the Android MIDI bridge on a phone (macropad_android.go). The MIDI-to-
// Handlers mapping (handle) and the read loop (device.Run) are shared here.
package macropad

import (
	"io"

	"github.com/mono4loop/rp6/internal/midiin"
	"github.com/mono4loop/rp6/p6"
)

// Name is the model name reported to the app.
const Name = "Adafruit MacroPad RP2040"

// System realtime status bytes used for transport (see p6/midi.go).
const (
	midiStart = 0xFA
	midiStop  = 0xFC
)

// device is an opened MacroPad, reading MIDI from rc (an ALSA rawmidi node on
// the desktop, or an Android MIDI bridge reader on a phone).
type device struct {
	path string
	rc   io.ReadCloser
}

func (d *device) Name() string { return Name }
func (d *device) Path() string { return d.path }

// Run parses incoming MIDI (reusing p6's tested parser) until the node is
// closed, translating each message via handle.
func (d *device) Run(h midiin.Handlers) error {
	return p6.ParseMIDI(d.rc, func(ev p6.Event) { handle(h, ev) })
}

func (d *device) Close() error { return d.rc.Close() }

// handle maps one incoming MIDI event to a Handlers call. It is package-level
// (not a method) so it can be unit-tested without a device node.
func handle(h midiin.Handlers, ev p6.Event) {
	switch ev.Type {
	case p6.EventNoteOn:
		if h.TriggerPad == nil {
			return
		}
		bank, pad, err := p6.PadForNote(ev.Data1)
		if err != nil {
			return // not a pad note (48..95)
		}
		// The parser maps NoteOn/velocity-0 to EventNoteOff, so a NoteOn here
		// always carries a real velocity (mechanical keys send a fixed value).
		h.TriggerPad(bank*p6.PadsPerBank+(pad-1), ev.Data2)
	case p6.EventRealTime:
		if h.Transport == nil {
			return
		}
		switch ev.Status {
		case midiStart:
			h.Transport(true)
		case midiStop:
			h.Transport(false)
		}
	}
}
