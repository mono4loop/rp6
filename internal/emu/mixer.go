package emu

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/mono4loop/rp6/internal/audiofx"
)

// maxVoices caps simultaneous playing samples, mirroring the P-6's 16-voice
// sample polyphony. When exceeded, the oldest voice is dropped.
const maxVoices = 16

// keyboardBusFrames bounds the preallocated keyboard-effects scratch buffer.
// Larger backend callbacks are processed in consecutive chunks, so render never
// allocates because a device chose a larger period.
const keyboardBusFrames = 8192

// voice is one playing instance of a clip.
type voice struct {
	data  []float32 // interleaved at the mixer's output format
	posf  float64   // current fractional frame index into data
	speed float64   // frames advanced per output frame (1 = original pitch)
	gain  float32
	start int64 // absolute frame (mixer clock) at which this voice begins
	bus   voiceBus
}

type voiceBus uint8

const (
	busMain voiceBus = iota
	busKeyboard
)

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
	channels    int
	rate        int
	accurate    bool
	limiter     *peakLimiter
	keysFX      *audiofx.Instrument
	keysBuf     []float32
	keysFXOn    atomic.Bool
	keysFXReset atomic.Bool

	renderMu sync.Mutex // serializes audio callbacks and limiter reset
	mu       sync.Mutex // protects voices and timing from concurrent triggers

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
	return &mixer{
		channels: channels,
		rate:     rate,
		accurate: accurate,
		limiter:  newPeakLimiter(channels, rate),
		keysFX:   audiofx.NewInstrument(channels, rate),
		keysBuf:  make([]float32, keyboardBusFrames*channels),
	}
}

// trigger starts playing data (interleaved, already in output format) at the
// given gain (0..1) and original pitch. The newest voice replaces the oldest
// once maxVoices is hit.
func (m *mixer) trigger(data []float32, gain float32) {
	m.triggerVoice(data, gain, 1, busMain)
}

// triggerSpeed is trigger with an explicit playback speed: speed>1 raises the
// pitch (reads the clip faster), speed<1 lowers it. Used by keyboard mode to
// transpose a pad's sample chromatically. speed<=0 is treated as 1.
func (m *mixer) triggerSpeed(data []float32, gain float32, speed float64) {
	m.triggerVoice(data, gain, speed, busMain)
}

// triggerKeyboard starts a chromatically pitched voice on the keyboard bus,
// which passes through the instrument effects before joining the master mix.
func (m *mixer) triggerKeyboard(data []float32, gain float32, speed float64) {
	m.triggerVoice(data, gain, speed, busKeyboard)
}

func (m *mixer) triggerVoice(data []float32, gain float32, speed float64, bus voiceBus) {
	if len(data) == 0 {
		return
	}
	if speed <= 0 {
		speed = 1
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
	m.voices = append(m.voices, &voice{data: data, gain: gain, speed: speed, start: start, bus: bus})
}

// setKeyboardFX updates the keyboard bus's instrument-effect macros.
func (m *mixer) setKeyboardFX(settings audiofx.Settings) { m.keysFX.Set(settings) }

// setKeyboardFXEnabled bypasses or enables the keyboard effects without
// changing their parameters. Disabling also requests that the audio thread
// clear any existing tails before a later re-enable.
func (m *mixer) setKeyboardFXEnabled(enabled bool) {
	if !enabled {
		m.keysFXReset.Store(true)
	}
	m.keysFXOn.Store(enabled)
}

// active reports the number of currently playing voices.
func (m *mixer) active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.voices)
}

// render fills out with the mastered sum of all active voices, advancing and
// retiring them. out is fully overwritten (zeroed first).
func (m *mixer) render(out []float32) {
	m.renderMu.Lock()
	defer m.renderMu.Unlock()

	for len(out) > 0 {
		n := min(len(out), len(m.keysBuf))
		n -= n % m.channels
		if n == 0 {
			clear(out)
			return
		}
		m.renderBlock(out[:n])
		out = out[n:]
	}
}

func (m *mixer) renderBlock(out []float32) {
	for i := range out {
		out[i] = 0
	}
	keys := m.keysBuf[:len(out)]
	clear(keys)
	m.mixVoices(out, keys)

	if m.keysFXReset.Swap(false) {
		m.keysFX.Reset()
	}
	if m.keysFXOn.Load() {
		m.keysFX.Process(keys)
	}
	for i := range out {
		out[i] += keys[i]
	}
	m.limiter.process(out)
}

func (m *mixer) mixVoices(out, keys []float32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	bufStart := m.frameClock
	m.renderWall = time.Now()
	m.rendered = true
	frames := len(out) / m.channels
	m.frameClock += int64(frames)

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
		tf := len(v.data) / m.channels // total source frames
		dst := out
		if v.bus == busKeyboard {
			dst = keys
		}
		for f := off; f < frames; f++ {
			idx := int(v.posf)
			if idx >= tf {
				break
			}
			frac := float32(v.posf - float64(idx))
			base := idx * m.channels
			ob := f * m.channels
			for c := range m.channels {
				s0 := v.data[base+c]
				s1 := s0
				if idx+1 < tf { // linear interpolate toward the next frame
					s1 = v.data[base+m.channels+c]
				}
				dst[ob+c] += (s0 + (s1-s0)*frac) * v.gain
			}
			v.posf += v.speed
		}
		if int(v.posf) < tf {
			live = append(live, v)
		}
	}
	m.voices = live
}

// reset drops all playing voices (used on Close/panic).
func (m *mixer) reset() {
	m.renderMu.Lock()
	m.mu.Lock()
	m.voices = nil
	m.mu.Unlock()
	m.keysFX.Reset()
	clear(m.keysBuf)
	m.keysFXOn.Store(false)
	m.keysFXReset.Store(false)
	m.limiter.reset()
	m.renderMu.Unlock()
}
