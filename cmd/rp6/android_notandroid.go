//go:build !android

package main

// startAndroidMIDI is a no-op off Android. On the desktop and web the MIDI paths
// are wired directly in main() (ALSA / Web MIDI); only Android needs the
// JNI/USB reader.
func (u *ui) startAndroidMIDI() {}
