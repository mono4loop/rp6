package p6

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

// Default MIDI channels, matching the P-6's factory settings. These can be
// changed on the device under MENU; override them via Config if you have.
const (
	DefaultSamplerChannel  = 11 // sample pads [1]-[6], banks A-H
	DefaultGranularChannel = 4  // GRANULAR pad
	DefaultAutoChannel     = 15 // currently-selected pad + control change
	DefaultProgramChannel  = 16 // program change (pattern select)

	// DefaultVelocity is the note-on velocity used when triggering a pad.
	DefaultVelocity = 100
)

// Config holds the MIDI channel assignments and default velocity used when
// talking to a P-6.
type Config struct {
	SamplerChannel  int
	GranularChannel int
	AutoChannel     int
	ProgramChannel  int
	Velocity        uint8
}

// DefaultConfig returns a Config populated with the P-6 factory defaults.
func DefaultConfig() Config {
	return Config{
		SamplerChannel:  DefaultSamplerChannel,
		GranularChannel: DefaultGranularChannel,
		AutoChannel:     DefaultAutoChannel,
		ProgramChannel:  DefaultProgramChannel,
		Velocity:        DefaultVelocity,
	}
}

// Device is a connection to a P-6 that sends MIDI messages. It is safe for
// concurrent use.
type Device struct {
	mu   sync.Mutex
	w    io.Writer
	r    io.Reader // non-nil when the node was opened for reading (see Listen)
	c    io.Closer
	cfg  Config
	path string
}

// ErrNotFound is returned by Discover when no P-6 is present.
var ErrNotFound = errors.New("p6: no P-6 found (is it connected via USB and powered on?)")

// ErrBusy is returned by Open/OpenPath when the P-6's MIDI node is held open by
// another program (the raw MIDI node is exclusive — e.g. amidi or a second
// instance). errors.Is(err, ErrBusy) reports it.
var ErrBusy = errors.New("p6: P-6 MIDI port is busy (another program has it open — close amidi or the other instance)")

// ErrPermission is returned by Open/OpenPath when the MIDI node exists but can't
// be opened for lack of permission (typically not being in the audio group, or
// a udev rule). errors.Is(err, ErrPermission) reports it.
var ErrPermission = errors.New("p6: permission denied opening the P-6 MIDI port (add your user to the 'audio' group or check udev rules)")

// Discover, Open and OpenPath are platform-specific: ALSA rawmidi on the desktop
// (device_alsa.go) and the Web MIDI API in the browser (device_js.go).

// New wraps an arbitrary writer as a Device. It is primarily useful for tests
// and for platform backends (ALSA/Web MIDI); most callers should use Open or
// OpenPath.
func New(w io.Writer, cfg Config) *Device {
	return &Device{w: w, cfg: cfg}
}

// Path returns the raw MIDI device node backing this Device, or "" if the
// Device was created from a custom writer.
func (d *Device) Path() string { return d.path }

// Config returns the Device's channel/velocity configuration.
func (d *Device) Config() Config { return d.cfg }

func (d *Device) send(b []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.w.Write(b); err != nil {
		return fmt.Errorf("p6: writing MIDI: %w", err)
	}
	return nil
}

// TriggerPad triggers the pad at (bank, pad) by sending a note-on on the
// Sampler channel. bank is 0-based (0=A..7=H); pad is 1-based (1..6).
//
// The P-6 ignores note-off, so a pad's own GATE/one-shot/loop setting governs
// how long it sounds; there is no need to send a matching note-off.
func (d *Device) TriggerPad(bank, pad int) error {
	note, err := NoteFor(bank, pad)
	if err != nil {
		return err
	}
	return d.send(noteOnBytes(d.cfg.SamplerChannel, note, d.cfg.Velocity))
}

// TriggerPadVelocity is like TriggerPad but with an explicit velocity (0..127).
func (d *Device) TriggerPadVelocity(bank, pad int, velocity uint8) error {
	note, err := NoteFor(bank, pad)
	if err != nil {
		return err
	}
	return d.send(noteOnBytes(d.cfg.SamplerChannel, note, velocity))
}

// NoteOn sends a raw note-on message on the given 1-based channel.
func (d *Device) NoteOn(channel int, note, velocity uint8) error {
	return d.send(noteOnBytes(channel, note, velocity))
}

// ControlChange sends a control-change message on the given 1-based channel.
func (d *Device) ControlChange(channel int, cc, value uint8) error {
	return d.send(controlChangeBytes(channel, cc, value))
}

// ProgramChange selects a pattern (0..63) by sending a program-change message
// on the configured Program Change channel. The P-6 only acts on it if "Rx
// Program Change" is enabled on the device.
func (d *Device) ProgramChange(program uint8) error {
	return d.send(programChangeBytes(d.cfg.ProgramChannel, program))
}

// Start sends a MIDI Start (transport play from the top). The P-6 only follows
// it when MIDI Clock Sync (SYnC) is set to USB (or MIDI) and nothing is plugged
// into SYNC IN.
func (d *Device) Start() error { return d.send([]byte{msgStart}) }

// Continue sends a MIDI Continue. On the P-6 this behaves the same as Start.
func (d *Device) Continue() error { return d.send([]byte{msgContinue}) }

// Stop sends a MIDI Stop (transport stop). See Start for the sync requirement.
func (d *Device) Stop() error { return d.send([]byte{msgStop}) }

// Clock sends a single MIDI timing-clock pulse (24 per quarter note). Send
// these at a steady rate to drive the P-6's tempo when SYnC is set to USB.
func (d *Device) Clock() error { return d.send([]byte{msgClock}) }

// Close releases the underlying device, if any.
func (d *Device) Close() error {
	if d.c != nil {
		return d.c.Close()
	}
	return nil
}
