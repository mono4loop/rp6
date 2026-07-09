//go:build android

package main

import (
	"log"
	"strings"

	"fyne.io/fyne/v2"

	"github.com/mono4loop/rp6/internal/androidusb"
)

// startAndroidMIDI wires up USB MIDI on Android. Unlike the desktop (ALSA) and
// web (Web MIDI) paths, Android can't touch /dev/snd, so we read plugged-in
// USB-MIDI gear (a MacroPad, a P-6, …) straight from Go over JNI (androidusb),
// which feeds the midibridge package that the p6 + midiin backends consume.
//
// The app starts on the built-in emulator (like a desktop with no P-6), marked
// as a fallback so the device watcher promotes to a P-6 if one is read. Because
// USB devices appear asynchronously (the OS shows a permission dialog first),
// the external-controller attach is retried by the shared MIDI-input watcher
// (which also handles hot-plugging/swapping a controller later).
func (u *ui) startAndroidMIDI() {
	if u.useEmu && strings.TrimSpace(u.emuDir) == "" {
		u.emuFallback.Store(true)
	}

	androidusb.Start(func(s string) {
		log.Print(s)
		fyne.Do(func() { u.setStatus(s) })
	})

	u.startDeviceWatch()
	u.startMIDIInputWatch()
}
