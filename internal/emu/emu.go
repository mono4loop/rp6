// Package emu is a software emulator for the Roland P-6 that plays WAV samples
// through the computer's audio output, so rp6 is usable without the hardware.
//
// It implements p6.Controller, so the app can hold an *emu.Emulator anywhere it
// would hold a *p6.Device and swap the two transparently. Triggering a pad
// plays that pad's sample; transport/clock/CC operations are accepted but have
// no audible effect (rp6 sequences host-side, and the emulator has no internal
// sequencer or granular engine).
//
// Samples are laid out like the P-6's own 48 slots — 8 banks (A–H) × 6 pads —
// so the same directory works for both the emulator and the hardware. Each pad
// file may be a WAV or a FLAC (".wav"/".flac", case-insensitive); FLAC is
// lossless and rp6-emulator-only (the P-6 hardware imports WAV only). Three
// layouts are recognized (the first match per pad wins):
//
//   - flat pad labels in one directory: "A1.wav".."H6.wav" (a descriptive
//     suffix is allowed, e.g. "A1 kick.wav"; ".flac" works too);
//   - per-bank subdirectories with pad files: "A/1.wav".."H/6.wav";
//   - the P-6's own export/import layout: "BANK_A/PAD_1/*.wav" .. "BANK_H/PAD_6/*.wav"
//     (bank dir "BANK_<A..H>", pad dir "PAD_<1..6>", any audio file inside; a
//     sibling ".PRM" is ignored). This is exactly what the P-6 SampleTool and
//     the factory sample pack produce, so one directory feeds both the emulator
//     and a hardware import.
//
// Audio output uses the malgo/miniaudio backend behind the "capture" build tag
// (the same tag rp6 already uses for the VU meter). Without the tag the
// emulator still loads and mixes samples but stays silent, keeping it fully
// testable and swappable.
package emu

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"math"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mono4loop/rp6/internal/audiofx"
	"github.com/mono4loop/rp6/internal/recorder"
	"github.com/mono4loop/rp6/p6"
)

// profile enables coarse load-timing logs (RP6_PROFILE=1) to find where the time
// goes when opening/switching sample kits.
var profile = os.Getenv("RP6_PROFILE") != ""

func perf(format string, args ...any) {
	if profile {
		log.Printf("rp6perf/emu: "+format, args...)
	}
}

// SampleAccurate selects the mixer's voice-start timing for emulators opened
// afterwards: true (default) starts each triggered sample at its exact sub-buffer
// position so near-simultaneous pad hits keep their real relative timing; false
// aligns voice starts to the audio-buffer boundary (coarser, marginally lower
// latency). Set it before Open/OpenDefault (see cmd/rp6's -timing flag).
var SampleAccurate = true

// Emulator is a sample-playing stand-in for a P-6. It is safe for concurrent
// use (pads may be fired from the app, sequencer and effects-roll goroutines).
type Emulator struct {
	cfg    p6.Config
	name   string // human-readable source (directory path, or the built-in kit)
	fsys   fs.FS  // samples filesystem (os.DirFS for a dir, or the embedded kit)
	sink   sink
	mix    *mixer
	clips  [p6.NumPads][]float32 // per-pad samples, resampled to the sink format
	loaded int

	// selected is the pad id (0..47) that keyboard mode (PlayNote) pitches. It
	// tracks the last pad triggered, mirroring the hardware's "select a pad,
	// then play it chromatically" behavior. Accessed from multiple goroutines.
	selected atomic.Int32
}

// Open loads the WAV samples under dir and starts audio output, returning an
// Emulator ready to trigger pads. It fails if dir isn't a directory or contains
// no recognizable pad samples.
func Open(dir string, cfg p6.Config) (*Emulator, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("emu: no samples directory given")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("emu: samples directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("emu: %s is not a directory", dir)
	}
	return OpenFS(os.DirFS(dir), dir, cfg)
}

// OpenDefault loads the built-in "modular-hits" sample kit embedded in the
// binary and starts audio output, so the emulator is playable with no external
// samples. See DefaultKitName / assets/modular-hits/CREDITS.txt.
func OpenDefault(cfg p6.Config) (*Emulator, error) {
	return OpenFS(defaultKitFSSub(), DefaultKitName, cfg)
}

// OpenFS loads pad samples from an arbitrary filesystem (e.g. os.DirFS, an
// embedded kit, or a *zip.Reader for a .rp6sp pak) and starts audio output.
// name is a human-readable label for the source. It scans fsys for pad samples
// (A1..H6), decodes them and starts the audio sink.
func OpenFS(fsys fs.FS, name string, cfg p6.Config) (*Emulator, error) {
	t := time.Now()
	s, err := openSink()
	if err != nil {
		return nil, err
	}
	perf("openSink %s -> %s", time.Since(t), s.Name())
	e := &Emulator{cfg: cfg, name: name, fsys: fsys, sink: s, mix: newMixer(s.Channels(), s.SampleRate(), SampleAccurate)}

	t = time.Now()
	n, err := e.load()
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	perf("load %d pads in %s", n, time.Since(t))
	if n == 0 {
		_ = s.Close()
		return nil, fmt.Errorf("emu: no pad samples (A1..H6 .wav/.flac) found in %s", name)
	}
	e.loaded = n

	t = time.Now()
	if err := s.Start(e.mix.render); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("emu: starting audio output: %w", err)
	}
	perf("sink.Start %s", time.Since(t))
	log.Printf("emu: loaded %d/%d pad samples from %s (output: %s)", n, p6.NumPads, name, s.Name())
	return e, nil
}

// Loaded reports how many of the 48 pads have a sample assigned.
func (e *Emulator) Loaded() int { return e.loaded }

// load scans the samples fs for pad-labeled WAV files and decodes them into
// per-pad clips resampled to the sink's format. Undecodable files are logged
// and skipped.
func (e *Emulator) load() (int, error) {
	paths, err := scanSamples(e.fsys)
	if err != nil {
		return 0, err
	}
	ids := make([]int, 0, len(paths))
	for id := range paths {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	// Decode + resample the pads in parallel. Each pad is independent — decoding
	// allocates a fresh clip and each writes a distinct e.clips[id] — and the
	// resampler's kernel cache is concurrency-safe, so this scales across cores
	// and is the bulk of the kit-load time. wg.Wait establishes happens-before for
	// the clips written by the workers.
	workers := min(runtime.NumCPU(), len(ids))
	if workers < 1 {
		workers = 1
	}
	dstCh, dstRate := e.sink.Channels(), e.sink.SampleRate()
	var (
		count atomic.Int64
		wg    sync.WaitGroup
		jobs  = make(chan int)
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				clip, err := decodeFile(e.fsys, paths[id])
				if err != nil {
					log.Printf("emu: skipping %s: %v", paths[id], err)
					continue
				}
				e.clips[id] = clip.Resample(dstCh, dstRate)
				count.Add(1)
			}
		}()
	}
	for _, id := range ids {
		jobs <- id
	}
	close(jobs)
	wg.Wait()
	perf("load: %d/%d files across %d workers", count.Load(), len(ids), workers)
	return int(count.Load()), nil
}

// scanSamples maps pad ids to WAV file paths (relative to the fs root) found in
// fsys, honoring the flat "A1.wav" layout, per-bank subdirectories "A/1.wav",
// and the P-6's own "BANK_A/PAD_1/*.wav" export layout. The first match for a
// pad wins (entries are visited in sorted order for determinism).
func scanSamples(fsys fs.FS) (map[int]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("emu: reading samples: %w", err)
	}
	sortEntries(entries)

	paths := make(map[int]string)
	put := func(id int, p string) {
		if _, exists := paths[id]; !exists {
			paths[id] = p
		}
	}

	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() {
			bank, ok := parseBankDir(name)
			if !ok {
				continue
			}
			subEntries, err := fs.ReadDir(fsys, name)
			if err != nil {
				log.Printf("emu: skipping bank dir %s: %v", name, err)
				continue
			}
			sortEntries(subEntries)
			for _, se := range subEntries {
				if se.IsDir() {
					// P-6 layout: BANK_x/PAD_n/<file>.{wav,flac} — take the
					// first audio file inside the pad folder (its sibling .PRM
					// is ignored).
					if pad, ok := parsePadDir(se.Name()); ok {
						if af, ok := firstAudio(fsys, path.Join(name, se.Name())); ok {
							put(padID(bank, pad), af)
						}
					}
					continue
				}
				if !isAudioFile(se.Name()) {
					continue
				}
				if pad, ok := parsePadNumber(se.Name()); ok {
					put(padID(bank, pad), path.Join(name, se.Name()))
				}
			}
			continue
		}
		if !isAudioFile(name) {
			continue
		}
		if bank, pad, ok := parsePadLabel(name); ok {
			put(padID(bank, pad), name)
		}
	}
	return paths, nil
}

// decodeFile opens name from fsys and decodes it by extension: WAV via
// DecodeWAV, FLAC via DecodeFLAC. FLAC is an rp6-emulator-only format (the P-6
// hardware only imports WAV).
func decodeFile(fsys fs.FS, name string) (*Clip, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if isFLAC(name) {
		return DecodeFLAC(f)
	}
	return DecodeWAV(f)
}

// padID maps a 0-based bank and 1-based pad to a stable 0-based pad id (0..47),
// matching the note ordering the hardware uses (bank A pad 1 .. bank H pad 6).
func padID(bank, pad int) int { return bank*p6.PadsPerBank + (pad - 1) }

func isWAV(name string) bool  { return strings.EqualFold(filepath.Ext(name), ".wav") }
func isFLAC(name string) bool { return strings.EqualFold(filepath.Ext(name), ".flac") }

// isAudioFile reports whether name is a pad sample the emulator can decode: a
// WAV (also readable by the P-6 hardware) or a FLAC (rp6-emulator only).
func isAudioFile(name string) bool { return isWAV(name) || isFLAC(name) }

func sortEntries(entries []fs.DirEntry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
}

// parsePadLabel extracts a bank/pad from a filename like "A1", "a1 kick" or
// "H6-crash" (extension already ignored). bank is 0-based, pad is 1-based.
func parsePadLabel(name string) (bank, pad int, ok bool) {
	base := strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
	if len(base) < 2 {
		return 0, 0, false
	}
	b, ok := bankLetter(base[0])
	if !ok {
		return 0, 0, false
	}
	p, ok := padDigit(base[1])
	if !ok {
		return 0, 0, false
	}
	return b, p, true
}

// parseBankDir maps a bank directory name to a 0-based bank. It accepts both a
// single letter "A".."H" and the P-6's own "BANK_A".."BANK_H"
// (case-insensitive).
func parseBankDir(name string) (bank int, ok bool) {
	if len(name) == 1 {
		return bankLetter(name[0])
	}
	if len(name) == len("BANK_")+1 && strings.EqualFold(name[:len("BANK_")], "BANK_") {
		return bankLetter(name[len("BANK_")])
	}
	return 0, false
}

// parsePadDir maps a pad directory name to a 1-based pad. It accepts both a bare
// number "1".."6" and the P-6's own "PAD_1".."PAD_6" (case-insensitive).
func parsePadDir(name string) (pad int, ok bool) {
	if len(name) == 1 {
		return padDigit(name[0])
	}
	if len(name) == len("PAD_")+1 && strings.EqualFold(name[:len("PAD_")], "PAD_") {
		return padDigit(name[len("PAD_")])
	}
	return 0, false
}

// firstAudio returns the path of the first audio file (WAV or FLAC, in sorted
// order) directly inside dir of fsys, if any.
func firstAudio(fsys fs.FS, dir string) (string, bool) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		log.Printf("emu: reading pad dir %s: %v", dir, err)
		return "", false
	}
	sortEntries(entries)
	for _, e := range entries {
		if !e.IsDir() && isAudioFile(e.Name()) {
			return path.Join(dir, e.Name()), true
		}
	}
	return "", false
}

// parsePadNumber maps a filename beginning with "1".."6" to a 1-based pad.
func parsePadNumber(name string) (pad int, ok bool) {
	base := strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
	if base == "" {
		return 0, false
	}
	return padDigit(base[0])
}

// bankLetter maps an ASCII 'A'..'H' (case-insensitive) to a 0-based bank index.
func bankLetter(c byte) (int, bool) {
	if c >= 'a' && c <= 'z' {
		c -= 'a' - 'A'
	}
	if c < 'A' || c > 'H' {
		return 0, false
	}
	return int(c - 'A'), true
}

// padDigit maps an ASCII '1'..'6' to a 1-based pad number.
func padDigit(c byte) (int, bool) {
	if c < '1' || c > '6' {
		return 0, false
	}
	return int(c - '0'), true
}

// --- p6.Controller implementation ---

var _ p6.Controller = (*Emulator)(nil)

// TriggerPad plays the sample assigned to (bank, pad) at the default velocity.
func (e *Emulator) TriggerPad(bank, pad int) error {
	return e.TriggerPadVelocity(bank, pad, e.cfg.Velocity)
}

// TriggerPadVelocity plays the pad's sample scaled by velocity (0..127). Pads
// with no assigned sample are silently ignored.
func (e *Emulator) TriggerPadVelocity(bank, pad int, velocity uint8) error {
	if _, err := p6.NoteFor(bank, pad); err != nil {
		return err
	}
	id := padID(bank, pad)
	e.selected.Store(int32(id)) // playing a pad selects it for keyboard mode
	data := e.clips[id]
	if data == nil {
		return nil
	}
	e.mix.trigger(data, float32(velocity)/127)
	return nil
}

// NoteOn plays a pad when the note lands on the Sampler channel and maps to a
// pad; other channels are ignored (the emulator has no granular/auto voice).
func (e *Emulator) NoteOn(channel int, note, velocity uint8) error {
	if channel != e.cfg.SamplerChannel {
		return nil
	}
	bank, pad, err := p6.PadForNote(note)
	if err != nil {
		return nil
	}
	return e.TriggerPadVelocity(bank, pad, velocity)
}

// PlayNote pitches the currently-selected pad's sample by
// (note - KeyboardCenterNote) semitones and plays it — the emulator's take on
// the P-6's keyboard mode. The selected pad is the last one triggered (see
// TriggerPadVelocity), mirroring the hardware ("select a pad, then play it
// chromatically"). Pads with no assigned sample are silently ignored.
func (e *Emulator) PlayNote(note, velocity uint8) error {
	id := int(e.selected.Load())
	if id < 0 || id >= len(e.clips) {
		return nil
	}
	data := e.clips[id]
	if data == nil {
		return nil
	}
	speed := math.Pow(2, float64(int(note)-p6.KeyboardCenterNote)/12.0)
	e.mix.triggerKeyboard(data, float32(velocity)/127, speed)
	return nil
}

// SetKeyboardFX updates the emulator's keyboard-bus instrument effects. It is
// intentionally outside p6.Controller: hardware keyboard notes are generated
// and heard inside the P-6, so host-side DSP is only available on the emulator.
func (e *Emulator) SetKeyboardFX(settings audiofx.Settings) {
	e.mix.setKeyboardFX(settings)
}

// SetKeyboardFXEnabled bypasses or enables the keyboard effects while retaining
// their configured macro values for the next enable.
func (e *Emulator) SetKeyboardFXEnabled(enabled bool) {
	e.mix.setKeyboardFXEnabled(enabled)
}

// RecorderFormat reports the emulator output format used for host recording.
func (e *Emulator) RecorderFormat() (channels, sampleRate int) {
	return e.sink.Channels(), e.sink.SampleRate()
}

// SetRecorder attaches the host recorder to the emulator's existing audio
// callback. tap receives emulator audio only, so recorder playback cannot feed
// back into a new recording.
func (e *Emulator) SetRecorder(rec *recorder.Engine, tap func([]float32)) {
	e.mix.setRecorder(rec, tap)
}

// ControlChange is accepted but has no effect (no emulated FX engine).
func (e *Emulator) ControlChange(channel int, cc, value uint8) error { return nil }

// ProgramChange is accepted but has no effect (no emulated patterns).
func (e *Emulator) ProgramChange(program uint8) error { return nil }

// AutoCC is accepted but has no effect.
func (e *Emulator) AutoCC(cc, value uint8) error { return nil }

// GranularCC is accepted but has no effect.
func (e *Emulator) GranularCC(cc, value uint8) error { return nil }

// Start/Continue/Stop/Clock are no-ops: rp6 sequences host-side and fires pads
// directly, so the emulator needs no transport of its own.
func (e *Emulator) Start() error    { return nil }
func (e *Emulator) Continue() error { return nil }
func (e *Emulator) Stop() error     { return nil }
func (e *Emulator) Clock() error    { return nil }

// Config returns the channel/velocity configuration.
func (e *Emulator) Config() p6.Config { return e.cfg }

// Path returns a human-readable description of the emulated endpoint.
func (e *Emulator) Path() string {
	return fmt.Sprintf("emulator — %s (%d/%d pads, %s)", e.name, e.loaded, p6.NumPads, e.sink.Name())
}

// Listen reports that the emulator has no MIDI input. Returning ErrNoInput lets
// the app's listen goroutine exit quietly (it special-cases that error).
func (e *Emulator) Listen(handler func(p6.Event)) error { return p6.ErrNoInput }

// Close stops all playing voices and releases the audio device.
func (e *Emulator) Close() error {
	e.mix.setRecorder(nil, nil)
	e.mix.reset()
	_ = e.sink.Stop()
	return e.sink.Close()
}
