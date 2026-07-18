package mapped

import (
	"io"

	"github.com/mono4loop/rp6/internal/midiin"
	"github.com/mono4loop/rp6/p6"
)

// Interpreter-internal intents are handled inside the mapped device (they mutate
// its input-bank register) rather than being forwarded to the app. See §6 of
// docs/architecture/midimaps.md.
const (
	intentBankSet  = "input.bank.set"
	intentBankNext = "input.bank.next"
)

// IsInternalIntent reports whether name is handled inside the interpreter (and
// so is a valid binding target even though the app vocabulary doesn't list it).
func IsInternalIntent(name string) bool {
	return name == intentBankSet || name == intentBankNext
}

// System realtime status bytes (see p6/midi.go).
const (
	rtStart    = 0xFA
	rtContinue = 0xFB
	rtStop     = 0xFC
)

// Device is an opened controller driven by a Map. It reads MIDI from rc (an ALSA
// rawmidi node on the desktop) and forwards matched bindings as midiin.Intents.
type Device struct {
	path string
	rc   io.ReadCloser
	m    *Map
	bank int // input-bank register (for pad.trigger.rel / input.bank.*)
}

// Open opens the controller at path (a device node) bound to m.
func Open(path string, m *Map) (midiin.Device, error) {
	rc, err := openReader(path)
	if err != nil {
		return nil, err
	}
	return &Device{path: path, rc: rc, m: m}, nil
}

func (d *Device) Name() string { return d.m.Name }
func (d *Device) Path() string { return d.path }
func (d *Device) Close() error { return d.rc.Close() }

// Run parses incoming MIDI (reusing p6's tested parser) until the node is
// closed, forwarding each matched binding through h.Dispatch.
func (d *Device) Run(h midiin.Handlers) error {
	return p6.ParseMIDI(d.rc, func(ev p6.Event) { d.handle(h, ev) })
}

func (d *Device) handle(h midiin.Handlers, ev p6.Event) {
	for i := range d.m.Bindings {
		in, ok := d.eval(&d.m.Bindings[i], ev)
		if !ok {
			continue
		}
		if d.internal(&d.m.Bindings[i]) {
			continue // handled by the interpreter (bank register); not dispatched
		}
		if h.Dispatch != nil {
			h.Dispatch(in)
		}
	}
}

// internal applies (and consumes) an interpreter-internal binding, returning
// true if it was one.
func (d *Device) internal(b *Binding) bool {
	switch b.Intent {
	case intentBankSet:
		d.bank = argInt(b.Arg)
		return true
	case intentBankNext:
		d.bank++
		return true
	}
	return false
}

// eval matches ev against b. It returns the intent to emit and whether b matched
// (for internal bindings the returned intent is unused).
func (d *Device) eval(b *Binding, ev p6.Event) (midiin.Intent, bool) {
	in := midiin.Intent{Name: b.Intent, Arg: b.Arg}
	switch b.Src.kind {
	case srcNote:
		if ev.Type != p6.EventNoteOn || !d.chan1(b, ev.Channel) {
			return in, false
		}
		if ev.Data1 < b.Src.num || ev.Data1 > b.Src.hi {
			return in, false
		}
		in.Velocity = ev.Data2
		in.Note = ev.Data1
		off := b.Offset
		if b.Intent == "pad.trigger.rel" {
			stride := int(b.Src.hi-b.Src.num) + 1
			in.Pad = d.bank*stride + int(ev.Data1) - off
		} else {
			in.Pad = int(ev.Data1) - off
		}
		return in, true

	case srcCC:
		if ev.Type != p6.EventControlChange || ev.Data1 != b.Src.num || !d.chan1(b, ev.Channel) {
			return in, false
		}
		if !whenOK(b, ev.Data2) {
			return in, false
		}
		switch {
		case b.Abs:
			in.Value = scale(ev.Data2, b.ScaleLo, b.ScaleHi)
		case b.Rel != "":
			in.Delta = decodeRel(b.Rel, ev.Data2)
			if in.Delta == 0 {
				return in, false
			}
		default:
			in.Velocity = ev.Data2
			in.Pad = argInt(b.Arg)
		}
		return in, true

	case srcPC:
		if ev.Type != p6.EventProgramChange || !d.chan1(b, ev.Channel) {
			return in, false
		}
		in.Pad = int(ev.Data1)
		in.Value = float64(ev.Data1) / 127
		return in, true

	case srcRealtime:
		if ev.Type != p6.EventRealTime || !realtimeMatches(b.Src.word, ev.Status) {
			return in, false
		}
		return in, true
	}
	return in, false // srcMMC: not decoded yet (p6.ParseMIDI skips SysEx)
}

// chan1 reports whether ev's 1-based channel passes the binding's (or map's)
// channel filter. A zero filter means "any".
func (d *Device) chan1(b *Binding, ch int) bool {
	want := b.Channel
	if want == 0 {
		want = d.m.Channel
	}
	return want == 0 || want == ch
}

func whenOK(b *Binding, v uint8) bool {
	switch b.When {
	case whenEq:
		return v == b.WhenVal
	case whenGE:
		return v >= b.WhenVal
	}
	return true
}

// scale normalizes v within [lo,hi] to 0..1 (clamped).
func scale(v, lo, hi uint8) float64 {
	if hi <= lo {
		return 0
	}
	switch {
	case v <= lo:
		return 0
	case v >= hi:
		return 1
	}
	return float64(v-lo) / float64(hi-lo)
}

func realtimeMatches(word string, status byte) bool {
	switch word {
	case "start":
		return status == rtStart
	case "stop":
		return status == rtStop
	case "continue":
		return status == rtContinue
	}
	return false
}

// argInt parses an optional integer arg; returns 0 when absent/non-numeric.
func argInt(arg string) int {
	n := 0
	neg := false
	for i, c := range arg {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		return -n
	}
	return n
}
