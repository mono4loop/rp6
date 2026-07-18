//go:build android

package mapped

import (
	"fmt"
	"io"
	"strings"

	"github.com/mono4loop/rp6/midibridge"
)

// Detect scans the Android MIDI bridge's inputs for a device whose port name
// matches any of the map's substrings, returning its bridge id (the "path" Open
// reopens) — the input-side analogue of the ALSA rawmidi node.
func Detect(match []string) (string, bool) {
	for i := 0; i < midibridge.InputCount(); i++ {
		name := strings.ToLower(midibridge.InputName(i))
		for _, m := range match {
			if m != "" && strings.Contains(name, m) {
				return midibridge.InputID(i), true
			}
		}
	}
	return "", false
}

// openReader opens the bridge reader for a device id.
func openReader(id string) (io.ReadCloser, error) {
	rd := midibridge.OpenReader(id)
	if rd == nil {
		return nil, fmt.Errorf("midimap: bridge device %q not available", id)
	}
	return rd, nil
}
