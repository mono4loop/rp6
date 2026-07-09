package p6

// Controller is the set of operations rp6 needs from a "P-6" endpoint. The real
// hardware (*Device) implements it by writing MIDI to the USB rawmidi node; the
// sample-playing emulator (internal/emu) implements it by mixing WAV files to an
// audio output. Because both satisfy this interface, the app can swap one for
// the other transparently — use the emulator when the hardware isn't around.
//
// *Device satisfies Controller (see the compile-time assertion below), so
// existing callers that hold a *Device keep working unchanged.
type Controller interface {
	// TriggerPad fires the pad at (bank, pad); bank is 0-based (0=A..7=H), pad
	// is 1-based (1..6). On the hardware this is a Note On on the Sampler
	// channel; on the emulator it plays that pad's sample.
	TriggerPad(bank, pad int) error
	// TriggerPadVelocity is TriggerPad with an explicit velocity (0..127).
	TriggerPadVelocity(bank, pad int, velocity uint8) error

	// NoteOn sends a raw note-on on a 1-based channel.
	NoteOn(channel int, note, velocity uint8) error
	// PlayNote plays a chromatic note in "keyboard mode": on the hardware a
	// Note On on the Auto channel, pitching the physically-selected pad; on the
	// emulator it pitches the last-selected pad's sample by
	// (note - KeyboardCenterNote) semitones.
	PlayNote(note, velocity uint8) error
	// ControlChange sends a control-change on a 1-based channel.
	ControlChange(channel int, cc, value uint8) error
	// ProgramChange selects a pattern (0..63).
	ProgramChange(program uint8) error
	// AutoCC sends a control change on the Auto channel (selected pad).
	AutoCC(cc, value uint8) error
	// GranularCC sends a control change on the Granular channel.
	GranularCC(cc, value uint8) error

	// Start/Continue/Stop/Clock drive transport. The emulator has no internal
	// sequencer, so these are no-ops there (rp6 sequences host-side).
	Start() error
	Continue() error
	Stop() error
	Clock() error

	// Config returns the channel/velocity configuration.
	Config() Config
	// Path returns a human-readable identifier for the endpoint (the rawmidi
	// node for hardware, or the samples directory for the emulator).
	Path() string
	// Listen reads incoming MIDI and calls handler for each message, blocking
	// until closed. Endpoints without an input (e.g. the emulator) return
	// ErrNoInput immediately.
	Listen(handler func(Event)) error
	// Close releases the endpoint.
	Close() error
}

// Compile-time assertion that the hardware device satisfies Controller.
var _ Controller = (*Device)(nil)
