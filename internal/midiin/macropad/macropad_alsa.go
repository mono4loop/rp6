//go:build !android && !js

package macropad

import (
	"fmt"
	"os"

	"github.com/mono4loop/rp6/internal/midiin"
)

// cardMatches are the /proc/asound/cards substrings that identify the MacroPad.
// CircuitPython's USB MIDI advertises the product string "MacroPad"; QMK builds
// advertise "Adafruit Macropad". Both contain "macropad" (matched case-folded).
var cardMatches = []string{"macropad"}

func init() {
	midiin.Register(midiin.Driver{
		Name:   Name,
		Detect: func() (string, bool) { return midiin.FindRawMIDI(cardMatches...) },
		Open:   open,
	})
}

func open(path string) (midiin.Device, error) {
	// Read-only: we only consume the controller's output. It is a distinct USB
	// device from the P-6, so this never conflicts with the P-6's exclusive node.
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("macropad: opening %s: %w", path, err)
	}
	return &device{path: path, rc: f}, nil
}
