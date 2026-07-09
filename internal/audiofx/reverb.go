package audiofx

type comb struct {
	buffer []float32
	index  int
	filter float32
}

func (c *comb) process(input, feedback, damp float32) float32 {
	output := c.buffer[c.index]
	c.filter = output*(1-damp) + c.filter*damp
	c.buffer[c.index] = input + c.filter*feedback
	c.index++
	if c.index == len(c.buffer) {
		c.index = 0
	}
	return output
}

func (c *comb) reset() {
	clear(c.buffer)
	c.index = 0
	c.filter = 0
}

type allpass struct {
	buffer []float32
	index  int
}

func (a *allpass) process(input float32) float32 {
	buffered := a.buffer[a.index]
	output := buffered - input
	a.buffer[a.index] = input + buffered*0.5
	a.index++
	if a.index == len(a.buffer) {
		a.index = 0
	}
	return output
}

func (a *allpass) reset() {
	clear(a.buffer)
	a.index = 0
}

type reverbChannel struct {
	combs   []comb
	allpass []allpass
}

func newReverbChannel(rate, spread int) reverbChannel {
	combTuning := [...]int{1116, 1188, 1277, 1356}
	allpassTuning := [...]int{556, 441}
	scale := float64(rate) / 44100
	r := reverbChannel{combs: make([]comb, len(combTuning)), allpass: make([]allpass, len(allpassTuning))}
	for i, n := range combTuning {
		frames := max(1, int(float64(n+spread)*scale))
		r.combs[i].buffer = make([]float32, frames)
	}
	for i, n := range allpassTuning {
		frames := max(1, int(float64(n+spread)*scale))
		r.allpass[i].buffer = make([]float32, frames)
	}
	return r
}

func (r *reverbChannel) process(input, feedback, damp float32) float32 {
	var output float32
	for i := range r.combs {
		output += r.combs[i].process(input, feedback, damp)
	}
	output *= 0.25
	for i := range r.allpass {
		output = r.allpass[i].process(output)
	}
	return output
}

func (r *reverbChannel) reset() {
	for i := range r.combs {
		r.combs[i].reset()
	}
	for i := range r.allpass {
		r.allpass[i].reset()
	}
}

// Reverb is a compact stereo Schroeder/Freeverb-style room with damped comb
// filters and decorrelating allpasses.
type Reverb struct {
	channels    int
	left        reverbChannel
	right       reverbChannel
	amount      smoothParameter
	enabled     bool
	quietFrames int
	quietLimit  int
}

// NewReverb returns a fully preallocated room reverb.
func NewReverb(channels, rate int) *Reverb {
	channels, rate = validFormat(channels, rate)
	return &Reverb{
		channels:   channels,
		left:       newReverbChannel(rate, 0),
		right:      newReverbChannel(rate, 23),
		amount:     newSmoothParameter(rate),
		quietLimit: rate,
	}
}

// SetAmount sets reverb wetness and decay from 0 to 1.
func (r *Reverb) SetAmount(amount float32) { r.amount.set(unit(amount)) }

// Process adds the reverb return to the dry signal.
func (r *Reverb) Process(samples []float32) {
	target := r.amount.target.load()
	if target == 0 && r.amount.current == 0 && !r.enabled {
		return
	}
	frames := len(samples) / r.channels
	returnPeak := float32(0)
	for frame := range frames {
		amount := r.amount.next(target)
		if amount != 0 {
			r.enabled = true
		}
		base := frame * r.channels
		input := samples[base]
		if r.channels > 1 {
			input = (input + samples[base+1]) * 0.5
		}
		input *= 0.18 * amount
		left := r.left.process(input, 0.84, 0.22)
		right := r.right.process(input, 0.84, 0.22)
		if peak := max(abs(left), abs(right)); peak > returnPeak {
			returnPeak = peak
		}
		samples[base] += left * 0.42
		if r.channels > 1 {
			samples[base+1] += right * 0.42
		}
		for ch := 2; ch < r.channels; ch++ {
			samples[base+ch] += (left + right) * 0.21
		}
	}
	if target == 0 && r.amount.current == 0 && returnPeak < 1e-5 {
		r.quietFrames += frames
		if r.quietFrames >= r.quietLimit {
			r.Reset()
		}
	} else {
		r.quietFrames = 0
	}
}

// Reset clears the room tail.
func (r *Reverb) Reset() {
	r.left.reset()
	r.right.reset()
	r.enabled = false
	r.quietFrames = 0
}
