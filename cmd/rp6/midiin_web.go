//go:build js

package main

// On the web the MacroPad (and other controllers) arrive over the Web MIDI API,
// so register that input driver. The ALSA-based macropad driver is also imported
// (below, for all platforms); its Detect simply finds nothing in a browser.
import _ "github.com/mono4loop/rp6/internal/midiin/webmidi"
