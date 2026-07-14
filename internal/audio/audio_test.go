package audio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeakAndRMS(t *testing.T) {
	assert.Equal(t, 0.0, Peak(nil))
	assert.Equal(t, 0.0, RMS(nil))

	s := []float32{0, 0.5, -0.8, 0.2}
	assert.InDelta(t, 0.8, Peak(s), 1e-6)
	assert.InDelta(t, 0.4822, RMS(s), 1e-3)

	// Full-scale square wave: peak and RMS both 1.
	sq := []float32{1, -1, 1, -1}
	assert.InDelta(t, 1.0, Peak(sq), 1e-6)
	assert.InDelta(t, 1.0, RMS(sq), 1e-6)
}

// fakeCapturer lets tests push frames without any hardware.
type fakeCapturer struct {
	fn      FrameFunc
	started bool
	closed  bool
}

func (f *fakeCapturer) Start(fn FrameFunc) error { f.fn = fn; f.started = true; return nil }
func (f *fakeCapturer) Stop() error              { f.started = false; return nil }
func (f *fakeCapturer) Format() Format           { return Format{SampleRate: 48000, Channels: 2} }
func (f *fakeCapturer) Name() string             { return "fake" }
func (f *fakeCapturer) Close() error             { f.closed = true; return nil }
func (f *fakeCapturer) push(s []float32)         { f.fn(s) }

func TestMeterTracksLevel(t *testing.T) {
	fc := &fakeCapturer{}
	m := NewMeter(fc)
	require.NoError(t, m.Start())
	assert.True(t, fc.started)
	assert.Equal(t, 0.0, m.Level())

	// Feed loud blocks: level rises (fast attack).
	loud := []float32{1, -1, 1, -1}
	for range 5 {
		fc.push(loud)
	}
	assert.Greater(t, m.Level(), 0.8)

	// Feed silence: level falls (slower release) but decreases.
	before := m.Level()
	silence := make([]float32, 4)
	fc.push(silence)
	assert.Less(t, m.Level(), before)

	require.NoError(t, m.Stop())
	require.NoError(t, m.Close())
	assert.True(t, fc.closed)
}

func TestMeterCanConsumeSharedStream(t *testing.T) {
	m := NewMeter(nil)
	require.NoError(t, m.Start())
	m.Process([]float32{1, -1})
	assert.Greater(t, m.Level(), 0.4)
	require.NoError(t, m.Stop())
	require.NoError(t, m.Close())
}

func TestNormDB(t *testing.T) {
	// 0 dB (full scale) -> 1; silence -> 0; at/below floor -> 0.
	assert.Equal(t, 1.0, NormDB(1.0, -42))
	assert.Equal(t, 0.0, NormDB(0, -42))
	assert.Equal(t, 0.0, NormDB(0.001, -42)) // ~-60 dB, below floor
	// -21 dB is the midpoint of a -42..0 range -> ~0.5.
	half := NormDB(0.0891, -42) // 20*log10(0.0891) ≈ -21 dB
	assert.InDelta(t, 0.5, half, 0.02)
	// -6 dB (0.5 linear) should be high on the scale.
	assert.InDelta(t, ((-6.0)-(-42))/42, NormDB(0.5, -42), 0.02)
	// guard against bad floor
	assert.Equal(t, 0.0, NormDB(0.5, 0))
}

func TestOpenCaptureStub(t *testing.T) {
	// Without the portaudio build tag this returns ErrUnavailable.
	_, err := OpenCapture("P-6")
	assert.ErrorIs(t, err, ErrUnavailable)
}
