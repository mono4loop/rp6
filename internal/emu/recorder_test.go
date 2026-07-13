package emu

import (
	"testing"

	"github.com/mono4loop/rp6/internal/recorder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecorderTapExcludesRecorderPlayback(t *testing.T) {
	m := newMixer(1, 1000, false)
	rec := recorder.New(1, 1000)
	require.NoError(t, rec.SetClip(0, recorder.Clip{Samples: []float32{0.8, 0.8}, Channels: 1, SampleRate: 1000}))
	require.NoError(t, rec.ArmRecord(1))
	require.True(t, rec.TriggerRecord())
	m.setRecorder(rec, rec.Capture)
	require.NoError(t, rec.Play(0))

	out := make([]float32, 8)
	m.render(out)
	rec.StopRecordingImmediate()

	captured, ok := rec.Clip(1)
	require.True(t, ok)
	assert.NotEmpty(t, captured.Samples)
	for _, sample := range captured.Samples {
		assert.Zero(t, sample, "recorder playback must not feed the emulator capture tap")
	}
	assert.Contains(t, out, float32(0.8), "the recorder clip still reaches output")
}
