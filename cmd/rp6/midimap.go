package main

import (
	"embed"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"

	fyne "fyne.io/fyne/v2"
	"github.com/mono4loop/rp6/internal/midiin"
	"github.com/mono4loop/rp6/internal/midiin/mapped"
	"github.com/mono4loop/rp6/p6"
)

// Data-driven MIDI input controllers. A .midimap file binds a controller's MIDI
// messages to the named RP6 control intents dispatched here — see
// docs/architecture/midimaps.md. This file owns the intent vocabulary; the
// generic interpreter (internal/midiin/mapped) only forwards names.

//go:embed assets/midimaps/*.midimap
var embeddedMaps embed.FS

// midiMapVocabulary is the set of intent names dispatchIntent handles. Maps are
// validated against it at load time (plus the interpreter-internal names), so a
// typo or a renamed intent fails loudly instead of silently doing nothing.
var midiMapVocabulary = map[string]bool{
	"pad.trigger":     true,
	"pad.trigger.rel": true,
	"note.play":       true,
	"transport.play":  true,
	"transport.stop":  true,
	"tempo.set":       true,
	"tempo.delta":     true,
	"pattern.set":     true,
	"pattern.delta":   true,
	"delay.set":       true,
	"reverb.set":      true,
}

// dispatchIntent applies one control intent from a mapped controller. It runs on
// the controller's read goroutine, so UI-touching work is marshalled through
// fyne.Do (the pad/note helpers do their own). Gated by the listen (eye) toggle,
// like the hand-written controllers.
func (u *ui) dispatchIntent(in midiin.Intent) {
	if !u.listenMIDI.Load() {
		return
	}
	switch in.Name {
	case "pad.trigger", "pad.trigger.rel":
		// When keys-routing is on, play the raw note on the on-screen keyboard
		// (pitching the selected sample) instead of triggering the mapped pad —
		// for controllers switched to a keyboard/note mode (e.g. the C16's).
		if u.keysRoute.Load() {
			u.playExternalNote(in.Note, ensureVel(in.Velocity))
		} else {
			u.fireExternalPad(in.Pad, ensureVel(in.Velocity))
		}
	case "note.play":
		u.playExternalNote(in.Note, ensureVel(in.Velocity))
	case "transport.play":
		fyne.Do(func() { u.playBtn.SetRunning(true); u.play() })
	case "transport.stop":
		fyne.Do(func() { u.playBtn.SetRunning(false); u.stop() })
	case "tempo.set":
		fyne.Do(func() { u.tempo.SetValue(rangeVal(in.Value, 40, 300)) })
	case "tempo.delta":
		fyne.Do(func() { u.tempo.SetValue(u.tempo.Value() + in.Delta*5) })
	case "pattern.set":
		fyne.Do(func() { u.patternStep.SetValue(rangeVal(in.Value, 0, 63)) })
	case "pattern.delta":
		fyne.Do(func() { u.patternStep.SetValue(u.patternStep.Value() + in.Delta) })
	case "delay.set":
		fyne.Do(func() {
			if u.delayKnob != nil {
				u.delayKnob.SetValue(rangeVal(in.Value, 0, 127))
			}
		})
	case "reverb.set":
		fyne.Do(func() {
			if u.reverbKnob != nil {
				u.reverbKnob.SetValue(rangeVal(in.Value, 0, 127))
			}
		})
	}
}

// ensureVel clamps a trigger velocity to a musical minimum (a mapped CC-as-pad
// can arrive as 0).
func ensureVel(v uint8) uint8 {
	if v == 0 {
		return p6.DefaultVelocity
	}
	return v
}

// rangeVal maps a normalized 0..1 value onto the integer range [lo,hi].
func rangeVal(v float64, lo, hi int) int {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return lo + int(math.Round(v*float64(hi-lo)))
}

var midiMapsOnce sync.Once

// registerMIDIMaps loads every valid .midimap (embedded + user directory) and
// registers a midiin.Driver for each, so startMIDIInput picks them up alongside
// the hand-written drivers. Idempotent (runs at most once per process).
func registerMIDIMaps() {
	midiMapsOnce.Do(func() {
		for _, m := range loadMIDIMaps() {
			m := m
			midiin.Register(midiin.Driver{
				Name:   m.Name,
				Detect: func() (string, bool) { return mapped.Detect(m.Match) },
				Open:   func(path string) (midiin.Device, error) { return mapped.Open(path, m) },
			})
			log.Printf("rp6: registered MIDI map %q (match %v)", m.Name, m.Match)
		}
	})
}

// loadMIDIMaps parses user-directory maps first (so they override embedded ones
// by device name), then embedded maps. Invalid maps are logged and skipped so a
// bad file never breaks launch.
func loadMIDIMaps() []*mapped.Map {
	var out []*mapped.Map
	seen := map[string]bool{}
	add := func(name, text string) {
		m, err := mapped.Parse(text)
		if err != nil {
			log.Printf("rp6: skipping midimap %s: %v", name, err)
			return
		}
		if err := validateMap(m); err != nil {
			log.Printf("rp6: skipping midimap %s: %v", name, err)
			return
		}
		if seen[m.Name] {
			return // already provided (user overrides embedded)
		}
		seen[m.Name] = true
		out = append(out, m)
	}
	for _, f := range userMapFiles() {
		if data, err := os.ReadFile(f); err == nil {
			add(f, string(data))
		}
	}
	_ = fs.WalkDir(embeddedMaps, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".midimap" {
			return nil
		}
		if data, e := embeddedMaps.ReadFile(p); e == nil {
			add(p, string(data))
		}
		return nil
	})
	return out
}

// validateMap rejects a map whose bindings target an unknown intent (guards
// against typos / renamed intents drifting from the vocabulary).
func validateMap(m *mapped.Map) error {
	for _, b := range m.Bindings {
		if !midiMapVocabulary[b.Intent] && !mapped.IsInternalIntent(b.Intent) {
			return &unknownIntentError{device: m.Name, intent: b.Intent}
		}
	}
	return nil
}

type unknownIntentError struct{ device, intent string }

func (e *unknownIntentError) Error() string {
	return "unknown intent " + e.intent + " in device " + e.device
}

// userMapFiles lists *.midimap in the per-user config directory, if any.
func userMapFiles() []string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(dir, "rp6", "midimaps", "*.midimap"))
	return files
}
