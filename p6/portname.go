//go:build js || android

package p6

import "strings"

// isP6Name reports whether a MIDI port name looks like a P-6. Shared by the
// port-name-matching backends (Web MIDI in device_js.go and Android MIDI in
// device_android.go); the ALSA backend matches on the ALSA card name instead.
// Built only for those targets so it isn't flagged unused on the desktop build.
func isP6Name(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "p-6") || strings.Contains(n, "p6")
}
