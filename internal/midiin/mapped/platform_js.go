//go:build js

package mapped

import (
	"errors"
	"io"
)

// ErrUnsupported is returned on the web build, where MIDI input is handled by the
// dedicated Web MIDI driver (internal/midiin/webmidi) rather than the byte-stream
// mapped interpreter.
var ErrUnsupported = errors.New("midimap: mapped controllers are unavailable on the web build")

// Detect never matches on the web build.
func Detect([]string) (string, bool) { return "", false }

func openReader(string) (io.ReadCloser, error) { return nil, ErrUnsupported }
