//go:build android

package macropad

import (
	"fmt"
	"strings"

	"github.com/mono4loop/rp6/internal/midiin"
	"github.com/mono4loop/rp6/midibridge"
)

// bridgeMatch is the MIDI port-name substring (case-folded) that identifies the
// MacroPad among the devices the Android MIDI layer reports to the bridge.
const bridgeMatch = "macropad"

func init() {
	midiin.Register(midiin.Driver{
		Name:   Name,
		Detect: detect,
		Open:   open,
	})
}

// detect scans the bridge's input devices for a MacroPad. It returns the bridge
// device id (used as the "path" that Open reopens), mirroring the ALSA driver
// where Detect returns the rawmidi node path.
func detect() (string, bool) {
	for i := 0; i < midibridge.InputCount(); i++ {
		if strings.Contains(strings.ToLower(midibridge.InputName(i)), bridgeMatch) {
			return midibridge.InputID(i), true
		}
	}
	return "", false
}

func open(id string) (midiin.Device, error) {
	rd := midibridge.OpenReader(id)
	if rd == nil {
		return nil, fmt.Errorf("macropad: bridge device %q not available", id)
	}
	return &device{path: id, rc: rd}, nil
}
