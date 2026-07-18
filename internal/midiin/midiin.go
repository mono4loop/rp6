// Package midiin is a small, pluggable framework for MIDI *input* controllers:
// external hardware (a pad controller, a grid, a keyboard, …) that drives rp6's
// pads, on-screen keyboard and transport host-side. It is the mirror image of
// the p6 package (which sends MIDI *out* to the P-6): here MIDI comes *in* from a
// controller and is translated into rp6 actions.
//
// Most controllers are described by a data-driven .midimap file and served by
// the generic interpreter in internal/midiin/mapped (see
// docs/architecture/midimaps.md) — the app parses those files and registers a
// Driver per map. A controller that needs real logic (e.g. the web-only Web MIDI
// driver) can still be a hand-written Driver in its own subpackage, registered
// from its init(). Either way the app calls Detect/Present to open whichever
// controller is currently plugged in:
//
//	dev, err := midiin.Detect()
//
// Design rules (mirroring the rest of rp6):
//   - No Fyne here — this is pure logic, unit-testable without a display.
//   - No device-model knowledge in the framework: a controller speaks only in
//     device-agnostic Handlers (fire an absolute pad, play a note, toggle
//     transport) or forwards named Intents, so adding new hardware never touches
//     the UI. The mapping from a controller's buttons/notes lives in its .midimap
//     (or its hand-written subpackage), never here.
package midiin

import (
	"errors"
	"sync"
)

// Handlers is the surface an input controller drives in rp6. All fields may be
// nil and a driver MUST tolerate that (e.g. a controller with no transport
// control simply never calls Transport). The callbacks are invoked from the
// driver's read goroutine, so implementations that touch UI state must marshal
// onto their own thread.
type Handlers struct {
	// TriggerPad fires an absolute pad by its 0-based id (0..47, i.e.
	// bank*6+(pad-1)) at the given velocity (1..127). rp6 both plays the pad
	// (through the active P-6 or emulator) and reflects it in the UI.
	TriggerPad func(padID int, velocity uint8)

	// PlayNote plays a chromatic MIDI note (0..127) at the given velocity —
	// for keyboard-style controllers that drive rp6's on-screen keyboard
	// (pitching the selected sample) rather than the pads.
	PlayNote func(note, velocity uint8)

	// Transport requests the host transport change to the given state (true =
	// play, false = stop), e.g. from an encoder press.
	Transport func(playing bool)

	// Dispatch handles a named control intent from a data-driven controller
	// (internal/midiin/mapped). The application owns and validates the intent
	// vocabulary; the driver only forwards. Hand-written drivers (e.g. the web
	// MIDI driver) leave this nil and use the typed callbacks above instead. May
	// be nil, and a driver MUST tolerate that.
	Dispatch func(Intent)
}

// Intent is a named RP6 control action produced by a data-driven (mapped) input
// controller — see internal/midiin/mapped. This package intentionally does NOT
// own the set of valid names: the application (cmd/rp6) owns the intent
// vocabulary and validates names, while the mapped interpreter only forwards
// them. Only the fields relevant to a given Name are populated.
type Intent struct {
	// Name is the control action, e.g. "pad.trigger", "transport.play",
	// "tempo.set". Its meaning (and which fields below matter) is the app's.
	Name string
	// Pad is an absolute pad id (0..47) for pad.trigger, or a discrete integer
	// argument (track/bank index) for other intents.
	Pad int
	// Note is a chromatic MIDI note (0..127) for note.play.
	Note uint8
	// Velocity is 1..127 for trigger-shaped intents (pad.trigger, note.play).
	Velocity uint8
	// Value is a normalized 0..1 for value-shaped intents (the "*.set" family).
	Value float64
	// Delta is a signed detent count for delta-shaped intents (the "*.delta"
	// family), e.g. from a relative encoder.
	Delta int
	// Arg is an optional string argument (e.g. "seq", "a_d") for intents that
	// take one.
	Arg string
}

// Device is an opened MIDI input controller.
type Device interface {
	// Name is a human-readable model name (e.g. "Adafruit MacroPad RP2040").
	Name() string
	// Path is the underlying device node, for status and debugging.
	Path() string
	// Run listens for input and drives h, blocking until the device is closed;
	// it then returns the read error (see Close).
	Run(h Handlers) error
	// Close releases the device, unblocking Run.
	Close() error
}

// Driver detects and opens one kind of input controller. Drivers register
// themselves from their package init() so that blank-importing the package is
// enough to make Detect aware of them.
type Driver struct {
	// Name is the model name reported by the resulting Device.
	Name string
	// Detect reports whether this controller is present, returning the device
	// node to open. It must be cheap and free of side effects.
	Detect func() (path string, ok bool)
	// Open opens the controller at the node returned by Detect.
	Open func(path string) (Device, error)
}

var (
	mu      sync.Mutex
	drivers []Driver
)

// Register adds a driver to the registry. Call it from a driver package's
// init(). Drivers with a nil Detect or Open are ignored by Detect.
func Register(d Driver) {
	mu.Lock()
	defer mu.Unlock()
	drivers = append(drivers, d)
}

// ErrNotFound is returned by Detect when no registered controller is present.
var ErrNotFound = errors.New("midiin: no supported MIDI input controller found")

// Detect probes every registered driver in registration order and opens the
// first controller that is present. It returns ErrNotFound if none match.
func Detect() (Device, error) {
	mu.Lock()
	ds := append([]Driver(nil), drivers...)
	mu.Unlock()
	for _, d := range ds {
		if d.Detect == nil || d.Open == nil {
			continue
		}
		if path, ok := d.Detect(); ok {
			return d.Open(path)
		}
	}
	return nil, ErrNotFound
}

// Found is a present-but-not-yet-opened controller reported by Present.
type Found struct {
	// Name is the controller model (the driver's Name).
	Name string
	// Path is the device node it would open.
	Path string
	open func(string) (Device, error)
}

// Open opens the found controller.
func (f Found) Open() (Device, error) { return f.open(f.Path) }

// Present probes every registered driver and returns *all* controllers currently
// plugged in (each not yet opened). Unlike Detect it doesn't stop at the first,
// so several different controllers — e.g. a macropad and a MIDI keyboard — can be
// opened and used at the same time. Drivers with a nil Detect/Open are skipped.
func Present() []Found {
	mu.Lock()
	ds := append([]Driver(nil), drivers...)
	mu.Unlock()
	var out []Found
	for _, d := range ds {
		if d.Detect == nil || d.Open == nil {
			continue
		}
		if path, ok := d.Detect(); ok {
			out = append(out, Found{Name: d.Name, Path: path, open: d.Open})
		}
	}
	return out
}
