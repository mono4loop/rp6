package recorder

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/mono4loop/rp6/internal/audiofx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClip(samples ...float32) Clip { return Clip{Samples: samples, Channels: 1, SampleRate: 100} }

func TestPlayAllStartsTracksOnSameFrame(t *testing.T) {
	e := New(1, 100)
	require.NoError(t, e.SetClip(0, testClip(0.2, 0.2)))
	require.NoError(t, e.SetClip(1, testClip(0.3, 0.3)))
	e.SetQuantization(QuantizeBeat)
	e.SetTempo(60) // one beat = 100 frames

	e.Mix(make([]float32, 25))
	e.PlayAll()
	out := make([]float32, 90)
	e.Mix(out)

	for _, sample := range out[:75] {
		assert.Zero(t, sample)
	}
	assert.InDelta(t, 0.5, out[76], 0.001)
	assert.InDelta(t, 0.5, out[77], 0.001)
}

func TestMuteSoloLoopAndOneShot(t *testing.T) {
	e := New(1, 100)
	require.NoError(t, e.SetClip(0, testClip(0.2, 0.3)))
	require.NoError(t, e.SetClip(1, testClip(0.4, 0.5)))
	require.NoError(t, e.SetLoop(0, false))
	require.NoError(t, e.SetMuted(1, true))
	e.PlayAll()
	out := make([]float32, 5)
	e.Mix(out)
	assert.InDeltaSlice(t, []float32{0, 0.2, 0.3, 0, 0}, out, 0.001, "limiter adds a one-frame lookahead")
	assert.False(t, e.Playing(0), "one-shot stops at its end")

	require.NoError(t, e.SetMuted(1, false))
	require.NoError(t, e.SetSolo(0, true))
	e.PlayAll()
	out = make([]float32, 4)
	e.Mix(out)
	assert.NotContains(t, out, float32(0.4), "solo track excludes the other track")
}

func TestArmPadTriggerCaptureAndQuantizedStop(t *testing.T) {
	e := New(1, 100)
	e.SetTempo(60)
	e.SetQuantization(QuantizeBeat)
	e.Capture(make([]float32, 25))
	require.NoError(t, e.ArmRecord(3))
	require.True(t, e.TriggerRecord())

	e.Capture([]float32{9, 9}) // waiting for frame 100
	assert.Equal(t, 3, e.RecordPendingTrack())
	e.Capture(make([]float32, 73))
	e.Capture([]float32{0.1, 0.2, 0.3})
	assert.Equal(t, 3, e.RecordingTrack())

	e.SetQuantization(QuantizeOff)
	require.True(t, e.StopRecording())
	e.Capture([]float32{0.9})
	clip, ok := e.Clip(3)
	require.True(t, ok)
	assert.InDeltaSlice(t, []float32{0.1, 0.2, 0.3}, clip.Samples, 0.0001)
}

func TestQuantizedRecordingUsesCaptureClockWithoutPlayback(t *testing.T) {
	e := New(1, 100)
	e.SetTempo(60)
	e.SetQuantization(QuantizeBeat)
	e.Capture(make([]float32, 25))
	require.NoError(t, e.ArmRecord(0))
	require.True(t, e.TriggerRecord())

	e.Capture(make([]float32, 75))
	assert.Equal(t, 0, e.RecordPendingTrack())
	e.Capture([]float32{0.5})
	assert.Equal(t, 0, e.RecordingTrack(), "capture starts without the playback clock advancing")
}

func TestCannotReplaceActiveRecording(t *testing.T) {
	e := New(1, 100)
	require.NoError(t, e.ArmRecord(0))
	require.True(t, e.TriggerRecord())
	e.Capture([]float32{0.25})
	require.ErrorIs(t, e.ArmRecord(1), ErrRecording)
	e.StopRecordingImmediate()
	clip, ok := e.Clip(0)
	require.True(t, ok)
	assert.InDeltaSlice(t, []float32{0.25}, clip.Samples, 0.0001)
}

func TestStoppingArmedTrackPreservesExistingClip(t *testing.T) {
	e := New(1, 100)
	require.NoError(t, e.SetClip(0, testClip(0.5)))
	require.NoError(t, e.ArmRecord(0))
	assert.False(t, e.StopRecordingImmediate())
	clip, ok := e.Clip(0)
	require.True(t, ok)
	assert.InDeltaSlice(t, []float32{0.5}, clip.Samples, 0.0001)
}

func TestRecordingLimitIsReported(t *testing.T) {
	e := New(1, 1)
	require.NoError(t, e.ArmRecord(0))
	require.True(t, e.TriggerRecord())
	e.Capture(make([]float32, MaxRecordSeconds+1))
	assert.True(t, e.Truncated(0))
	assert.Equal(t, MaxRecordSeconds, e.DurationFrames(0))
}

func TestCaptureDoesNotAllocate(t *testing.T) {
	e := New(2, 48000)
	require.NoError(t, e.ArmRecord(0))
	require.True(t, e.TriggerRecord())
	samples := make([]float32, 512)
	e.Capture(samples)
	allocs := testing.AllocsPerRun(100, func() { e.Capture(samples) })
	assert.Zero(t, allocs)
}

func TestTrackEffectsAreIndependent(t *testing.T) {
	e := New(1, 1000)
	require.NoError(t, e.SetClip(0, Clip{Samples: make([]float32, 20), Channels: 1, SampleRate: 1000}))
	require.NoError(t, e.SetClip(1, Clip{Samples: make([]float32, 20), Channels: 1, SampleRate: 1000}))
	require.NoError(t, e.SetEffects(0, audiofx.Settings{Reverb: 1}))
	assert.Equal(t, float32(1), e.Effects(0).Reverb)
	assert.Zero(t, e.Effects(1).Reverb)
}

func TestProjectRoundTripAndWAVExport(t *testing.T) {
	e := New(2, 48000)
	clip := Clip{Samples: []float32{0.25, -0.25, 0.5, -0.5}, Channels: 2, SampleRate: 48000}
	require.NoError(t, e.SetClip(2, clip))
	require.NoError(t, e.SetName(2, "drums"))
	require.NoError(t, e.SetMuted(2, true))
	require.NoError(t, e.SetPan(2, 0.4))
	require.NoError(t, e.SetEffects(2, audiofx.Settings{Delay: 0.5}))
	e.SetQuantization(QuantizeBar)

	dir := filepath.Join(t.TempDir(), "project")
	require.NoError(t, e.Save(dir))

	restored := New(2, 48000)
	require.NoError(t, restored.Load(dir))
	assert.Equal(t, "drums", restored.Name(2))
	assert.True(t, restored.Muted(2))
	assert.InDelta(t, 0.4, restored.Pan(2), 0.001)
	assert.InDelta(t, 0.5, restored.Effects(2).Delay, 0.001)
	assert.Equal(t, QuantizeBar, restored.Quantization())
	got, ok := restored.Clip(2)
	require.True(t, ok)
	assert.InDeltaSlice(t, clip.Samples, got.Samples, 0.0001)

	var wav bytes.Buffer
	require.NoError(t, restored.ExportWAV(2, &wav))
	decoded, err := DecodeWAV(&wav)
	require.NoError(t, err)
	assert.Equal(t, 2, decoded.Channels)
	assert.Equal(t, 48000, decoded.SampleRate)
}

func TestMixDoesNotAllocate(t *testing.T) {
	e := New(2, 48000)
	require.NoError(t, e.SetClip(0, Clip{Samples: make([]float32, 2048), Channels: 2, SampleRate: 48000}))
	require.NoError(t, e.Play(0))
	out := make([]float32, 512)
	e.Mix(out)
	allocs := testing.AllocsPerRun(100, func() { e.Mix(out) })
	assert.Zero(t, allocs)
}
