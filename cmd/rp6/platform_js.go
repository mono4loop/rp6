//go:build js

package main

// onWeb reports whether this is the browser (wasm) build. On the web there is
// no ALSA/USB access or filesystem, so the app runs the built-in emulator
// (Web Audio) and skips hardware MIDI, USB-audio capture and device watching.
const onWeb = true
