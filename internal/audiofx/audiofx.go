// Package audiofx provides allocation-free, UI-agnostic audio processors for
// interleaved float32 PCM. Processors own their sample-rate/channel state and
// expose lock-free parameter setters so UI controls can safely update them while
// the audio callback is running.
package audiofx

import (
	"math"
	"sync/atomic"
)

// Processor transforms an interleaved audio buffer in place. Implementations
// must not allocate or block in Process.
type Processor interface {
	Process(samples []float32)
	Reset()
}

// Chain applies a fixed sequence of processors. Build or replace chains off the
// audio thread; processing and parameter changes are callback-safe.
type Chain struct {
	processors []Processor
}

// NewChain returns a chain that applies processors in order.
func NewChain(processors ...Processor) *Chain {
	return &Chain{processors: append([]Processor(nil), processors...)}
}

// Process applies every processor to samples in order.
func (c *Chain) Process(samples []float32) {
	for _, p := range c.processors {
		p.Process(samples)
	}
}

// Reset clears every processor's delay/envelope state.
func (c *Chain) Reset() {
	for _, p := range c.processors {
		p.Reset()
	}
}

// Settings are normalized instrument-effect macro controls. Tone spans -1
// (dark) to +1 (bright); the other values span 0 (dry/off) to 1 (maximum).
type Settings struct {
	Tone   float32
	Comp   float32
	Chorus float32
	Delay  float32
	Reverb float32
}

// Instrument is the stock melodic-instrument chain used by the emulator's
// keyboard bus. The concrete processors remain independently reusable.
type Instrument struct {
	tone   *Tone
	comp   *Compressor
	chorus *Chorus
	delay  *Delay
	reverb *Reverb
	chain  *Chain
}

// NewInstrument builds the stock tone -> compressor -> chorus -> delay ->
// reverb chain for an interleaved stream.
func NewInstrument(channels, rate int) *Instrument {
	channels, rate = validFormat(channels, rate)
	i := &Instrument{
		tone:   NewTone(channels, rate),
		comp:   NewCompressor(channels, rate),
		chorus: NewChorus(channels, rate),
		delay:  NewDelay(channels, rate),
		reverb: NewReverb(channels, rate),
	}
	i.chain = NewChain(i.tone, i.comp, i.chorus, i.delay, i.reverb)
	return i
}

// Set applies all macro settings atomically per parameter.
func (i *Instrument) Set(s Settings) {
	i.tone.SetAmount(clamp(s.Tone, -1, 1))
	i.comp.SetAmount(unit(s.Comp))
	i.chorus.SetAmount(unit(s.Chorus))
	i.delay.SetAmount(unit(s.Delay))
	i.reverb.SetAmount(unit(s.Reverb))
}

// Process applies the instrument chain in place.
func (i *Instrument) Process(samples []float32) { i.chain.Process(samples) }

// Reset clears all effect tails and envelopes.
func (i *Instrument) Reset() { i.chain.Reset() }

type parameter struct{ bits atomic.Uint32 }

func (p *parameter) set(v float32) { p.bits.Store(math.Float32bits(v)) }
func (p *parameter) load() float32 { return math.Float32frombits(p.bits.Load()) }

type smoothParameter struct {
	target  parameter
	current float32
	step    float32
}

func newSmoothParameter(rate int) smoothParameter {
	return smoothParameter{step: float32(1 - math.Exp(-1/(0.020*float64(rate))))}
}

func (p *smoothParameter) set(v float32) { p.target.set(v) }

func (p *smoothParameter) next(target float32) float32 {
	p.current += (target - p.current) * p.step
	if d := target - p.current; d < 1e-5 && d > -1e-5 {
		p.current = target
	}
	return p.current
}

func validFormat(channels, rate int) (int, int) {
	if channels <= 0 {
		channels = 1
	}
	if rate <= 0 {
		rate = 48000
	}
	return channels, rate
}

func unit(v float32) float32 { return clamp(v, 0, 1) }

func clamp(v, low, high float32) float32 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
