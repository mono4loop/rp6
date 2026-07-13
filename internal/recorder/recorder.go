// Package recorder implements a host-side multitrack audio recorder and clip
// player. It is independent of Fyne, MIDI, and any particular audio backend.
package recorder

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mono4loop/rp6/internal/audiofx"
)

const (
	// TrackCount is the fixed number of recorder tracks exposed by RP6.
	TrackCount = 8
	// MaxRecordSeconds bounds an armed recording without allocating on the audio
	// callback. The P-6's longest mono sample is shorter than this.
	MaxRecordSeconds = 30
	maxBlockFrames   = 8192
)

var (
	ErrTrack     = errors.New("recorder: invalid track")
	ErrFormat    = errors.New("recorder: clip format does not match engine")
	ErrRecording = errors.New("recorder: another track is recording")
)

// Quantization controls when record and playback actions take effect.
type Quantization int

const (
	QuantizeOff Quantization = iota
	QuantizeBeat
	QuantizeBar
)

// Clip is immutable interleaved PCM audio in the engine's format.
type Clip struct {
	Samples    []float32
	Channels   int
	SampleRate int
}

type clipRef struct{ clip Clip }
type changeFunc struct{ fn func(int) }

type recording struct {
	track   int
	samples []float32
	pos     atomic.Int64
	active  atomic.Bool
	writers atomic.Int32
}

type track struct {
	clip atomic.Pointer[clipRef]

	playing   atomic.Bool
	muted     atomic.Bool
	solo      atomic.Bool
	loop      atomic.Bool
	truncated atomic.Bool
	level     atomic.Uint32
	pan       atomic.Uint32

	startAt atomic.Int64 // target frame + 1; zero means no pending start
	stopAt  atomic.Int64 // target frame + 1; zero means no pending stop
	pos     int

	mu       sync.Mutex
	name     string
	settings audiofx.Settings
	fx       *audiofx.Instrument
	scratch  []float32
}

// Engine captures one input stream and mixes up to eight clips into an output
// stream. Capture and Mix may be called concurrently by separate audio devices.
type Engine struct {
	channels     atomic.Int32
	rate         atomic.Int32
	bpmBits      atomic.Uint64
	quant        atomic.Int32
	clock        atomic.Int64
	captureClock atomic.Int64

	tracks [TrackCount]track
	mixMu  sync.Mutex
	mixBuf []float32
	limit  *peakLimiter

	armedTrack atomic.Int32 // track + 1; zero means none
	recording  atomic.Pointer[recording]
	recordOn   atomic.Int64 // target frame + 1
	recordOff  atomic.Int64 // target frame + 1

	onChange atomic.Pointer[changeFunc]
}

// New returns an empty eight-track engine in the given PCM format.
func New(channels, sampleRate int) *Engine {
	e := &Engine{}
	e.armedTrack.Store(0)
	e.SetTempo(120)
	e.setFormat(channels, sampleRate)
	for i := range TrackCount {
		e.tracks[i].name = "Track " + string(rune('1'+i))
		e.tracks[i].loop.Store(true)
		e.tracks[i].level.Store(math.Float32bits(1))
		e.tracks[i].pan.Store(math.Float32bits(0))
	}
	return e
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

func (e *Engine) setFormat(channels, rate int) {
	channels, rate = validFormat(channels, rate)
	e.channels.Store(int32(channels))
	e.rate.Store(int32(rate))
	e.mixBuf = make([]float32, maxBlockFrames*channels)
	e.limit = newPeakLimiter(channels, rate)
	for i := range TrackCount {
		t := &e.tracks[i]
		t.fx = audiofx.NewInstrument(channels, rate)
		t.scratch = make([]float32, maxBlockFrames*channels)
	}
}

// SetFormat changes the engine format and clears audio clips. Call only while
// its capture/output backends are detached, such as during a profile switch.
func (e *Engine) SetFormat(channels, sampleRate int) {
	e.mixMu.Lock()
	defer e.mixMu.Unlock()
	channels, sampleRate = validFormat(channels, sampleRate)
	if e.Channels() == channels && e.SampleRate() == sampleRate {
		return
	}
	e.clearLocked()
	e.setFormat(channels, sampleRate)
}

func (e *Engine) Channels() int   { return int(e.channels.Load()) }
func (e *Engine) SampleRate() int { return int(e.rate.Load()) }

// SetOnChange installs a callback for asynchronous audio-thread state changes.
// The callback must marshal UI work to the UI thread.
func (e *Engine) SetOnChange(fn func(track int)) {
	if fn == nil {
		e.onChange.Store(nil)
		return
	}
	e.onChange.Store(&changeFunc{fn: fn})
}

func (e *Engine) changed(track int) {
	if cb := e.onChange.Load(); cb != nil {
		cb.fn(track)
	}
}

func (e *Engine) validTrack(track int) bool { return track >= 0 && track < TrackCount }

// SetTempo sets the musical clock tempo used by quantized actions.
func (e *Engine) SetTempo(bpm float64) {
	if bpm < 1 {
		bpm = 120
	}
	e.bpmBits.Store(math.Float64bits(bpm))
}

func (e *Engine) Tempo() float64 { return math.Float64frombits(e.bpmBits.Load()) }

func (e *Engine) SetQuantization(q Quantization) {
	if q < QuantizeOff || q > QuantizeBar {
		q = QuantizeOff
	}
	e.quant.Store(int32(q))
}

func (e *Engine) Quantization() Quantization { return Quantization(e.quant.Load()) }

func (e *Engine) quantizedFrame(now int64) int64 {
	q := e.Quantization()
	if q == QuantizeOff {
		return now
	}
	frames := int64(math.Round(float64(e.SampleRate()) * 60 / e.Tempo()))
	if q == QuantizeBar {
		frames *= 4
	}
	if frames < 1 {
		return now
	}
	return ((now + frames - 1) / frames) * frames
}

func (e *Engine) actionFrame() int64 { return e.quantizedFrame(e.clock.Load()) }

func (e *Engine) recordActionFrame() int64 { return e.quantizedFrame(e.captureClock.Load()) }

// Play queues a track to start from its beginning at the selected quantization.
func (e *Engine) Play(track int) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	if e.tracks[track].clip.Load() == nil {
		return nil
	}
	e.queuePlay(track, e.actionFrame())
	return nil
}

func (e *Engine) queuePlay(track int, frame int64) {
	t := &e.tracks[track]
	t.stopAt.Store(0)
	t.startAt.Store(frame + 1)
	e.changed(track)
}

// PlayAll starts every non-empty track on the same exact output frame.
func (e *Engine) PlayAll() {
	frame := e.actionFrame()
	for i := range TrackCount {
		if e.tracks[i].clip.Load() != nil {
			e.queuePlay(i, frame)
		}
	}
}

// Stop queues a track to stop at the selected quantization.
func (e *Engine) Stop(track int) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	e.queueStop(track, e.actionFrame())
	return nil
}

func (e *Engine) queueStop(track int, frame int64) {
	t := &e.tracks[track]
	t.startAt.Store(0)
	t.stopAt.Store(frame + 1)
	e.changed(track)
}

// StopAll queues every track to stop on the same exact output frame.
func (e *Engine) StopAll() {
	frame := e.actionFrame()
	for i := range TrackCount {
		e.queueStop(i, frame)
	}
}

// StopAllImmediate stops playback without waiting for quantization.
func (e *Engine) StopAllImmediate() {
	e.mixMu.Lock()
	defer e.mixMu.Unlock()
	for i := range TrackCount {
		t := &e.tracks[i]
		t.startAt.Store(0)
		t.stopAt.Store(0)
		t.playing.Store(false)
		t.pos = 0
		t.fx.Reset()
	}
	e.limit.reset()
}

func (e *Engine) Playing(track int) bool {
	return e.validTrack(track) && e.tracks[track].playing.Load()
}

func (e *Engine) PlayPending(track int) bool {
	return e.validTrack(track) && e.tracks[track].startAt.Load() != 0
}

func (e *Engine) StopPending(track int) bool {
	return e.validTrack(track) && e.tracks[track].stopAt.Load() != 0
}

func (e *Engine) SetMuted(track int, muted bool) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	e.tracks[track].muted.Store(muted)
	return nil
}

func (e *Engine) Muted(track int) bool { return e.validTrack(track) && e.tracks[track].muted.Load() }

func (e *Engine) SetSolo(track int, solo bool) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	e.tracks[track].solo.Store(solo)
	return nil
}

func (e *Engine) Solo(track int) bool { return e.validTrack(track) && e.tracks[track].solo.Load() }

func (e *Engine) SetLoop(track int, loop bool) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	e.tracks[track].loop.Store(loop)
	return nil
}

func (e *Engine) Loop(track int) bool { return e.validTrack(track) && e.tracks[track].loop.Load() }

func clamp(v, low, high float32) float32 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func (e *Engine) SetLevel(track int, level float32) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	e.tracks[track].level.Store(math.Float32bits(clamp(level, 0, 1)))
	return nil
}

func (e *Engine) Level(track int) float32 {
	if !e.validTrack(track) {
		return 0
	}
	return math.Float32frombits(e.tracks[track].level.Load())
}

func (e *Engine) SetPan(track int, pan float32) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	e.tracks[track].pan.Store(math.Float32bits(clamp(pan, -1, 1)))
	return nil
}

func (e *Engine) Pan(track int) float32 {
	if !e.validTrack(track) {
		return 0
	}
	return math.Float32frombits(e.tracks[track].pan.Load())
}

func (e *Engine) SetName(track int, name string) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	t := &e.tracks[track]
	t.mu.Lock()
	t.name = name
	t.mu.Unlock()
	return nil
}

func (e *Engine) Name(track int) string {
	if !e.validTrack(track) {
		return ""
	}
	t := &e.tracks[track]
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.name
}

func (e *Engine) SetEffects(track int, settings audiofx.Settings) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	t := &e.tracks[track]
	t.mu.Lock()
	t.settings = settings
	t.mu.Unlock()
	t.fx.Set(settings)
	return nil
}

func (e *Engine) Effects(track int) audiofx.Settings {
	if !e.validTrack(track) {
		return audiofx.Settings{}
	}
	t := &e.tracks[track]
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.settings
}

// SetClip installs a copy of clip on track.
func (e *Engine) SetClip(track int, clip Clip) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	if clip.Channels != e.Channels() || clip.SampleRate != e.SampleRate() {
		return ErrFormat
	}
	samples := append([]float32(nil), clip.Samples...)
	samples = samples[:len(samples)-len(samples)%clip.Channels]
	e.setClipRef(track, Clip{Samples: samples, Channels: clip.Channels, SampleRate: clip.SampleRate})
	return nil
}

func (e *Engine) setClipRef(track int, clip Clip) {
	t := &e.tracks[track]
	if len(clip.Samples) == 0 {
		t.clip.Store(nil)
	} else {
		t.clip.Store(&clipRef{clip: clip})
	}
	t.startAt.Store(0)
	t.stopAt.Store(0)
	t.playing.Store(false)
	e.changed(track)
}

// Clip returns a copy of a track's immutable clip.
func (e *Engine) Clip(track int) (Clip, bool) {
	if !e.validTrack(track) {
		return Clip{}, false
	}
	ref := e.tracks[track].clip.Load()
	if ref == nil {
		return Clip{}, false
	}
	clip := ref.clip
	clip.Samples = append([]float32(nil), clip.Samples...)
	return clip, true
}

func (e *Engine) HasClip(track int) bool {
	return e.validTrack(track) && e.tracks[track].clip.Load() != nil
}

// Truncated reports whether the last take hit MaxRecordSeconds.
func (e *Engine) Truncated(track int) bool {
	return e.validTrack(track) && e.tracks[track].truncated.Load()
}

func (e *Engine) DurationFrames(track int) int {
	if !e.validTrack(track) {
		return 0
	}
	ref := e.tracks[track].clip.Load()
	if ref == nil {
		return 0
	}
	return len(ref.clip.Samples) / ref.clip.Channels
}

// ArmRecord makes track wait for the next live RP6 pad press.
func (e *Engine) ArmRecord(track int) error {
	if !e.validTrack(track) {
		return ErrTrack
	}
	if r := e.recording.Load(); r != nil && (r.active.Load() || e.recordOn.Load() != 0) {
		return ErrRecording
	}
	e.CancelRecording()
	channels, rate := e.Channels(), e.SampleRate()
	e.recording.Store(&recording{track: track, samples: make([]float32, MaxRecordSeconds*rate*channels)})
	e.tracks[track].truncated.Store(false)
	e.armedTrack.Store(int32(track + 1))
	e.changed(track)
	return nil
}

func (e *Engine) ArmedTrack() int { return int(e.armedTrack.Load()) - 1 }

// TriggerRecord starts an armed recording, optionally at the next beat/bar.
// It returns true when an armed track consumed the trigger.
func (e *Engine) TriggerRecord() bool {
	track := int(e.armedTrack.Swap(0)) - 1
	if !e.validTrack(track) {
		return false
	}
	r := e.recording.Load()
	if r == nil || r.track != track || r.active.Load() || e.recordOn.Load() != 0 {
		return false
	}
	e.recordOff.Store(0)
	e.recordOn.Store(e.recordActionFrame() + 1)
	e.changed(track)
	return true
}

// StopRecording requests a quantized recording stop. A recording that has not
// reached its start boundary yet is cancelled.
func (e *Engine) StopRecording() bool {
	r := e.recording.Load()
	if r == nil {
		return false
	}
	if !r.active.Load() {
		e.CancelRecording()
		return true
	}
	e.recordOff.Store(e.recordActionFrame() + 1)
	e.changed(r.track)
	return true
}

// CancelRecording discards the armed/pending/current take.
func (e *Engine) CancelRecording() {
	armed := int(e.armedTrack.Swap(0)) - 1
	e.recordOn.Store(0)
	e.recordOff.Store(0)
	if r := e.recording.Swap(nil); r != nil {
		r.active.Store(false)
		waitWriters(r)
		e.changed(r.track)
	}
	if e.validTrack(armed) {
		e.changed(armed)
	}
}

// StopRecordingImmediate finishes the current take without quantization. It is
// used before profile switches and shutdown so autosave sees the final clip.
func (e *Engine) StopRecordingImmediate() bool {
	e.armedTrack.Store(0)
	e.recordOn.Store(0)
	e.recordOff.Store(0)
	r := e.recording.Swap(nil)
	if r == nil {
		return false
	}
	r.active.Store(false)
	waitWriters(r)
	if r.pos.Load() == 0 {
		e.changed(r.track)
		return false
	}
	e.finalize(r)
	return true
}

func waitWriters(r *recording) {
	for r.writers.Load() != 0 {
		time.Sleep(50 * time.Microsecond)
	}
}

func (e *Engine) RecordingTrack() int {
	r := e.recording.Load()
	if r == nil || !r.active.Load() {
		return -1
	}
	return r.track
}

func (e *Engine) RecordPendingTrack() int {
	r := e.recording.Load()
	if r == nil || r.active.Load() || e.recordOn.Load() == 0 {
		return -1
	}
	return r.track
}

func (e *Engine) RecordStopPending() bool { return e.recordOff.Load() != 0 }

// Capture consumes input PCM synchronously. It performs no allocation and is
// safe to call from a capture callback while Mix runs on another audio thread.
func (e *Engine) Capture(samples []float32) {
	channels := e.Channels()
	frames := len(samples) / channels
	if frames == 0 {
		return
	}
	blockStart := e.captureClock.Add(int64(frames)) - int64(frames)
	r := e.recording.Load()
	if r == nil {
		return
	}
	startOff, starts := commandOffset(&e.recordOn, blockStart, frames)
	stopOff, stops := commandOffset(&e.recordOff, blockStart, frames)
	if starts {
		r.active.Store(true)
		e.changed(r.track)
	}
	from := 0
	if starts {
		from = startOff
	}
	to := frames
	if stops {
		to = stopOff
	}
	if !r.active.Load() || from >= to {
		if stops {
			e.finishFromCapture(r)
		}
		return
	}
	r.writers.Add(1)
	defer r.writers.Add(-1)
	if !r.active.Load() || e.recording.Load() != r {
		return
	}
	pos := int(r.pos.Load())
	source := samples[from*channels : to*channels]
	n := min(len(source), len(r.samples)-pos)
	if n > 0 {
		copy(r.samples[pos:pos+n], source[:n])
		r.pos.Add(int64(n))
	}
	if pos+n == len(r.samples) {
		e.tracks[r.track].truncated.Store(true)
		e.finishFromCapture(r)
		return
	}
	if stops {
		e.finishFromCapture(r)
	}
}

func (e *Engine) finishFromCapture(r *recording) {
	if !e.recording.CompareAndSwap(r, nil) {
		return
	}
	r.active.Store(false)
	// This callback is the sole capture writer, so its own write is complete and
	// no new writer can enter after the recording pointer was cleared.
	e.finalize(r)
}

func (e *Engine) finalize(r *recording) {
	n := int(r.pos.Load())
	n -= n % e.Channels()
	samples := r.samples[:n]
	e.setClipRef(r.track, Clip{Samples: samples, Channels: e.Channels(), SampleRate: e.SampleRate()})
}

// Mix adds recorder playback to out and advances the musical frame clock. It
// does not clear out, allowing an emulator to mix its own voices first.
func (e *Engine) Mix(out []float32) {
	channels := e.Channels()
	if channels <= 0 || len(out) < channels {
		return
	}
	e.mixMu.Lock()
	defer e.mixMu.Unlock()
	for len(out) >= channels {
		n := min(len(out), len(e.mixBuf))
		n -= n % channels
		e.mixBlock(out[:n])
		out = out[n:]
	}
}

func (e *Engine) mixBlock(out []float32) {
	clear(e.mixBuf[:len(out)])
	start := e.clock.Load()
	frames := len(out) / e.Channels()
	anySolo := false
	for i := range TrackCount {
		anySolo = anySolo || e.tracks[i].solo.Load()
	}
	for i := range TrackCount {
		e.mixTrack(i, e.mixBuf[:len(out)], start, frames, anySolo)
	}
	e.limit.process(e.mixBuf[:len(out)])
	for i := range out {
		out[i] += e.mixBuf[i]
	}
	e.clock.Add(int64(frames))
}

func commandOffset(pending *atomic.Int64, start int64, frames int) (int, bool) {
	v := pending.Load()
	if v == 0 {
		return 0, false
	}
	target := v - 1
	if target >= start+int64(frames) {
		return 0, false
	}
	if !pending.CompareAndSwap(v, 0) {
		return 0, false
	}
	if target <= start {
		return 0, true
	}
	return int(target - start), true
}

func (e *Engine) mixTrack(index int, dst []float32, start int64, frames int, anySolo bool) {
	t := &e.tracks[index]
	buf := t.scratch[:len(dst)]
	clear(buf)
	startOff, starts := commandOffset(&t.startAt, start, frames)
	stopOff, stops := commandOffset(&t.stopAt, start, frames)
	if starts && (!stops || startOff <= stopOff) {
		t.pos = 0
		t.playing.Store(true)
		e.changed(index)
	}
	from := 0
	if starts {
		from = startOff
	}
	to := frames
	if stops {
		to = stopOff
	}
	if t.playing.Load() && from < to {
		wasPlaying := t.playing.Load()
		e.readTrack(t, buf, from, to)
		if wasPlaying && !t.playing.Load() {
			e.changed(index)
		}
	}
	if stops {
		t.playing.Store(false)
		e.changed(index)
	}
	// Effects always receive silence while idle so delay/reverb tails decay.
	t.fx.Process(buf)
	if t.muted.Load() || (anySolo && !t.solo.Load()) {
		return
	}
	e.addTrack(t, dst, buf)
}

func (e *Engine) readTrack(t *track, buf []float32, from, to int) {
	ref := t.clip.Load()
	if ref == nil || len(ref.clip.Samples) == 0 {
		t.playing.Store(false)
		return
	}
	channels := e.Channels()
	clipFrames := len(ref.clip.Samples) / channels
	for frame := from; frame < to && t.playing.Load(); frame++ {
		if t.pos >= clipFrames {
			if t.loop.Load() {
				t.pos = 0
			} else {
				t.playing.Store(false)
				break
			}
		}
		copy(buf[frame*channels:(frame+1)*channels], ref.clip.Samples[t.pos*channels:(t.pos+1)*channels])
		t.pos++
	}
}

func (e *Engine) addTrack(t *track, dst, src []float32) {
	level := math.Float32frombits(t.level.Load())
	channels := e.Channels()
	if channels == 2 {
		pan := math.Float32frombits(t.pan.Load())
		left, right := level, level
		if pan > 0 {
			left *= 1 - pan
		} else if pan < 0 {
			right *= 1 + pan
		}
		for i := 0; i < len(src); i += 2 {
			dst[i] += src[i] * left
			dst[i+1] += src[i+1] * right
		}
		return
	}
	for i := range src {
		dst[i] += src[i] * level
	}
}

// Reset clears clips, playback, recording, settings, and the musical clock.
func (e *Engine) Reset() {
	e.CancelRecording()
	e.mixMu.Lock()
	defer e.mixMu.Unlock()
	e.clearLocked()
}

func (e *Engine) clearLocked() {
	for i := range TrackCount {
		t := &e.tracks[i]
		t.clip.Store(nil)
		t.playing.Store(false)
		t.muted.Store(false)
		t.solo.Store(false)
		t.loop.Store(true)
		t.truncated.Store(false)
		t.level.Store(math.Float32bits(1))
		t.pan.Store(math.Float32bits(0))
		t.startAt.Store(0)
		t.stopAt.Store(0)
		t.pos = 0
		t.mu.Lock()
		t.name = "Track " + string(rune('1'+i))
		t.settings = audiofx.Settings{}
		t.mu.Unlock()
		t.fx.Set(audiofx.Settings{})
		t.fx.Reset()
	}
	e.clock.Store(0)
	e.captureClock.Store(0)
	if e.limit != nil {
		e.limit.reset()
	}
}

// TrackState is the persistable non-audio state of one track.
type TrackState struct {
	Name    string           `json:"name"`
	Muted   bool             `json:"muted"`
	Solo    bool             `json:"solo"`
	Loop    bool             `json:"loop"`
	Level   float32          `json:"level"`
	Pan     float32          `json:"pan"`
	Effects audiofx.Settings `json:"effects"`
	HasClip bool             `json:"hasClip"`
}

// State is project metadata; audio samples are stored as separate WAV files.
type State struct {
	Version      int                    `json:"version"`
	Tempo        float64                `json:"tempo"`
	Quantization Quantization           `json:"quantization"`
	Tracks       [TrackCount]TrackState `json:"tracks"`
}

func (e *Engine) Snapshot() State {
	state := State{Version: 1, Tempo: e.Tempo(), Quantization: e.Quantization()}
	for i := range TrackCount {
		state.Tracks[i] = TrackState{
			Name: e.Name(i), Muted: e.Muted(i), Solo: e.Solo(i), Loop: e.Loop(i),
			Level: e.Level(i), Pan: e.Pan(i), Effects: e.Effects(i), HasClip: e.HasClip(i),
		}
	}
	return state
}

func (e *Engine) Restore(state State) {
	e.StopAllImmediate()
	e.SetTempo(state.Tempo)
	e.SetQuantization(state.Quantization)
	for i, st := range state.Tracks {
		_ = e.SetName(i, st.Name)
		_ = e.SetMuted(i, st.Muted)
		_ = e.SetSolo(i, st.Solo)
		_ = e.SetLoop(i, st.Loop)
		_ = e.SetLevel(i, st.Level)
		_ = e.SetPan(i, st.Pan)
		_ = e.SetEffects(i, st.Effects)
	}
}
