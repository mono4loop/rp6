package emu

import (
	"bytes"
	"errors"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mono4loop/rp6/p6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sine returns a mono sine tone as interleaved float32.
func sine(freq float64, rate, frames int) []float32 {
	out := make([]float32, frames)
	for i := range out {
		out[i] = float32(0.5 * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
	}
	return out
}

// writeWAV writes a mono 16-bit WAV to path.
func writeWAV(t *testing.T, path string, samples []float32, rate int) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, EncodeWAV(&buf, samples, 1, rate))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := sine(440, 44100, 2048)
	var buf bytes.Buffer
	require.NoError(t, EncodeWAV(&buf, in, 1, 44100))

	clip, err := DecodeWAV(&buf)
	require.NoError(t, err)
	assert.Equal(t, 1, clip.Channels)
	assert.Equal(t, 44100, clip.SampleRate)
	require.Equal(t, len(in), len(clip.Samples))
	// 16-bit quantization error is < 1/32768.
	for i := range in {
		assert.InDelta(t, in[i], clip.Samples[i], 1e-3)
	}
}

func TestDecodeRejectsNonWAV(t *testing.T) {
	_, err := DecodeWAV(bytes.NewReader([]byte("not a wav file at all")))
	assert.ErrorIs(t, err, errNotWAV)
}

func TestResampleChannelsAndRate(t *testing.T) {
	c := &Clip{Samples: sine(200, 22050, 1000), Channels: 1, SampleRate: 22050}
	out := c.Resample(2, 44100)
	// Mono->stereo doubles per-frame samples; 22050->44100 doubles frames.
	// So total length ~= 1000 * 2 (channels) * 2 (rate).
	assert.InDelta(t, 1000*2*2, len(out), 4)
	// Stereo: left and right identical for a mono source.
	for i := 0; i+1 < len(out); i += 2 {
		assert.Equal(t, out[i], out[i+1])
	}
}

func TestSincTableCached(t *testing.T) {
	// The (expensive) kernel table is built once per cutoff and reused, so a kit's
	// pads don't each rebuild an identical table.
	a := sincTable(0.5)
	b := sincTable(0.5)
	assert.Same(t, a, b, "same cutoff returns the cached table")
	assert.NotSame(t, a, sincTable(0.25), "a different cutoff builds its own table")
}

func TestResampleNoOp(t *testing.T) {
	c := &Clip{Samples: []float32{0.1, -0.2, 0.3, -0.4}, Channels: 2, SampleRate: 48000}
	out := c.Resample(2, 48000)
	assert.Equal(t, c.Samples, out)
}

// TestResampleSincQuality checks the band-limited resampler reconstructs a pure
// tone accurately across a rate change (44.1k -> 48k), which linear
// interpolation could not (it rolls off / images the highs — the audible
// "lo-fi" artifact). The resampler is phase-aligned (output frame f samples
// source position f*srcRate/dstRate), so the expected value at f is the same
// sine evaluated at the destination rate.
func TestResampleSincQuality(t *testing.T) {
	const srcRate, dstRate, freq = 44100, 48000, 3000.0
	in := make([]float32, 0, srcRate) // 1s mono
	for n := 0; n < srcRate; n++ {
		in = append(in, float32(0.8*math.Sin(2*math.Pi*freq*float64(n)/srcRate)))
	}
	c := &Clip{Samples: in, Channels: 1, SampleRate: srcRate}
	out := c.Resample(1, dstRate)

	var maxErr float64
	// Skip the kernel-width edges where truncation reduces the window.
	for f := sincTaps; f < len(out)-sincTaps; f++ {
		want := 0.8 * math.Sin(2*math.Pi*freq*float64(f)/dstRate)
		if e := math.Abs(float64(out[f]) - want); e > maxErr {
			maxErr = e
		}
	}
	// Well below −60 dB (0.001); linear interpolation is ~10× worse here.
	assert.Less(t, maxErr, 1e-3, "sinc resample peak error too high: %v", maxErr)
}

func TestRemixDownmix(t *testing.T) {
	// Stereo->mono averages the source channels rather than dropping one.
	out := remixChannels([]float32{1.0, -1.0}, 2, 1)
	assert.Equal(t, []float32{0.0}, out)

	// 4ch->2ch folds all source channels in (average fanned out), not dropped.
	out = remixChannels([]float32{1.0, 3.0, 0.0, 0.0}, 4, 2)
	assert.Equal(t, []float32{1.0, 1.0}, out)

	// Mono->stereo duplicates.
	out = remixChannels([]float32{0.5, -0.25}, 1, 2)
	assert.Equal(t, []float32{0.5, 0.5, -0.25, -0.25}, out)
}

func TestParsePadLabel(t *testing.T) {
	cases := []struct {
		name      string
		bank, pad int
		ok        bool
	}{
		{"A1.wav", 0, 1, true},
		{"a1.wav", 0, 1, true},
		{"H6.WAV", 7, 6, true},
		{"E3 kick.wav", 4, 3, true},
		{"D6-crash.wav", 3, 6, true},
		{"I1.wav", 0, 0, false}, // bank out of range
		{"A7.wav", 0, 0, false}, // pad out of range
		{"kick.wav", 0, 0, false},
		{"A.wav", 0, 0, false},
	}
	for _, c := range cases {
		bank, pad, ok := parsePadLabel(c.name)
		assert.Equal(t, c.ok, ok, c.name)
		if c.ok {
			assert.Equal(t, c.bank, bank, c.name)
			assert.Equal(t, c.pad, pad, c.name)
		}
	}
}

func TestScanSamplesFlatAndSubdirs(t *testing.T) {
	dir := t.TempDir()
	// Flat pad label.
	writeWAV(t, filepath.Join(dir, "A1.wav"), sine(100, 8000, 100), 8000)
	writeWAV(t, filepath.Join(dir, "H6 crash.wav"), sine(100, 8000, 100), 8000)
	// Per-bank subdirectory.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "B"), 0o755))
	writeWAV(t, filepath.Join(dir, "B", "2.wav"), sine(100, 8000, 100), 8000)
	// Non-matching files are ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644))

	paths, err := scanSamples(os.DirFS(dir))
	require.NoError(t, err)
	assert.Len(t, paths, 3)
	assert.Contains(t, paths, padID(0, 1)) // A1
	assert.Contains(t, paths, padID(1, 2)) // B2
	assert.Contains(t, paths, padID(7, 6)) // H6
}

// TestScanSamplesP6Layout covers the P-6's own "BANK_x/PAD_n/*.wav" layout (as
// produced by the SampleTool and the factory pack), including that a sibling
// .PRM file is ignored and the first WAV in a pad folder is used.
func TestScanSamplesP6Layout(t *testing.T) {
	dir := t.TempDir()
	mk := func(bankDir, padDir, file string) {
		t.Helper()
		d := filepath.Join(dir, bankDir, padDir)
		require.NoError(t, os.MkdirAll(d, 0o755))
		writeWAV(t, filepath.Join(d, file), sine(100, 8000, 100), 8000)
	}
	mk("BANK_A", "PAD_1", "P6_A-1.WAV")
	mk("BANK_H", "PAD_6", "P6_H-6.WAV")
	// Lower-case variants are accepted too.
	mk("bank_c", "pad_3", "whatever.wav")
	// A stray .PRM alongside the WAV must be ignored (not treated as a sample).
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "BANK_A", "PAD_1", "P6_A-1.PRM"), []byte("PHRASE\t= 0\n"), 0o644))
	// An empty pad folder contributes nothing.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "BANK_B", "PAD_2"), 0o755))

	paths, err := scanSamples(os.DirFS(dir))
	require.NoError(t, err)
	assert.Len(t, paths, 3)
	assert.Contains(t, paths, padID(0, 1)) // A1
	assert.Contains(t, paths, padID(2, 3)) // C3
	assert.Contains(t, paths, padID(7, 6)) // H6
	assert.True(t, isWAV(paths[padID(0, 1)]), "should pick the WAV, not the PRM")
}

func TestParseBankAndPadDir(t *testing.T) {
	bankCases := map[string]int{"A": 0, "h": 7, "BANK_A": 0, "bank_h": 7}
	for name, want := range bankCases {
		b, ok := parseBankDir(name)
		assert.True(t, ok, name)
		assert.Equal(t, want, b, name)
	}
	for _, bad := range []string{"", "AA", "BANK_", "BANK_I", "I", "RESTORE", "IMPORT"} {
		_, ok := parseBankDir(bad)
		assert.False(t, ok, bad)
	}

	padCases := map[string]int{"1": 1, "6": 6, "PAD_1": 1, "pad_6": 6}
	for name, want := range padCases {
		p, ok := parsePadDir(name)
		assert.True(t, ok, name)
		assert.Equal(t, want, p, name)
	}
	for _, bad := range []string{"", "12", "PAD_", "PAD_7", "7", "0"} {
		_, ok := parsePadDir(bad)
		assert.False(t, ok, bad)
	}
}

func TestMixerTriggerRenderRetire(t *testing.T) {
	m := newMixer(1, 1000, false) // 2-frame limiter look-ahead
	m.trigger([]float32{1, 1, 1}, 1)
	assert.Equal(t, 1, m.active())

	out := make([]float32, 2)
	m.render(out)
	assert.Equal(t, []float32{0, 0}, out)
	assert.Equal(t, 1, m.active()) // one sample left

	m.render(out) // consumes the last sample; voice retires
	assert.InDeltaSlice(t, []float32{limiterCeiling, limiterCeiling}, out, 1e-6)
	assert.Equal(t, 0, m.active())

	m.render(out) // the limiter drains its final delayed sample
	assert.InDelta(t, limiterCeiling, out[0], 1e-6)
	assert.Zero(t, out[1])
}

func TestMixerLimitsOverlappingSamples(t *testing.T) {
	m := newMixer(1, 1000, false) // 2-frame limiter look-ahead
	m.trigger([]float32{0.8, 0.8, 0.8, 0.8}, 1)
	m.trigger([]float32{0.8, 0.8, 0.8, 0.8}, 1) // raw sum 1.6
	out := make([]float32, 6)
	m.render(out)
	assert.InDeltaSlice(t, []float32{0, 0, limiterCeiling, limiterCeiling, limiterCeiling, limiterCeiling}, out, 1e-6)
	assert.NotContains(t, out, float32(1), "the mix must not be hard-clipped")
}

func TestMixerVoiceCap(t *testing.T) {
	m := newMixer(1, 48000, false)
	for range maxVoices + 5 {
		m.trigger([]float32{0.1, 0.1, 0.1, 0.1}, 1)
	}
	assert.Equal(t, maxVoices, m.active())
}

func TestMixerTriggerSpeedDoublePitch(t *testing.T) {
	// A double-speed voice reads the clip twice as fast: a 4-frame mono clip is
	// consumed in ~2 output frames, skipping every other frame at zero phase.
	m := newMixer(1, 1000, false) // 2-frame limiter look-ahead
	m.triggerSpeed([]float32{0, 0.1, 0.2, 0.3}, 1, 2)
	out := make([]float32, 4)
	m.render(out)
	assert.InDeltaSlice(t, []float32{0, 0, 0, 0.2}, out, 1e-6)
	assert.Equal(t, 0, m.active(), "the clip is fully consumed")
}

func TestMixerTriggerSpeedHalfPitchInterpolates(t *testing.T) {
	// A half-speed voice reads the clip half as fast, linearly interpolating the
	// in-between frames.
	m := newMixer(1, 1000, false) // 2-frame limiter look-ahead
	m.triggerSpeed([]float32{0, 0.4}, 1, 0.5)
	out := make([]float32, 6)
	m.render(out)
	assert.InDeltaSlice(t, []float32{0, 0, 0, 0.2, 0.4, 0.4}, out, 1e-6)
	assert.Equal(t, 0, m.active())
}

// firstNonZero returns the index of the first non-zero sample, or -1.
func firstNonZero(out []float32) int {
	for i, v := range out {
		if v != 0 {
			return i
		}
	}
	return -1
}

func TestMixerVoiceStartTiming(t *testing.T) {
	const rate = 48000
	out := make([]float32, 4800) // 100 ms window, mono

	// Buffer-aligned: a trigger after a delay still starts at the buffer head.
	mb := newMixer(1, rate, false)
	mb.render(out) // establishes the render clock
	time.Sleep(15 * time.Millisecond)
	mb.trigger([]float32{1}, 1)
	mb.render(out)
	bufferStart := firstNonZero(out)
	assert.Equal(t, mb.limiter.lookahead, bufferStart, "buffer mode starts after the limiter look-ahead")

	// Sample-accurate: the same delayed trigger starts partway into the buffer,
	// reflecting the elapsed time (preserving true inter-hit timing).
	ma := newMixer(1, rate, true)
	ma.render(out)
	time.Sleep(15 * time.Millisecond)
	ma.trigger([]float32{1}, 1)
	ma.render(out)
	assert.Greater(t, firstNonZero(out), bufferStart, "sample-accurate mode adds the elapsed time to the limiter look-ahead")
}

func TestEmulatorOpenAndTrigger(t *testing.T) {
	dir := t.TempDir()
	writeWAV(t, filepath.Join(dir, "A1.wav"), sine(220, 8000, 200), 8000)
	writeWAV(t, filepath.Join(dir, "C4.wav"), sine(440, 8000, 200), 8000)

	e, err := Open(dir, p6.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	// Keep the capture sink from consuming voices while this test inspects them.
	require.NoError(t, e.sink.Stop())

	assert.Equal(t, 2, e.Loaded())
	assert.Contains(t, e.Path(), dir)

	// Triggering a loaded pad enqueues a voice.
	require.NoError(t, e.TriggerPad(0, 1)) // A1
	assert.Equal(t, 1, e.mix.active())

	// A pad with no sample is silently ignored.
	require.NoError(t, e.TriggerPad(7, 6)) // H6 (not loaded)
	assert.Equal(t, 1, e.mix.active())

	// Out-of-range pads error.
	assert.Error(t, e.TriggerPad(0, 7))
	assert.Error(t, e.TriggerPad(8, 1))

	// NoteOn on the Sampler channel triggers; other channels don't.
	note, _ := p6.NoteFor(2, 4) // C4
	require.NoError(t, e.NoteOn(e.Config().SamplerChannel, note, 100))
	assert.Equal(t, 2, e.mix.active())
	require.NoError(t, e.NoteOn(e.Config().GranularChannel, note, 100))
	assert.Equal(t, 2, e.mix.active())
}

func TestEmulatorPlayNote(t *testing.T) {
	dir := t.TempDir()
	writeWAV(t, filepath.Join(dir, "A1.wav"), sine(220, 8000, 200), 8000)
	e, err := Open(dir, p6.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	// Keep the capture sink from consuming voices while this test inspects them.
	require.NoError(t, e.sink.Stop())

	// The default selection (A1, id 0) is loaded, so keyboard notes pitch it.
	require.NoError(t, e.PlayNote(p6.KeyboardCenterNote, 100))
	assert.Equal(t, 1, e.mix.active())
	require.NoError(t, e.PlayNote(p6.KeyboardCenterNote+12, 100)) // an octave up
	assert.Equal(t, 2, e.mix.active())

	// Triggering a pad selects it: keyboard mode then pitches that pad's sample.
	require.NoError(t, e.TriggerPad(7, 6)) // H6 (no sample) -> selects it
	assert.Equal(t, 2, e.mix.active())
	require.NoError(t, e.PlayNote(p6.KeyboardCenterNote, 100))
	assert.Equal(t, 2, e.mix.active(), "no sample on the selected pad -> ignored")
}

func TestEmulatorControllerNoOps(t *testing.T) {
	dir := t.TempDir()
	writeWAV(t, filepath.Join(dir, "A1.wav"), sine(220, 8000, 100), 8000)
	e, err := Open(dir, p6.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })

	// Transport/CC operations are accepted no-ops.
	assert.NoError(t, e.Start())
	assert.NoError(t, e.Continue())
	assert.NoError(t, e.Stop())
	assert.NoError(t, e.Clock())
	assert.NoError(t, e.ProgramChange(3))
	assert.NoError(t, e.AutoCC(p6.CCDelayLevel, 64))
	assert.NoError(t, e.GranularCC(p6.CCSample, 10))
	assert.NoError(t, e.ControlChange(1, 7, 100))

	// The emulator has no MIDI input.
	assert.ErrorIs(t, e.Listen(func(p6.Event) {}), p6.ErrNoInput)
}

func TestOpenErrors(t *testing.T) {
	_, err := Open("", p6.DefaultConfig())
	assert.Error(t, err)

	_, err = Open(filepath.Join(t.TempDir(), "does-not-exist"), p6.DefaultConfig())
	assert.Error(t, err)

	// Empty directory (no pad samples) fails with a clear message.
	_, err = Open(t.TempDir(), p6.DefaultConfig())
	assert.Error(t, err)
}

// TestOpenDefaultLoadsEmbeddedKit confirms the built-in "modular-hits" kit
// embedded in the binary loads all 48 pads with no external files (silent sink
// under `go test`, so no audio backend needed).
func TestOpenDefaultLoadsEmbeddedKit(t *testing.T) {
	e, err := OpenDefault(p6.DefaultConfig())
	require.NoError(t, err)
	defer e.Close()
	assert.Equal(t, p6.NumPads, e.Loaded(), "embedded kit should populate all 48 pads")
	assert.Contains(t, e.Path(), "modular-hits")
}

// TestDefaultKitEmbedsAll48 checks the embedded FS directly (independent of the
// audio sink).
func TestDefaultKitEmbedsAll48(t *testing.T) {
	paths, err := scanSamples(defaultKitFSSub())
	require.NoError(t, err)
	assert.Len(t, paths, p6.NumPads, "embedded modular-hits kit should map all 48 pads")
}

// TestScanFactoryPack loads the checked-in factory sample pack (the real P-6
// export layout) if present, confirming all 48 pads are recognized. Skipped
// when the pack isn't available.
func TestScanFactoryPack(t *testing.T) {
	dir := filepath.Join("..", "..", "samples", "p6")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("factory sample pack not present at %s", dir)
	}
	paths, err := scanSamples(os.DirFS(dir))
	require.NoError(t, err)
	assert.Len(t, paths, p6.NumPads, "factory pack should populate all 48 pads")
	for id := range p6.NumPads {
		assert.Contains(t, paths, id)
		assert.True(t, isWAV(paths[id]), "pad %d should map to a WAV", id)
	}
}

// fakeDirEntry is a minimal fs.DirEntry for the scan-error tests.
type fakeDirEntry struct {
	name string
	dir  bool
}

func (d fakeDirEntry) Name() string { return d.name }
func (d fakeDirEntry) IsDir() bool  { return d.dir }
func (d fakeDirEntry) Type() os.FileMode {
	if d.dir {
		return os.ModeDir
	}
	return 0
}
func (d fakeDirEntry) Info() (os.FileInfo, error) { return nil, nil }

// fakeFS is an fs.ReadDirFS whose ReadDir returns a configured error for chosen
// directories, so sample-scan error paths can be exercised deterministically.
type fakeFS struct {
	root    []fakeDirEntry
	errDirs map[string]error
}

func (f fakeFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

func (f fakeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if e, ok := f.errDirs[name]; ok {
		return nil, e
	}
	if name == "." {
		out := make([]fs.DirEntry, len(f.root))
		for i, e := range f.root {
			out[i] = e
		}
		return out, nil
	}
	return nil, fs.ErrNotExist
}

// TestScanSamplesLogsBankDirError verifies a bank-subdirectory read failure is
// logged (like a bad WAV in load()) instead of being silently skipped (jaeb).
func TestScanSamplesLogsBankDirError(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	fsys := fakeFS{
		root:    []fakeDirEntry{{name: "A", dir: true}},
		errDirs: map[string]error{"A": errors.New("perm-boom")},
	}
	_, err := scanSamples(fsys)
	require.NoError(t, err) // the scan still succeeds, just with no pads
	assert.Contains(t, buf.String(), "perm-boom", "the bank-dir read error should be logged")
}

// TestFirstAudioLogsReadError verifies a pad-folder read failure (P-6 layout) is
// logged instead of silently returning no match (jaeb).
func TestFirstAudioLogsReadError(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	fsys := fakeFS{errDirs: map[string]error{"BANK_A/PAD_1": errors.New("pad-boom")}}
	_, ok := firstAudio(fsys, "BANK_A/PAD_1")
	assert.False(t, ok)
	assert.Contains(t, buf.String(), "pad-boom", "the pad-dir read error should be logged")
}
