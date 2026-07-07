package emu

import (
	"sync"
	"time"
)

// maxVoices caps simultaneous playing samples, mirroring the P-6's 16-voice
// sample polyphony. When exceeded, the oldest voice is dropped.
const maxVoices = 16

// voice is one playing instance of a clip.
type voice struct {
	data  []float32 // interleaved at the mixer's output format
	pos   int       // current interleaved read index
	gain  float32
	start int64 // absolute frame (mixer clock) at which this voice begins
}

// mixer sums active voices into an output buffer. It is safe for concurrent
// use: triggers arrive from the app/sequencer goroutines while render runs on
// the audio thread.
//
// When accurate is set, each voice begins at the sub-buffer frame matching the
// time since the last render, so near-simultaneous triggers (e.g. a chord on an
// external MIDI pad) keep their true relative timing instead of snapping to the
// audio-buffer boundary (which flams them by up to one period). Otherwise a
// voice starts at the beginning of the next buffer (lower latency, coarser).
type mixer struct {
	channels int
	rate     int
	accurate bool

	mu         sync.Mutex
	voices     []*voice
	frameClock int64     // total frames rendered so far (mixer clock)
	renderWall time.Time // wall-clock at the start of the last render
	rendered   bool      // a render has happened (renderWall/frameClock valid)
}

func newMixer(channels, rate int, accurate bool) *mixer {
	if channels <= 0 {
		channels = 1
	}
	if rate <= 0 {
		rate = 48000
	}
	return &mixer{channels: channels, rate: rate, accurate: accurate}
}

// trigger starts playing data (interleaved, already in output format) at the
// given gain (0..1). The newest voice replaces the oldest once maxVoices is hit.
func (m *mixer) trigger(data []float32, gain float32) {
	if len(data) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	start := m.frameClock
	if m.accurate && m.rendered {
		// Place the voice at the frame matching the elapsed time since the last
		// buffer began — it lands in the next buffer at that sub-buffer offset,
		// preserving the real inter-trigger timing.
		if elapsed := time.Since(m.renderWall).Seconds(); elapsed > 0 {
			start += int64(elapsed * float64(m.rate))
		}
	}
	if len(m.voices) >= maxVoices {
		m.voices = m.voices[1:]
	}
	m.voices = append(m.voices, &voice{data: data, gain: gain, start: start})
}

// active reports the number of currently playing voices.
func (m *mixer) active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.voices)
}

// render fills out with the sum of all active voices, advancing and retiring
// them. out is fully overwritten (zeroed first). Output is clamped to [-1, 1].
func (m *mixer) render(out []float32) {
	for i := range out {
		out[i] = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	bufStart := m.frameClock
	m.renderWall = time.Now()
	m.rendered = true
	frames := len(out) / m.channels
	m.frameClock += int64(frames)

	if len(m.voices) == 0 {
		return
	}
	live := m.voices[:0]
	for _, v := range m.voices {
		off := int(v.start - bufStart) // start frame within this buffer
		if off >= frames {
			live = append(live, v) // scheduled for a later buffer
			continue
		}
		if off < 0 {
			off = 0 // already playing (or buffer-aligned)
		}
		outStart := off * m.channels
		n := min(len(out)-outStart, len(v.data)-v.pos)
		for i := range n {
			out[outStart+i] += v.data[v.pos+i] * v.gain
		}
		v.pos += n
		if v.pos < len(v.data) {
			live = append(live, v)
		}
	}
	m.voices = live
	for i := range out {
		if out[i] > 1 {
			out[i] = 1
		} else if out[i] < -1 {
			out[i] = -1
		}
	}
}

// reset drops all playing voices (used on Close/panic).
func (m *mixer) reset() {
	m.mu.Lock()
	m.voices = nil
	m.mu.Unlock()
}
