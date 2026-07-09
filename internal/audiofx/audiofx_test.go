package audiofx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type addProcessor float32

func (a addProcessor) Process(samples []float32) {
	for i := range samples {
		samples[i] += float32(a)
	}
}
func (addProcessor) Reset() {}

type multiplyProcessor float32

func (m multiplyProcessor) Process(samples []float32) {
	for i := range samples {
		samples[i] *= float32(m)
	}
}
func (multiplyProcessor) Reset() {}

func TestChainProcessesInOrder(t *testing.T) {
	chain := NewChain(addProcessor(1), multiplyProcessor(2))
	samples := []float32{1, -1}
	chain.Process(samples)
	assert.Equal(t, []float32{4, 0}, samples)
}

func TestInstrumentDryIsTransparent(t *testing.T) {
	fx := NewInstrument(2, 48000)
	samples := []float32{0.25, -0.25, 0.5, -0.5}
	want := append([]float32(nil), samples...)
	fx.Process(samples)
	assert.Equal(t, want, samples)
}

func TestInstrumentEffectsProduceTail(t *testing.T) {
	const rate = 1000
	fx := NewInstrument(2, rate)
	fx.Set(Settings{Delay: 1, Reverb: 1})
	block := make([]float32, rate*2)
	block[0], block[1] = 1, 1
	fx.Process(block)
	assert.NotZero(t, block[600], "300 ms delay produces a left-channel repeat")

	clear(block)
	fx.Process(block)
	assert.NotZero(t, block[0], "effect tail continues into the next callback")
}

func TestEnablingEffectsDoesNotRevealBypassedHistory(t *testing.T) {
	fx := NewInstrument(2, 1000)
	samples := make([]float32, 800)
	samples[0], samples[1] = 1, 1
	fx.Process(samples) // all macros are at zero

	fx.Set(Settings{Delay: 1, Reverb: 1})
	for range 4 { // allow the 20 ms parameter ramp to settle before checking
		clear(samples)
		fx.Process(samples)
	}
	for i, sample := range samples {
		assert.Zero(t, sample, i)
	}
}

func TestDelayTailDecaysAfterAmountReturnsToZero(t *testing.T) {
	delay := NewDelay(1, 1000)
	delay.SetAmount(1)
	first := make([]float32, 400)
	first[0] = 1
	delay.Process(first)
	require.NotZero(t, first[300], "first repeat is audible")

	delay.SetAmount(0)
	second := make([]float32, 400)
	delay.Process(second)
	assert.NotZero(t, second[200], "existing feedback tail rings out after send reaches zero")
}

func TestTimeEffectsSleepAfterTailBecomesSilent(t *testing.T) {
	const rate = 1000
	delay := NewDelay(1, rate)
	reverb := NewReverb(1, rate)
	delay.SetAmount(1)
	reverb.SetAmount(1)
	samples := make([]float32, 100)
	samples[0] = 1
	delay.Process(samples)
	reverb.Process(samples)
	delay.SetAmount(0)
	reverb.SetAmount(0)

	for range 200 { // ample time for both tails to decay, then one quiet second
		clear(samples)
		delay.Process(samples)
		reverb.Process(samples)
	}
	assert.False(t, delay.enabled)
	assert.False(t, reverb.enabled)
}

func TestCompressorLinksChannels(t *testing.T) {
	c := NewCompressor(2, 1000)
	c.SetAmount(1)
	samples := make([]float32, 400)
	for i := 0; i < len(samples); i += 2 {
		samples[i], samples[i+1] = 1, 0.25
	}
	c.Process(samples)

	left, right := samples[len(samples)-2], samples[len(samples)-1]
	assert.Less(t, left, float32(1))
	assert.InDelta(t, 4, left/right, 1e-5, "linked compression preserves the stereo balance")
}

func TestInstrumentResetClearsTails(t *testing.T) {
	fx := NewInstrument(2, 1000)
	fx.Set(Settings{Delay: 1, Reverb: 1})
	samples := make([]float32, 800)
	samples[0], samples[1] = 1, 1
	fx.Process(samples)
	fx.Reset()
	clear(samples)
	fx.Process(samples)
	for _, sample := range samples {
		assert.Zero(t, sample)
	}
}

func TestInstrumentProcessDoesNotAllocate(t *testing.T) {
	fx := NewInstrument(2, 48000)
	fx.Set(Settings{Tone: 0.3, Comp: 0.5, Chorus: 0.4, Delay: 0.2, Reverb: 0.3})
	samples := make([]float32, 512)
	allocs := testing.AllocsPerRun(100, func() { fx.Process(samples) })
	assert.Zero(t, allocs)
}
