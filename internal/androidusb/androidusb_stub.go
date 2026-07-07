//go:build !android

package androidusb

// Start is a no-op off Android. USB-MIDI host access is Android-only; every
// other platform reaches MIDI through ALSA (desktop) or Web MIDI (web).
func Start(logf func(string)) {}
