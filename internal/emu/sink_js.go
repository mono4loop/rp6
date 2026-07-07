//go:build js

package emu

import (
	"syscall/js"
	"unsafe"
)

// openSink returns the best available browser audio sink:
//   - an AudioWorklet sink (glitch-free, audio-thread playback) when the page is
//     cross-origin isolated so SharedArrayBuffer is available;
//   - otherwise a ScriptProcessorNode sink (works anywhere, but its callback
//     runs on the main thread so it can glitch under UI load);
//   - a silent sink if there's no Web Audio at all.
func openSink() (sink, error) {
	ctor := js.Global().Get("AudioContext")
	if !ctor.Truthy() {
		ctor = js.Global().Get("webkitAudioContext")
	}
	if !ctor.Truthy() {
		return jsSilentSink{}, nil
	}
	ctx := ctor.New()
	rate := ctx.Get("sampleRate").Int()
	if canUseWorklet(ctx) {
		return &workletSink{ctx: ctx, rate: rate, channels: 2}, nil
	}
	return &scriptSink{ctx: ctx, rate: rate, channels: 2}, nil
}

// canUseWorklet reports whether the AudioWorklet + SharedArrayBuffer path is
// available (needs a cross-origin-isolated page: COOP/COEP headers).
func canUseWorklet(ctx js.Value) bool {
	return js.Global().Get("SharedArrayBuffer").Truthy() &&
		js.Global().Get("crossOriginIsolated").Truthy() &&
		ctx.Get("audioWorklet").Truthy()
}

// resumeOnGesture resumes a (suspended) AudioContext on the first user gesture,
// as required by browser autoplay policy. Returns a cleanup that removes the
// listeners and releases the callback.
func resumeOnGesture(ctx js.Value) func() {
	fn := js.FuncOf(func(js.Value, []js.Value) any { ctx.Call("resume"); return nil })
	doc := js.Global().Get("document")
	events := []string{"pointerdown", "keydown", "touchstart", "mousedown"}
	if doc.Truthy() {
		for _, ev := range events {
			doc.Call("addEventListener", ev, fn)
		}
	}
	ctx.Call("resume")
	return func() {
		if doc.Truthy() {
			for _, ev := range events {
				doc.Call("removeEventListener", ev, fn)
			}
		}
		fn.Release()
	}
}

// float32Bytes reinterprets a float32 slice as little-endian bytes (wasm and
// Web Audio are both little-endian) without copying.
func float32Bytes(f []float32) []byte {
	if len(f) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&f[0])), len(f)*4)
}

// --- AudioWorklet sink (SharedArrayBuffer ring buffer) ----------------------

// Ring geometry. The control area is a 4-int32 header at the start of the SAB;
// ctrl[0] is the number of buffered frames (fill), updated atomically by both
// sides (producer adds, consumer subtracts). Audio data follows, interleaved.
const (
	ctrlInts    = 4
	ctrlBytes   = ctrlInts * 4
	ringFrames  = 8192 // ~170 ms at 48 kHz — ample slack for main-thread stalls
	chunkFrames = 512  // frames the producer tops up per step
)

// workletProcessorJS is the AudioWorkletProcessor: it drains the ring into the
// output on the audio thread, outputting silence on underrun. Loaded via a Blob
// URL so no separate file needs serving.
const workletProcessorJS = `
class RP6Processor extends AudioWorkletProcessor {
  constructor(options) {
    super();
    const o = options.processorOptions;
    this.ctrl = new Int32Array(o.sab, 0, 4);
    this.ring = new Float32Array(o.sab, o.dataOffset);
    this.ringFrames = o.ringFrames;
    this.channels = o.channels;
    this.readPos = 0;
  }
  process(inputs, outputs) {
    const out = outputs[0];
    if (!out || out.length === 0) return true;
    const ch = this.channels, frames = out[0].length;
    const avail = Atomics.load(this.ctrl, 0);
    const toRead = Math.min(avail, frames);
    for (let i = 0; i < frames; i++) {
      if (i < toRead) {
        const base = ((this.readPos + i) % this.ringFrames) * ch;
        for (let c = 0; c < ch; c++) out[c][i] = this.ring[base + c];
      } else {
        for (let c = 0; c < ch; c++) out[c][i] = 0;
      }
    }
    if (toRead > 0) {
      this.readPos = (this.readPos + toRead) % this.ringFrames;
      Atomics.add(this.ctrl, 0, -toRead);
    }
    return true;
  }
}
registerProcessor('rp6-processor', RP6Processor);
`

type workletSink struct {
	ctx      js.Value
	rate     int
	channels int
	render   func([]float32)

	sab     js.Value
	ctrl    js.Value // Int32Array over the control header
	sabU8   js.Value // Uint8Array over the whole SAB (for CopyBytesToJS)
	atomics js.Value
	node    js.Value

	writePos int
	scratch  []float32

	stop      chan struct{}
	cleanupFn func() // resume-listener cleanup
	onModule  js.Func
	timer     js.Value // setInterval handle for the producer
	cb        js.Func  // producer interval callback
}

func (s *workletSink) SampleRate() int { return s.rate }
func (s *workletSink) Channels() int   { return s.channels }
func (s *workletSink) Name() string    { return "Web Audio (AudioWorklet)" }

func (s *workletSink) Start(render func(out []float32)) error {
	s.render = render
	s.atomics = js.Global().Get("Atomics")

	total := ctrlBytes + ringFrames*s.channels*4
	s.sab = js.Global().Get("SharedArrayBuffer").New(total)
	s.ctrl = js.Global().Get("Int32Array").New(s.sab, 0, ctrlInts)
	s.sabU8 = js.Global().Get("Uint8Array").New(s.sab)
	s.scratch = make([]float32, chunkFrames*s.channels)

	// Load the processor module (from a Blob URL) then create + connect the node.
	blob := js.Global().Get("Blob").New(
		[]any{workletProcessorJS},
		map[string]any{"type": "text/javascript"},
	)
	url := js.Global().Get("URL").Call("createObjectURL", blob)
	s.onModule = js.FuncOf(func(js.Value, []js.Value) any {
		opts := map[string]any{
			"outputChannelCount": []any{s.channels},
			"processorOptions": map[string]any{
				"sab":        s.sab,
				"dataOffset": ctrlBytes,
				"ringFrames": ringFrames,
				"channels":   s.channels,
			},
		}
		s.node = js.Global().Get("AudioWorkletNode").New(s.ctx, "rp6-processor", opts)
		s.node.Call("connect", s.ctx.Get("destination"))
		js.Global().Get("URL").Call("revokeObjectURL", url)
		return nil
	})
	s.ctx.Get("audioWorklet").Call("addModule", url).Call("then", s.onModule)

	s.cleanupFn = resumeOnGesture(s.ctx)

	// Producer: keep the ring topped up. It renders ahead into the shared buffer,
	// so occasional main-thread stalls don't starve the audio-thread consumer.
	s.stop = make(chan struct{})
	s.startProducer()
	return nil
}

// startProducer schedules a JS interval that tops up the ring from Go. A short
// period suffices; the ring's ~170 ms depth absorbs jitter in the cadence.
func (s *workletSink) startProducer() {
	setInterval := js.Global().Get("setInterval")
	if !setInterval.Truthy() {
		return
	}
	s.cb = js.FuncOf(func(js.Value, []js.Value) any {
		select {
		case <-s.stop:
			js.Global().Get("clearInterval").Invoke(s.timer)
			s.cb.Release()
		default:
			s.topUp()
		}
		return nil
	})
	s.timer = setInterval.Invoke(s.cb, 5)
}

func (s *workletSink) topUp() {
	if !s.node.Truthy() || s.render == nil {
		return
	}
	for {
		fill := s.atomics.Call("load", s.ctrl, 0).Int()
		if ringFrames-fill < chunkFrames {
			return
		}
		for i := range s.scratch {
			s.scratch[i] = 0
		}
		s.render(s.scratch)
		s.writeChunk()
		s.writePos = (s.writePos + chunkFrames) % ringFrames
		s.atomics.Call("add", s.ctrl, 0, chunkFrames)
	}
}

// writeChunk copies s.scratch (chunkFrames interleaved) into the ring at
// writePos, wrapping around the end.
func (s *workletSink) writeChunk() {
	ch := s.channels
	firstFrames := ringFrames - s.writePos
	if firstFrames > chunkFrames {
		firstFrames = chunkFrames
	}
	b := float32Bytes(s.scratch)
	dataOff := ctrlBytes
	// First segment at writePos.
	dstOff := dataOff + s.writePos*ch*4
	seg1 := b[:firstFrames*ch*4]
	js.CopyBytesToJS(s.sabU8.Call("subarray", dstOff, dstOff+len(seg1)), seg1)
	// Wrapped segment at ring start.
	if firstFrames < chunkFrames {
		seg2 := b[firstFrames*ch*4:]
		js.CopyBytesToJS(s.sabU8.Call("subarray", dataOff, dataOff+len(seg2)), seg2)
	}
}

// timer/cb are stored so the interval can be cleared and the callback released.
func (s *workletSink) Stop() error {
	if s.stop != nil {
		select {
		case <-s.stop:
		default:
			close(s.stop)
		}
	}
	if s.node.Truthy() {
		s.node.Call("disconnect")
	}
	return nil
}

func (s *workletSink) Close() error {
	_ = s.Stop()
	if s.cleanupFn != nil {
		s.cleanupFn()
	}
	if s.onModule.Truthy() {
		s.onModule.Release()
	}
	if s.ctx.Truthy() {
		s.ctx.Call("close")
	}
	return nil
}

// --- ScriptProcessorNode sink (fallback; main-thread callback) --------------

const scriptBufferFrames = 2048

type scriptSink struct {
	ctx       js.Value
	rate      int
	channels  int
	render    func([]float32)
	node      js.Value
	uint8     js.Value
	onProc    js.Func
	cleanupFn func()
	inter     []float32
	chbuf     []float32
}

func (s *scriptSink) SampleRate() int { return s.rate }
func (s *scriptSink) Channels() int   { return s.channels }
func (s *scriptSink) Name() string    { return "Web Audio (ScriptProcessor)" }

func (s *scriptSink) Start(render func(out []float32)) error {
	s.render = render
	s.uint8 = js.Global().Get("Uint8Array")
	s.onProc = js.FuncOf(s.process)
	s.node = s.ctx.Call("createScriptProcessor", scriptBufferFrames, 0, s.channels)
	s.node.Set("onaudioprocess", s.onProc)
	s.node.Call("connect", s.ctx.Get("destination"))
	s.cleanupFn = resumeOnGesture(s.ctx)
	return nil
}

func (s *scriptSink) process(_ js.Value, args []js.Value) any {
	if s.render == nil || len(args) == 0 {
		return nil
	}
	out := args[0].Get("outputBuffer")
	n := out.Get("length").Int()
	if n <= 0 {
		return nil
	}
	ch := s.channels
	if cap(s.inter) < n*ch {
		s.inter = make([]float32, n*ch)
	}
	frame := s.inter[:n*ch]
	for i := range frame {
		frame[i] = 0
	}
	s.render(frame)
	if cap(s.chbuf) < n {
		s.chbuf = make([]float32, n)
	}
	cb := s.chbuf[:n]
	for c := 0; c < ch; c++ {
		for i := 0; i < n; i++ {
			cb[i] = frame[i*ch+c]
		}
		fa := out.Call("getChannelData", c)
		js.CopyBytesToJS(s.uint8.New(fa.Get("buffer")), float32Bytes(cb))
	}
	return nil
}

func (s *scriptSink) Stop() error {
	if s.node.Truthy() {
		s.node.Call("disconnect")
	}
	return nil
}

func (s *scriptSink) Close() error {
	_ = s.Stop()
	if s.cleanupFn != nil {
		s.cleanupFn()
	}
	if s.node.Truthy() {
		s.node.Set("onaudioprocess", js.Null())
	}
	if s.ctx.Truthy() {
		s.ctx.Call("close")
	}
	if s.onProc.Truthy() {
		s.onProc.Release()
	}
	return nil
}

// --- silent fallback --------------------------------------------------------

type jsSilentSink struct{}

func (jsSilentSink) Start(func(out []float32)) error { return nil }
func (jsSilentSink) Stop() error                     { return nil }
func (jsSilentSink) SampleRate() int                 { return 48000 }
func (jsSilentSink) Channels() int                   { return 2 }
func (jsSilentSink) Name() string                    { return "silent (no Web Audio)" }
func (jsSilentSink) Close() error                    { return nil }
