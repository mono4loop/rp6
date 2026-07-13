package recorder

import "math"

// peakLimiter is a small stereo-linked look-ahead limiter used on the recorder
// submix before it joins the host output.
type peakLimiter struct {
	channels  int
	delay     []float32
	ceiling   []float32
	pos       int
	control   int
	lookahead int
	gain      float32
	release   float32
}

func newPeakLimiter(channels, rate int) *peakLimiter {
	lookahead := max(1, rate/500) // 2 ms
	l := &peakLimiter{
		channels: channels, delay: make([]float32, lookahead*channels),
		ceiling: make([]float32, lookahead+1), lookahead: lookahead, gain: 1,
		release: float32(1 - math.Exp(-1/(float64(rate)*0.1))),
	}
	for i := range l.ceiling {
		l.ceiling[i] = 1
	}
	return l
}

func (l *peakLimiter) process(samples []float32) {
	const ceiling = float32(0.98)
	for frame := range len(samples) / l.channels {
		base := frame * l.channels
		var peak float32
		for ch := range l.channels {
			v := samples[base+ch]
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		if peak > ceiling {
			required := ceiling / peak
			start := min(l.gain, l.ceiling[l.control])
			for n := 0; n <= l.lookahead; n++ {
				limit := start + (required-start)*float32(n)/float32(l.lookahead)
				i := (l.control + n) % len(l.ceiling)
				if limit < l.ceiling[i] {
					l.ceiling[i] = limit
				}
			}
		}
		limit := l.ceiling[l.control]
		if l.gain > limit {
			l.gain = limit
		} else {
			l.gain += (1 - l.gain) * l.release
		}
		delayed := l.pos * l.channels
		for ch := range l.channels {
			input := samples[base+ch]
			samples[base+ch] = l.delay[delayed+ch] * l.gain
			l.delay[delayed+ch] = input
		}
		l.ceiling[l.control] = 1
		l.control = (l.control + 1) % len(l.ceiling)
		l.pos = (l.pos + 1) % l.lookahead
	}
}

func (l *peakLimiter) reset() {
	clear(l.delay)
	for i := range l.ceiling {
		l.ceiling[i] = 1
	}
	l.pos, l.control, l.gain = 0, 0, 1
}
