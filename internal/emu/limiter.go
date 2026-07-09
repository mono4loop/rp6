package emu

import (
	"math"
	"time"
)

const (
	limiterCeiling   = float32(0.98)
	limiterLookahead = 2 * time.Millisecond
	limiterRelease   = 100 * time.Millisecond
)

// peakLimiter is a stereo-linked look-ahead limiter. It delays the mixed audio
// just long enough to lower the gain smoothly before an over-level peak reaches
// the output, then restores it gradually. Its buffers are allocated up front so
// process is safe to use in the audio callback.
type peakLimiter struct {
	channels int
	delay    []float32
	ceiling  []float32
	pos      int
	control  int

	lookahead int
	gain      float32
	release   float32
}

func newPeakLimiter(channels, rate int) *peakLimiter {
	lookahead := int(float64(rate) * limiterLookahead.Seconds())
	if lookahead < 1 {
		lookahead = 1
	}
	releaseFrames := float64(rate) * limiterRelease.Seconds()
	l := &peakLimiter{
		channels:  channels,
		delay:     make([]float32, lookahead*channels),
		ceiling:   make([]float32, lookahead+1),
		lookahead: lookahead,
		gain:      1,
		release:   float32(1 - math.Exp(-1/releaseFrames)),
	}
	for i := range l.ceiling {
		l.ceiling[i] = 1
	}
	return l
}

func (l *peakLimiter) process(samples []float32) {
	frames := len(samples) / l.channels
	for frame := range frames {
		in := frame * l.channels
		peak := float32(0)
		for ch := range l.channels {
			v := samples[in+ch]
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}

		if peak > limiterCeiling {
			required := limiterCeiling / peak
			end := (l.control + l.lookahead) % len(l.ceiling)
			if required < l.ceiling[end] {
				start := l.gain
				if l.ceiling[l.control] < start {
					start = l.ceiling[l.control]
				}
				if start == required {
					l.ceiling[end] = required
				} else {
					for n := 0; n <= l.lookahead; n++ {
						limit := start + (required-start)*float32(n)/float32(l.lookahead)
						i := (l.control + n) % len(l.ceiling)
						if limit < l.ceiling[i] {
							l.ceiling[i] = limit
						}
					}
				}
			}
		}

		limit := l.ceiling[l.control]
		if l.gain > limit {
			l.gain = limit
		} else {
			l.gain += (1 - l.gain) * l.release
			if l.gain > limit {
				l.gain = limit
			}
		}
		delayed := l.pos * l.channels
		for ch := range l.channels {
			v := l.delay[delayed+ch] * l.gain
			l.delay[delayed+ch] = samples[in+ch]
			if v > 1 {
				v = 1
			} else if v < -1 {
				v = -1
			}
			samples[in+ch] = v
		}
		l.ceiling[l.control] = 1
		l.control++
		if l.control == len(l.ceiling) {
			l.control = 0
		}
		l.pos++
		if l.pos == l.lookahead {
			l.pos = 0
		}
	}
}

func (l *peakLimiter) reset() {
	clear(l.delay)
	for i := range l.ceiling {
		l.ceiling[i] = 1
	}
	l.pos = 0
	l.control = 0
	l.gain = 1
}
