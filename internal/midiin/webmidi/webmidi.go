//go:build !js

// Package webmidi is the rp6 MIDI-input driver for external controllers (e.g.
// the Adafruit MacroPad) over the browser's Web MIDI API. It is only built for
// the js/wasm target; on other platforms this file makes the package a no-op so
// `go build ./...` succeeds (the driver lives in webmidi_js.go).
package webmidi
