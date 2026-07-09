//go:build !android && !js

package arturia

import (
	"fmt"
	"os"

	"github.com/mono4loop/rp6/internal/midiin"
)

// init registers one driver per supported Arturia keyboard, each matched by a
// substring of its ALSA card name (case-folded by FindRawMIDI). The KeyStep and
// MicroLab advertise "KeyStep"/"MicroLab" in their USB product strings.
func init() {
	register("Arturia KeyStep 37", "keystep")
	register("Arturia MicroLab", "microlab")
}

func register(name string, matches ...string) {
	midiin.Register(midiin.Driver{
		Name:   name,
		Detect: func() (string, bool) { return midiin.FindRawMIDI(matches...) },
		Open:   func(path string) (midiin.Device, error) { return open(name, path) },
	})
}

func open(name, path string) (midiin.Device, error) {
	// Read-only: we only consume the keyboard's output. It is a distinct USB
	// device from the P-6, so this never conflicts with the P-6's exclusive node.
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("arturia: opening %s: %w", path, err)
	}
	return &device{name: name, path: path, rc: f}, nil
}
