// Package midibridge is the platform bridge that lets a host-owned MIDI layer
// (initially the Android Activity, which owns android.media.midi.MidiManager)
// feed rp6's pure-Go MIDI backends.
//
// rp6's desktop backend talks to ALSA rawmidi nodes directly and the browser
// backend talks to the Web MIDI API from wasm. Android allows neither: apps
// can't touch /dev/snd, and USB MIDI must go through Java (MidiManager /
// android.hardware.usb). So on Android the Java side owns device discovery,
// opening ports, sending bytes (MidiInputPort.send) and receiving bytes (a
// MidiReceiver subclass); it talks to Go through this bridge, which gomobile
// binds into a small Java-callable API.
//
// The bridge itself is deliberately dependency-free and transport-agnostic — it
// only shuttles raw MIDI bytes between named ports and Go io.Writer/io.Reader
// endpoints, so it is fully unit-testable on the host with a fake OutputPort.
// The p6 Android backend (p6/device_android.go) and the Android midiin driver
// consume it exactly as the ALSA/Web MIDI backends consume their transports.
//
// # Java-facing API (bound by gomobile)
//
// The Java layer reports the MIDI devices it can see and wires their I/O:
//
//	AddDevice(id, name, hasInput, hasOutput)   // a device appeared
//	RemoveDevice(id)                           // a device went away
//	SetOutput(id, port)                        // provide the sender for id's output
//	ClearOutput(id)                            // detach the sender
//	PushInput(id, data)                        // bytes arrived from id's input
//	Reset()                                    // drop all state (Activity restart)
//
// "output" means data flowing rp6 -> device (e.g. the P-6's MIDI IN), and
// "input" means data flowing device -> rp6 (e.g. the P-6's MIDI OUT / a
// controller's keys). This matches the p6 package's "we open the P-6's node for
// both directions" model.
//
// # Go-facing API (used by the p6 / midiin backends)
//
// Backends enumerate the reported devices by index (avoids binding slices of
// structs), then grab an io.Writer for a device's output and an io.ReadCloser
// for its input:
//
//	for i := 0; i < midibridge.OutputCount(); i++ {
//	    if isP6(midibridge.OutputName(i)) { id := midibridge.OutputID(i); ... }
//	}
//	w := midibridge.Writer(id)          // MIDI out to the device
//	r := midibridge.OpenReader(id)      // MIDI in from the device (blocking)
package midibridge

import (
	"errors"
	"io"
	"sync"
)

// OutputPort is the sink for MIDI bytes flowing from rp6 to a device. The Java
// layer implements it (via gomobile's reverse-binding) as a wrapper around an
// android.media.midi.MidiInputPort, and registers it with SetOutput. Send
// receives one complete MIDI message per call (the p6 Device writes a whole
// message at a time).
type OutputPort interface {
	Send(data []byte) error
}

// ErrNoOutput is returned by a Writer whose device has no registered OutputPort
// (not yet opened by the platform layer, or already unplugged).
var ErrNoOutput = errors.New("midibridge: no output port registered for device")

// inputQueue is the buffer depth for a device's incoming MIDI, mirroring the
// Web MIDI backend. Pushes beyond it are dropped rather than blocking the
// platform callback.
const inputQueue = 256

type port struct {
	id     string
	name   string
	input  bool
	output bool
	out    OutputPort
	reader *InputReader
}

var (
	mu    sync.Mutex
	ports = map[string]*port{}
	order []string // stable insertion order, for index-based enumeration
)

// AddDevice registers a MIDI device the platform layer can see. id is an opaque,
// stable handle chosen by the platform (e.g. the MidiDeviceInfo id as a string);
// name is the human-readable port name matched by the backends (e.g. "P-6").
// hasInput/hasOutput report which directions the device offers. Re-adding an
// existing id updates its metadata without disturbing open I/O.
func AddDevice(id, name string, hasInput, hasOutput bool) {
	mu.Lock()
	defer mu.Unlock()
	p := ports[id]
	if p == nil {
		p = &port{id: id}
		ports[id] = p
		order = append(order, id)
	}
	p.name = name
	p.input = hasInput
	p.output = hasOutput
}

// RemoveDevice drops a device (e.g. on unplug), detaching its output and
// unblocking any open reader with io.EOF.
func RemoveDevice(id string) {
	mu.Lock()
	p := ports[id]
	if p == nil {
		mu.Unlock()
		return
	}
	rd := p.reader
	delete(ports, id)
	for i, o := range order {
		if o == id {
			order = append(order[:i], order[i+1:]...)
			break
		}
	}
	mu.Unlock()
	if rd != nil {
		rd.close()
	}
}

// SetOutput registers the sender for a device's output (rp6 -> device). The
// platform layer calls it after opening the device's MidiInputPort.
func SetOutput(id string, p OutputPort) {
	mu.Lock()
	defer mu.Unlock()
	if pt := ports[id]; pt != nil {
		pt.out = p
	}
}

// ClearOutput detaches a device's output sender (e.g. when closing its port).
func ClearOutput(id string) {
	mu.Lock()
	defer mu.Unlock()
	if pt := ports[id]; pt != nil {
		pt.out = nil
	}
}

// PushInput delivers MIDI bytes received from a device (device -> rp6) to that
// device's open reader, if any. It never blocks: when no reader is open or its
// buffer is full, the bytes are dropped (so the platform's MidiReceiver callback
// is never stalled). The data slice is copied, so the caller may reuse it.
func PushInput(id string, data []byte) {
	mu.Lock()
	var rd *InputReader
	if pt := ports[id]; pt != nil {
		rd = pt.reader
	}
	mu.Unlock()
	if rd != nil {
		rd.push(data)
	}
}

// Reset drops all devices and unblocks every open reader. Used when the platform
// layer restarts (e.g. Activity recreation) to start from a clean slate.
func Reset() {
	mu.Lock()
	readers := make([]*InputReader, 0, len(ports))
	for _, p := range ports {
		if p.reader != nil {
			readers = append(readers, p.reader)
		}
	}
	ports = map[string]*port{}
	order = nil
	mu.Unlock()
	for _, rd := range readers {
		rd.close()
	}
}

// OutputCount reports how many registered devices offer an output (rp6 ->
// device). OutputID/OutputName index into that filtered list [0, OutputCount).
func OutputCount() int { return countDir(true) }

// OutputID returns the opaque id of the i-th output-capable device, or "".
func OutputID(i int) string { return idAt(true, i) }

// OutputName returns the name of the i-th output-capable device, or "".
func OutputName(i int) string { return nameAt(true, i) }

// InputCount reports how many registered devices offer an input (device ->
// rp6). InputID/InputName index into that filtered list [0, InputCount).
func InputCount() int { return countDir(false) }

// InputID returns the opaque id of the i-th input-capable device, or "".
func InputID(i int) string { return idAt(false, i) }

// InputName returns the name of the i-th input-capable device, or "".
func InputName(i int) string { return nameAt(false, i) }

func matches(p *port, output bool) bool {
	if output {
		return p.output
	}
	return p.input
}

func countDir(output bool) int {
	mu.Lock()
	defer mu.Unlock()
	n := 0
	for _, id := range order {
		if matches(ports[id], output) {
			n++
		}
	}
	return n
}

func idAt(output bool, i int) string {
	mu.Lock()
	defer mu.Unlock()
	if p := nthLocked(output, i); p != nil {
		return p.id
	}
	return ""
}

func nameAt(output bool, i int) string {
	mu.Lock()
	defer mu.Unlock()
	if p := nthLocked(output, i); p != nil {
		return p.name
	}
	return ""
}

func nthLocked(output bool, i int) *port {
	if i < 0 {
		return nil
	}
	n := 0
	for _, id := range order {
		p := ports[id]
		if !matches(p, output) {
			continue
		}
		if n == i {
			return p
		}
		n++
	}
	return nil
}

// Writer returns a writer that forwards each Write to the device's currently
// registered OutputPort. Writes before SetOutput (or after ClearOutput /
// RemoveDevice) return ErrNoOutput. The writer is safe for concurrent use and
// stays valid across output (re)registration — it always resolves the port by
// id at write time. *OutputWriter satisfies io.Writer, so the p6 backend passes
// it straight to p6.New. (A concrete type is returned rather than io.Writer so
// the package binds cleanly under gomobile.)
func Writer(id string) *OutputWriter { return &OutputWriter{id: id} }

// OutputWriter forwards MIDI messages to a device's registered OutputPort.
type OutputWriter struct{ id string }

// Write sends b to the device's OutputPort as one MIDI message.
func (w *OutputWriter) Write(b []byte) (int, error) {
	mu.Lock()
	var out OutputPort
	if p := ports[w.id]; p != nil {
		out = p.out
	}
	mu.Unlock()
	if out == nil {
		return 0, ErrNoOutput
	}
	// Copy so the OutputPort implementation (Java) may retain the slice without
	// aliasing the caller's buffer.
	cp := make([]byte, len(b))
	copy(cp, b)
	if err := out.Send(cp); err != nil {
		return 0, err
	}
	return len(b), nil
}

// OpenReader opens (or replaces) the input reader for a device and returns it.
// Subsequent PushInput(id, …) calls feed the returned reader; Read blocks until
// data arrives or the reader is closed (Close, RemoveDevice or Reset), after
// which it returns io.EOF. Only one reader per device is active; opening again
// closes the previous one. Returns nil if the device is unknown.
func OpenReader(id string) *InputReader {
	mu.Lock()
	p := ports[id]
	if p == nil {
		mu.Unlock()
		return nil
	}
	old := p.reader
	rd := newInputReader()
	p.reader = rd
	mu.Unlock()
	if old != nil {
		old.close()
	}
	return rd
}

// InputReader is a non-blocking sink for a device's incoming MIDI (fed by
// PushInput) presented as a blocking io.ReadCloser for the p6 parser — the same
// shape as the Web MIDI backend's reader.
type InputReader struct {
	ch   chan []byte
	buf  []byte
	done chan struct{}
	once sync.Once
}

func newInputReader() *InputReader {
	return &InputReader{ch: make(chan []byte, inputQueue), done: make(chan struct{})}
}

func (r *InputReader) push(b []byte) {
	cp := make([]byte, len(b))
	copy(cp, b)
	select {
	case r.ch <- cp:
	case <-r.done:
	default: // buffer full — drop (never block the platform callback)
	}
}

// Read implements io.Reader, blocking until MIDI bytes are available or the
// reader is closed (then io.EOF).
func (r *InputReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		select {
		case b := <-r.ch:
			r.buf = b
		case <-r.done:
			return 0, io.EOF
		}
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// Close unblocks Read with io.EOF. Safe to call multiple times.
func (r *InputReader) Close() error {
	r.close()
	return nil
}

func (r *InputReader) close() { r.once.Do(func() { close(r.done) }) }
