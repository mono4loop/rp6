//go:build js

// Web MIDI backend for the P-6 (browser/wasm). It implements the same
// Discover/Open/OpenPath surface as the ALSA backend (device_alsa.go), but talks
// to the P-6 over the browser's Web MIDI API (navigator.requestMIDIAccess). The
// resulting *Device reuses all of device.go's message builders and input.go's
// parser — only the byte transport (an io.Writer to a MIDIOutput, and an
// io.Reader fed by MIDIInput events) is browser-specific.
//
// Web MIDI is available in Chromium-based browsers (with user permission) and
// not in Firefox; where it is unavailable, Discover returns ErrNotFound and the
// app stays on the emulator.
package p6

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"syscall/js"
)

var (
	midiOnce    sync.Once
	midiAccess  js.Value
	midiReady   atomic.Bool
	midiSuccess js.Func // kept alive while registered as a promise handler
	midiFailure js.Func
)

// ensureAccess requests Web MIDI access once, asynchronously. Until the user
// grants it, midiReady stays false (so Discover reports ErrNotFound and the app
// runs the emulator); once granted, the outputs/inputs maps are read live.
func ensureAccess() {
	midiOnce.Do(func() {
		nav := js.Global().Get("navigator")
		if !nav.Truthy() || nav.Get("requestMIDIAccess").IsUndefined() {
			return // browser has no Web MIDI (e.g. Firefox)
		}
		midiSuccess = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				midiAccess = args[0]
				midiReady.Store(true)
			}
			return nil
		})
		midiFailure = js.FuncOf(func(js.Value, []js.Value) any { return nil })
		opts := map[string]any{"sysex": false}
		nav.Call("requestMIDIAccess", opts).Call("then", midiSuccess, midiFailure)
	})
}

// findPort returns the first port in a MIDIPortMap whose name matches.
func findPort(portMap js.Value, match func(string) bool) (js.Value, bool) {
	var found js.Value
	ok := false
	cb := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if ok || len(args) == 0 {
			return nil
		}
		port := args[0]
		if match(strings.ToLower(port.Get("name").String())) {
			found, ok = port, true
		}
		return nil
	})
	defer cb.Release()
	portMap.Call("forEach", cb)
	return found, ok
}

// Discover reports a connected P-6 over Web MIDI (once access is granted).
func Discover() (string, error) {
	ensureAccess()
	if !midiReady.Load() {
		return "", ErrNotFound
	}
	port, ok := findPort(midiAccess.Get("outputs"), isP6Name)
	if !ok {
		return "", ErrNotFound
	}
	return "webmidi:" + port.Get("name").String(), nil
}

// Open connects to a P-6 over Web MIDI using the default configuration.
func Open() (*Device, error) { return OpenPath("", DefaultConfig()) }

// OpenPath connects to the P-6's Web MIDI output (and its input, if present, for
// Listen). The path is informational; the P-6 is located by port name.
func OpenPath(_ string, cfg Config) (*Device, error) {
	ensureAccess()
	if !midiReady.Load() {
		return nil, ErrNotFound
	}
	out, ok := findPort(midiAccess.Get("outputs"), isP6Name)
	if !ok {
		return nil, ErrNotFound
	}
	out.Call("open")

	d := New(&webMIDIWriter{port: out}, cfg)
	d.path = "webmidi:" + out.Get("name").String()

	closer := &webMIDICloser{out: out}

	// Wire the P-6's MIDI input (if any) so Listen reflects hardware pad presses.
	if in, ok := findPort(midiAccess.Get("inputs"), isP6Name); ok {
		rd := newWebMIDIReader()
		onmsg := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			data := args[0].Get("data") // Uint8Array
			n := data.Get("length").Int()
			if n > 0 {
				b := make([]byte, n)
				js.CopyBytesToGo(b, data)
				rd.push(b)
			}
			return nil
		})
		in.Set("onmidimessage", onmsg)
		in.Call("open")
		d.r = rd
		closer.in = in
		closer.onmsg = onmsg
		closer.reader = rd
	}

	d.c = closer
	return d, nil
}

// webMIDIWriter sends complete MIDI messages to a MIDIOutput port. Device.send
// calls Write once per message, which maps to one MIDIOutput.send.
type webMIDIWriter struct{ port js.Value }

func (w *webMIDIWriter) Write(b []byte) (int, error) {
	arr := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(arr, b)
	w.port.Call("send", arr)
	return len(b), nil
}

// webMIDIReader is a non-blocking sink for incoming MIDI messages (pushed from
// the onmidimessage callback) that presents them as a blocking io.Reader for the
// parser. Close makes Read return io.EOF, unblocking Listen.
type webMIDIReader struct {
	ch   chan []byte
	buf  []byte
	done chan struct{}
	once sync.Once
}

func newWebMIDIReader() *webMIDIReader {
	return &webMIDIReader{ch: make(chan []byte, 256), done: make(chan struct{})}
}

func (r *webMIDIReader) push(b []byte) {
	select {
	case r.ch <- b:
	case <-r.done:
	default: // buffer full — drop (never block the JS callback)
	}
}

func (r *webMIDIReader) Read(p []byte) (int, error) {
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

func (r *webMIDIReader) close() { r.once.Do(func() { close(r.done) }) }

// webMIDICloser releases the Web MIDI resources when the Device is closed.
type webMIDICloser struct {
	out    js.Value
	in     js.Value
	onmsg  js.Func
	reader *webMIDIReader
}

func (c *webMIDICloser) Close() error {
	if c.in.Truthy() {
		c.in.Set("onmidimessage", js.Null())
	}
	if c.reader != nil {
		c.reader.close()
	}
	if c.onmsg.Truthy() {
		c.onmsg.Release()
	}
	return nil
}
