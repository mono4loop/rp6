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
	posf  float64   // current fractional frame index into data
	speed float64   // frames advanced per output frame (1 = original pitch)
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
	limiter  *peakLimiter

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
	}
}

// trigger starts playing data (interleaved, already in output format) at the
// given gain (0..1) and original pitch. The newest voice replaces the oldest
// once maxVoices is hit.
func (m *mixer) trigger(data []float32, gain float32) {
	m.triggerSpeed(data, gain, 1)
}

// triggerSpeed is trigger with an explicit playback speed: speed>1 raises the
// pitch (reads the clip faster), speed<1 lowers it. Used by keyboard mode to
// transpose a pad's sample chromatically. speed<=0 is treated as 1.
func (m *mixer) triggerSpeed(data []float32, gain float32, speed float64) {
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
	m.voices = append(m.voices, &voice{data: data, gain: gain, speed: speed, start: start})
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

	for i := range out {
		out[i] = 0
	}
	m.mu.Lock()

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
				out[ob+c] += (s0 + (s1-s0)*frac) * v.gain
			}
			v.posf += v.speed
		}
		if int(v.posf) < tf {
			live = append(live, v)
		}
	}
	m.voices = live
	m.mu.Unlock()

	m.limiter.process(out)
}

// reset drops all playing voices (used on Close/panic).
func (m *mixer) reset() {
	m.renderMu.Lock()
	m.mu.Lock()
	m.voices = nil
	m.mu.Unlock()
	m.limiter.reset()
	m.renderMu.Unlock()
}
