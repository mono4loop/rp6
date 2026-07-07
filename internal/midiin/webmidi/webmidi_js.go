//go:build js

package webmidi

import (
	"io"
	"strings"
	"sync"
	"syscall/js"

	"github.com/mono4loop/rp6/internal/midiin"
	"github.com/mono4loop/rp6/p6"
)

// Name is the model name reported to the app.
const Name = "Web MIDI Controller"

// controllerMatch reports whether a MIDI input port is a controller we drive.
// It matches the MacroPad and deliberately excludes the P-6 (whose own input is
// handled by the p6 Web MIDI device), so we don't double-handle its pad presses.
func controllerMatch(name string) bool {
	n := strings.ToLower(name)
	if strings.Contains(n, "p-6") || strings.Contains(n, "p6") {
		return false
	}
	return strings.Contains(n, "macropad")
}

// Realtime transport status bytes (see p6/midi.go).
const (
	midiStart = 0xFA
	midiStop  = 0xFC
)

func init() {
	midiin.Register(midiin.Driver{
		Name: Name,
		// Present whenever the browser exposes Web MIDI (cheap, no prompt here —
		// access is requested lazily in Run).
		Detect: func() (string, bool) {
			nav := js.Global().Get("navigator")
			if !nav.Truthy() || nav.Get("requestMIDIAccess").IsUndefined() {
				return "", false
			}
			return "webmidi", true
		},
		Open: func(path string) (midiin.Device, error) { return &device{path: path, rd: newReader()}, nil },
	})
}

type device struct {
	path string
	rd   *reader

	mu       sync.Mutex
	access   js.Value
	onState  js.Func
	onMsgs   []js.Func       // onmidimessage handlers, for Release on Close
	attached map[string]bool // input port ids we've wired
}

func (d *device) Name() string { return Name }
func (d *device) Path() string { return d.path }

// Run requests Web MIDI access, attaches to any matching controller input (now
// and as ports appear via statechange), and parses their MIDI into Handlers
// calls until Close (which unblocks the parser via the reader).
func (d *device) Run(h midiin.Handlers) error {
	access, ok := requestAccess(d.rd.done)
	if !ok {
		// No access (denied / unsupported) — block until Close so the app's Run
		// goroutine exits cleanly on shutdown rather than reporting an error.
		<-d.rd.done
		return nil
	}
	d.mu.Lock()
	d.access = access
	d.attached = map[string]bool{}
	d.mu.Unlock()

	d.attachAll()
	d.onState = js.FuncOf(func(js.Value, []js.Value) any { d.attachAll(); return nil })
	access.Set("onstatechange", d.onState)

	// Blocks until the reader is closed by Close.
	return p6.ParseMIDI(d.rd, func(ev p6.Event) { handle(h, ev) })
}

// attachAll wires an onmidimessage handler to every matching input port not yet
// attached (idempotent; safe to call repeatedly from statechange).
func (d *device) attachAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.access.Truthy() {
		return
	}
	cb := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		port := args[0]
		id := port.Get("id").String()
		if d.attached[id] || !controllerMatch(port.Get("name").String()) {
			return nil
		}
		onmsg := js.FuncOf(func(_ js.Value, margs []js.Value) any {
			if len(margs) == 0 {
				return nil
			}
			data := margs[0].Get("data")
			n := data.Get("length").Int()
			if n > 0 {
				b := make([]byte, n)
				js.CopyBytesToGo(b, data)
				d.rd.push(b)
			}
			return nil
		})
		port.Set("onmidimessage", onmsg)
		port.Call("open")
		d.attached[id] = true
		d.onMsgs = append(d.onMsgs, onmsg)
		return nil
	})
	defer cb.Release()
	d.access.Get("inputs").Call("forEach", cb)
}

func (d *device) Close() error {
	d.rd.close() // unblocks Run's parser
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.access.Truthy() {
		d.access.Set("onstatechange", js.Null())
	}
	if d.onState.Truthy() {
		d.onState.Release()
	}
	for _, f := range d.onMsgs {
		f.Release()
	}
	d.onMsgs = nil
	return nil
}

// handle maps one incoming MIDI event to a Handlers call (mirrors the macropad
// driver): pad notes (48..95) trigger pads; realtime Start/Stop toggle transport.
func handle(h midiin.Handlers, ev p6.Event) {
	switch ev.Type {
	case p6.EventNoteOn:
		if h.TriggerPad == nil {
			return
		}
		bank, pad, err := p6.PadForNote(ev.Data1)
		if err != nil {
			return
		}
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

// --- Web MIDI access + non-blocking reader ---------------------------------

// requestAccess requests Web MIDI access and blocks (yielding to the JS event
// loop) until it resolves, or cancel is closed. ok is false if unavailable.
func requestAccess(cancel <-chan struct{}) (js.Value, bool) {
	nav := js.Global().Get("navigator")
	if !nav.Truthy() || nav.Get("requestMIDIAccess").IsUndefined() {
		return js.Value{}, false
	}
	type res struct {
		v  js.Value
		ok bool
	}
	ch := make(chan res, 1)
	success := js.FuncOf(func(_ js.Value, args []js.Value) any {
		v := js.Value{}
		if len(args) > 0 {
			v = args[0]
		}
		ch <- res{v, true}
		return nil
	})
	failure := js.FuncOf(func(js.Value, []js.Value) any { ch <- res{js.Value{}, false}; return nil })
	defer success.Release()
	defer failure.Release()
	nav.Call("requestMIDIAccess", map[string]any{"sysex": false}).Call("then", success, failure)
	select {
	case r := <-ch:
		return r.v, r.ok
	case <-cancel:
		return js.Value{}, false
	}
}

// reader presents MIDI messages pushed from onmidimessage callbacks as a
// blocking io.Reader for p6.ParseMIDI. close makes Read return io.EOF.
type reader struct {
	ch   chan []byte
	buf  []byte
	done chan struct{}
	once sync.Once
}

func newReader() *reader {
	return &reader{ch: make(chan []byte, 256), done: make(chan struct{})}
}

func (r *reader) push(b []byte) {
	select {
	case r.ch <- b:
	case <-r.done:
	default: // full — drop rather than block the JS callback
	}
}

func (r *reader) Read(p []byte) (int, error) {
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

func (r *reader) close() { r.once.Do(func() { close(r.done) }) }
