//go:build android

package arturia

import (
	"fmt"
	"strings"

	"github.com/mono4loop/rp6/internal/midiin"
	"github.com/mono4loop/rp6/midibridge"
)

// init registers one driver per supported Arturia keyboard, each matched by a
// substring of the MIDI port name the Android MIDI layer reports to the bridge.
func init() {
	register("Arturia KeyStep 37", "keystep")
	register("Arturia MicroLab", "microlab")
}

func register(name string, matches ...string) {
	midiin.Register(midiin.Driver{
		Name:   name,
		Detect: func() (string, bool) { return detect(matches) },
		Open:   func(id string) (midiin.Device, error) { return open(name, id) },
	})
}

// detect scans the bridge's input devices for a matching keyboard, returning the
// bridge device id (used as the "path" Open reopens), mirroring the ALSA driver.
func detect(matches []string) (string, bool) {
	for i := 0; i < midibridge.InputCount(); i++ {
		n := strings.ToLower(midibridge.InputName(i))
		for _, m := range matches {
			if strings.Contains(n, m) {
				return midibridge.InputID(i), true
			}
		}
	}
	return "", false
}

func open(name, id string) (midiin.Device, error) {
	rd := midibridge.OpenReader(id)
	if rd == nil {
		return nil, fmt.Errorf("arturia: bridge device %q not available", id)
	}
	return &device{name: name, path: id, rc: rd}, nil
}
