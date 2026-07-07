//go:build !js

package main

// onWeb reports whether this is the browser (wasm) build. On the desktop build
// it is false, so hardware MIDI, USB-audio capture and device watching all run
// normally.
const onWeb = false
