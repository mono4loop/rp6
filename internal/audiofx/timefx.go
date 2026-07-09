package audiofx

import "math"

// Chorus is a short modulated-delay chorus with a quarter-cycle stereo offset.
type Chorus struct {
	channels int
	rate     int
	buffer   []float32
	frames   int
	write    int
	phase    float64
	amount   smoothParameter
	enabled  bool
}

// NewChorus returns a chorus with enough preallocated delay for its modulation.
func NewChorus(channels, rate int) *Chorus {
	channels, rate = validFormat(channels, rate)
	frames := int(float64(rate)*0.030) + 2
	return &Chorus{
		channels: channels, rate: rate, buffer: make([]float32, frames*channels), frames: frames,
		amount: newSmoothParameter(rate),
	}
}

// SetAmount sets the chorus wetness/depth from 0 to 1.
func (c *Chorus) SetAmount(amount float32) { c.amount.set(unit(amount)) }

// Process applies chorus in place.
func (c *Chorus) Process(samples []float32) {
	target := c.amount.target.load()
	if target == 0 && c.amount.current == 0 && !c.enabled {
		return
	}
	frames := len(samples) / c.channels
	phaseStep := 2 * math.Pi * 0.32 / float64(c.rate)
	for frame := range frames {
		amount := c.amount.next(target)
		if amount != 0 {
			c.enabled = true
		}
		wet := 0.45 * amount
		base := frame * c.channels
		for ch := range c.channels {
			input := samples[base+ch]
			phase := c.phase + float64(ch)*math.Pi/2
			delay := float64(c.rate) * (0.012 + 0.006*float64(amount)*(0.5+0.5*math.Sin(phase)))
			read := float64(c.write) - delay
			for read < 0 {
				read += float64(c.frames)
			}
			i0 := int(read) % c.frames
			i1 := (i0 + 1) % c.frames
			frac := float32(read - float64(int(read)))
			delayed := c.buffer[i0*c.channels+ch]
			delayed += (c.buffer[i1*c.channels+ch] - delayed) * frac
			c.buffer[c.write*c.channels+ch] = input
			samples[base+ch] = input*(1-wet) + delayed*wet
		}
		c.write++
		if c.write == c.frames {
			c.write = 0
		}
		c.phase += phaseStep
		if c.phase >= 2*math.Pi {
			c.phase -= 2 * math.Pi
		}
	}
	if target == 0 && c.amount.current == 0 && c.enabled {
		c.Reset()
	}
}

// Reset clears the delay and modulation state.
func (c *Chorus) Reset() {
	clear(c.buffer)
	c.write = 0
	c.phase = 0
	c.enabled = false
}

// Delay is a 300 ms feedback delay. Stereo streams cross-feed their repeats to
// create a restrained ping-pong field while mono streams use a normal delay.
type Delay struct {
	channels    int
	buffer      []float32
	frames      int
	write       int
	delayFrames int
	amount      smoothParameter
	enabled     bool
	quietFrames int
	quietLimit  int
}

// NewDelay returns a fully preallocated delay.
func NewDelay(channels, rate int) *Delay {
	channels, rate = validFormat(channels, rate)
	frames := int(float64(rate)*0.7) + 1
	return &Delay{
		channels: channels, buffer: make([]float32, frames*channels), frames: frames,
		delayFrames: int(float64(rate) * 0.30),
		amount:      newSmoothParameter(rate),
		quietLimit:  rate,
	}
}

// SetAmount sets the delay send/feedback from 0 to 1.
func (d *Delay) SetAmount(amount float32) { d.amount.set(unit(amount)) }

// Process applies delay in place.
func (d *Delay) Process(samples []float32) {
	target := d.amount.target.load()
	if target == 0 && d.amount.current == 0 && !d.enabled {
		return
	}
	read := d.write - d.delayFrames
	if read < 0 {
		read += d.frames
	}
	frames := len(samples) / d.channels
	returnPeak := float32(0)
	for frame := range frames {
		amount := d.amount.next(target)
		if amount != 0 {
			d.enabled = true
		}
		base := frame * d.channels
		readBase := read * d.channels
		writeBase := d.write * d.channels
		for ch := range d.channels {
			delayed := d.buffer[readBase+ch]
			if peak := abs(delayed); peak > returnPeak {
				returnPeak = peak
			}
			feedbackCh := ch
			if d.channels == 2 {
				feedbackCh = 1 - ch
			}
			d.buffer[writeBase+ch] = samples[base+ch]*amount + d.buffer[readBase+feedbackCh]*0.42
			samples[base+ch] += delayed * 0.42
		}
		d.write++
		read++
		if d.write == d.frames {
			d.write = 0
		}
		if read == d.frames {
			read = 0
		}
	}
	if target == 0 && d.amount.current == 0 && returnPeak < 1e-5 {
		d.quietFrames += frames
		if d.quietFrames >= d.quietLimit {
			d.Reset()
		}
	} else {
		d.quietFrames = 0
	}
}

// Reset clears all repeats.
func (d *Delay) Reset() {
	clear(d.buffer)
	d.write = 0
	d.enabled = false
	d.quietFrames = 0
}
