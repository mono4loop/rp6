package emu

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPeakLimiterLeavesQuietAudioUnchanged(t *testing.T) {
	l := newPeakLimiter(1, 1000) // 2-frame look-ahead
	samples := []float32{0.25, -0.5, 0.4, -0.2, 0, 0}
	l.process(samples)

	assert.Equal(t, []float32{0, 0, 0.25, -0.5, 0.4, -0.2}, samples)
}

func TestPeakLimiterAnticipatesPeak(t *testing.T) {
	l := newPeakLimiter(1, 1000) // 2-frame look-ahead
	samples := []float32{0.25, 0.5, 2, 0.5, 0, 0, 0}
	l.process(samples)

	assert.InDelta(t, limiterCeiling, samples[4], 1e-6)
	for _, sample := range samples {
		assert.LessOrEqual(t, sample, limiterCeiling)
		assert.GreaterOrEqual(t, sample, -limiterCeiling)
	}
}

func TestPeakLimiterCarriesLookaheadAcrossBuffers(t *testing.T) {
	l := newPeakLimiter(1, 1000) // 2-frame look-ahead
	first := []float32{0.25, 2}
	l.process(first)
	assert.Equal(t, []float32{0, 0}, first)

	second := []float32{0.5, 0, 0}
	l.process(second)
	assert.Less(t, second[0], float32(0.25), "the attack begins before the delayed peak")
	assert.InDelta(t, limiterCeiling, second[1], 1e-6)
}

func TestPeakLimiterHonorsOverlappingAttackRamps(t *testing.T) {
	l := newPeakLimiter(1, 2000) // 4-frame look-ahead
	samples := []float32{1.225, 0, 1.2405, 0, 0, 0, 0, 0}
	l.process(samples)

	for _, sample := range samples {
		assert.LessOrEqual(t, sample, limiterCeiling+1e-6)
		assert.GreaterOrEqual(t, sample, -limiterCeiling-1e-6)
	}
	assert.InDelta(t, limiterCeiling, samples[4], 1e-6)
	assert.InDelta(t, limiterCeiling, samples[6], 1e-6)
}

func TestPeakLimiterLinksChannels(t *testing.T) {
	l := newPeakLimiter(2, 1000) // 2-frame look-ahead
	samples := []float32{
		0.4, 0.4,
		0.4, 0.4,
		2, 0.5,
		0, 0,
		0, 0,
	}
	l.process(samples)

	assert.InDelta(t, limiterCeiling, samples[8], 1e-6)
	assert.InDelta(t, limiterCeiling/4, samples[9], 1e-6, "both channels must use the same gain")
}

func TestPeakLimiterDoesNotAllocateWhileProcessing(t *testing.T) {
	l := newPeakLimiter(2, 48000)
	samples := make([]float32, 512)
	allocs := testing.AllocsPerRun(100, func() {
		l.process(samples)
	})

	assert.Zero(t, allocs)
}

func BenchmarkPeakLimiterClippedCallback(b *testing.B) {
	l := newPeakLimiter(2, 48000)
	samples := make([]float32, 512) // the desktop sink's 256-frame callback
	for i := range samples {
		samples[i] = 2
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.process(samples)
	}
}
