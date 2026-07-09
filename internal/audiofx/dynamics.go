package audiofx

import "math"

// Tone is a one-knob tilt control. Negative values blend toward a warm low-pass
// signal; positive values add high-frequency presence.
type Tone struct {
	channels int
	low      []float32
	alpha    float32
	amount   smoothParameter
	enabled  bool
}

// NewTone returns a tone processor centered around a 1.4 kHz split.
func NewTone(channels, rate int) *Tone {
	channels, rate = validFormat(channels, rate)
	cutoff := math.Min(1400, 0.45*float64(rate))
	alpha := 1 - math.Exp(-2*math.Pi*cutoff/float64(rate))
	return &Tone{
		channels: channels, low: make([]float32, channels), alpha: float32(alpha),
		amount: newSmoothParameter(rate),
	}
}

// SetAmount sets the tilt from -1 (dark) through 0 (neutral) to +1 (bright).
func (t *Tone) SetAmount(amount float32) { t.amount.set(clamp(amount, -1, 1)) }

// Process applies the tone tilt in place.
func (t *Tone) Process(samples []float32) {
	target := t.amount.target.load()
	if target == 0 && t.amount.current == 0 && !t.enabled {
		return
	}
	frames := len(samples) / t.channels
	for frame := range frames {
		amount := t.amount.next(target)
		if amount != 0 {
			t.enabled = true
		}
		base := frame * t.channels
		for ch := range t.channels {
			sample := samples[base+ch]
			t.low[ch] += t.alpha * (sample - t.low[ch])
			if amount < 0 {
				samples[base+ch] = sample + (-amount)*0.88*(t.low[ch]-sample)
			} else if amount > 0 {
				samples[base+ch] = sample + amount*0.8*(sample-t.low[ch])
			}
		}
	}
	if target == 0 && t.amount.current == 0 && t.enabled {
		t.Reset()
	}
}

// Reset clears filter history.
func (t *Tone) Reset() {
	clear(t.low)
	t.enabled = false
}

// Compressor is a stereo-linked feed-forward compressor whose Amount macro
// lowers the threshold and raises the ratio together.
type Compressor struct {
	channels int
	envelope float32
	attack   float32
	release  float32
	amount   smoothParameter
	enabled  bool
}

// NewCompressor returns an instrument-friendly compressor with a 10 ms attack
// and 120 ms release.
func NewCompressor(channels, rate int) *Compressor {
	channels, rate = validFormat(channels, rate)
	return &Compressor{
		channels: channels,
		attack:   float32(math.Exp(-1 / (0.010 * float64(rate)))),
		release:  float32(math.Exp(-1 / (0.120 * float64(rate)))),
		amount:   newSmoothParameter(rate),
	}
}

// SetAmount sets compression from 0 (off) to 1 (strong).
func (c *Compressor) SetAmount(amount float32) { c.amount.set(unit(amount)) }

// Process applies linked gain reduction to each frame.
func (c *Compressor) Process(samples []float32) {
	target := c.amount.target.load()
	if target == 0 && c.amount.current == 0 && !c.enabled {
		return
	}
	frames := len(samples) / c.channels
	for frame := range frames {
		amount := c.amount.next(target)
		if amount != 0 {
			c.enabled = true
		}
		base := frame * c.channels
		peak := float32(0)
		for ch := range c.channels {
			v := samples[base+ch]
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		coeff := c.release
		if peak > c.envelope {
			coeff = c.attack
		}
		c.envelope = coeff*c.envelope + (1-coeff)*peak

		gain := float32(1)
		threshold := 1 - 0.8*amount
		if amount > 0 && c.envelope > threshold {
			ratio := 1 + 7*amount
			compressed := threshold + (c.envelope-threshold)/ratio
			gain = compressed / c.envelope
		}
		for ch := range c.channels {
			samples[base+ch] *= gain
		}
	}
	if target == 0 && c.amount.current == 0 && c.enabled {
		c.Reset()
	}
}

// Reset clears the detector envelope.
func (c *Compressor) Reset() {
	c.envelope = 0
	c.enabled = false
}
