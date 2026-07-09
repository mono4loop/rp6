// Package midiin is a small, pluggable framework for MIDI *input* controllers:
// external hardware (a macropad, a grid controller, a keyboard, …) that drives
// rp6's pads, on-screen keyboard and transport host-side. It is the mirror image
// of the p6 package (which sends MIDI *out* to the P-6): here MIDI comes *in*
// from a controller and is translated into rp6 actions.
//
// Each concrete controller lives in its own subpackage (e.g.
// internal/midiin/macropad) and registers a Driver from its init(). The app
// blank-imports the drivers it supports and calls Detect to open whichever
// controller is currently plugged in:
//
//	import _ "github.com/mono4loop/rp6/internal/midiin/macropad"
//	dev, err := midiin.Detect()
//
// Design rules (mirroring the rest of rp6):
//   - No Fyne here — this is pure logic, unit-testable without a display.
//   - No device-model knowledge in the framework: a controller speaks only in
//     device-agnostic Handlers (fire an absolute pad, toggle transport), so
//     adding new hardware never touches the UI. The mapping from a controller's
//     buttons/notes to those Handlers lives entirely inside its own subpackage.
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
