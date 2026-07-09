// Package arturia is the rp6 MIDI-input driver for Arturia USB keyboards — the
// KeyStep 37 and the MicroLab. Unlike the MacroPad (which triggers pads), these
// are melodic keyboards, so this driver routes their keys to rp6's on-screen
// keyboard: each Note On plays the currently-selected sample pitched (via
// PlayNote), and any transport realtime messages drive Play/Stop.
//
// The keyboards reach rp6 differently per platform, so discovery/open live in
// build-tagged files: an ALSA rawmidi node on the desktop (arturia_alsa.go) and
// the Android MIDI bridge on a phone (arturia_android.go). The MIDI-to-Handlers
// mapping (handle) and the read loop (device.Run) are shared here.
package arturia

import (
	"io"

	"github.com/mono4loop/rp6/internal/midiin"
	"github.com/mono4loop/rp6/p6"
)

// System realtime status bytes used for transport (see p6/midi.go).
const (
	midiStart = 0xFA
	midiStop  = 0xFC
)

// device is an opened Arturia keyboard, reading MIDI from rc (an ALSA rawmidi
// node on the desktop, or an Android MIDI bridge reader on a phone). name is the
// specific model, set by the driver that opened it.
type device struct {
	name string
	path string
	rc   io.ReadCloser
}

func (d *device) Name() string { return d.name }
func (d *device) Path() string { return d.path }

// Run parses incoming MIDI (reusing p6's tested parser) until the node is
// closed, translating each message via handle.
func (d *device) Run(h midiin.Handlers) error {
	return p6.ParseMIDI(d.rc, func(ev p6.Event) { handle(h, ev) })
}

func (d *device) Close() error { return d.rc.Close() }

// handle maps one incoming MIDI event to a Handlers call: a Note On plays a
// chromatic note on rp6's keyboard, and realtime Start/Stop drive transport.
// Note-offs are ignored (the pad/sample governs its own tail), matching how the
// on-screen keyboard behaves. It is package-level (not a method) so it can be
// unit-tested without a device node.
func handle(h midiin.Handlers, ev p6.Event) {
	switch ev.Type {
	case p6.EventNoteOn:
		if h.PlayNote != nil {
			// The parser maps NoteOn/velocity-0 to EventNoteOff, so a NoteOn
			// here always carries a real velocity.
			h.PlayNote(ev.Data1, ev.Data2)
		}
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
