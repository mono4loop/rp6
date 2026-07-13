//go:build !capture || js

package audio

// OpenOutput reports that host playback is unavailable. Emulator builds use
// their already-open native/Web Audio sink instead of this output.
func OpenOutput() (Output, error) { return nil, ErrUnavailable }
