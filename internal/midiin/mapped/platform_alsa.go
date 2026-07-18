//go:build !android && !js

package mapped

import (
	"fmt"
	"io"
	"os"

	"github.com/mono4loop/rp6/internal/midiin"
)

// Detect reports whether a controller whose ALSA card name matches any of the
// map's substrings is present, returning its rawmidi node path.
func Detect(match []string) (string, bool) { return midiin.FindRawMIDI(match...) }

// openReader opens a controller's rawmidi node read-only. It is a distinct USB
// device from the P-6, so this never conflicts with the P-6's exclusive node.
func openReader(path string) (io.ReadCloser, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("midimap: opening %s: %w", path, err)
	}
	return f, nil
}
