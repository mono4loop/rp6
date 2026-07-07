// Package audio provides reusable audio-capture primitives, independent of any
// UI or MIDI code. A Capturer delivers raw PCM frames from an input device; a
// Meter turns those frames into a smoothed 0..1 level for a VU display. The raw
// Capturer is intentionally general so future features (e.g. a live looper) can
// consume the same frames.
package audio

import (
	"errors"
	"math"
	"sync"
)

// ErrUnavailable is returned by OpenCapture when no capture backend is compiled
// in (build without the "portaudio" tag) or no matching device is found.
var ErrUnavailable = errors.New("audio: capture unavailable")

// Format describes a capture stream's PCM layout.
type Format struct {
	SampleRate int
	Channels   int
}

// FrameFunc consumes a block of interleaved float32 samples in [-1, 1]. It is
// invoked from a capture/audio thread and must not block.
type FrameFunc func(samples []float32)

// Capturer is a source of audio frames from an input device. Start begins
// delivery to the handler; Stop halts it; Close releases the device.
type Capturer interface {
	Start(FrameFunc) error
	Stop() error
	Format() Format
	Name() string
	Close() error
}

// Peak returns the maximum absolute sample value (0..1) in a block.
func Peak(samples []float32) float64 {
	var p float32
	for _, s := range samples {
		if s < 0 {
			s = -s
		}
		if s > p {
			p = s
		}
	}
	return float64(p)
}

// RMS returns the root-mean-square amplitude (0..1) of a block.
func RMS(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(samples)))
}

// NormDB maps a linear amplitude (0..1) to a 0..1 display value on a decibel
// scale spanning floorDB..0 dB. Audio levels are small in linear terms, so a
// linear meter barely moves; a dB scale spreads them across the display.
// level<=0 or at/below floorDB maps to 0; level>=1 (0 dB) maps to 1.
func NormDB(level, floorDB float64) float64 {
	if level <= 0 || floorDB >= 0 {
		return 0
	}
	db := 20 * math.Log10(level)
	if db <= floorDB {
		return 0
	}
	if db >= 0 {
		return 1
	}
	return (db - floorDB) / -floorDB
}

// Meter wraps a Capturer and exposes a smoothed peak level (0..1) with a fast
// attack and slower release, suitable for driving a VU display. Level is safe
// to call from any goroutine.
type Meter struct {
	cap     Capturer
	attack  float64
	release float64
	gate    float64 // levels below this read as silence (noise floor)

	mu    sync.Mutex
	level float64
}

// NewMeter returns a Meter over c using default attack/release smoothing and a
// small noise gate so a silent input reads as zero (no stuck bottom segment).
func NewMeter(c Capturer) *Meter {
	return &Meter{cap: c, attack: 0.5, release: 0.15, gate: 0.02}
}

// Start begins capturing and metering.
func (m *Meter) Start() error { return m.cap.Start(m.onFrames) }

// Stop halts capturing.
func (m *Meter) Stop() error { return m.cap.Stop() }

// Close releases the underlying device.
func (m *Meter) Close() error { return m.cap.Close() }

// Level returns the current smoothed level (0..1).
func (m *Meter) Level() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.level
}

func (m *Meter) onFrames(samples []float32) {
	p := Peak(samples)
	if p < m.gate {
		p = 0 // gate the noise floor so silence reads as zero
	}
	m.mu.Lock()
	coef := m.release
	if p > m.level {
		coef = m.attack
	}
	m.level += (p - m.level) * coef
	if m.level < 0.001 {
		m.level = 0
	}
	m.mu.Unlock()
}

// Name returns the underlying device name.
func (m *Meter) Name() string { return m.cap.Name() }
