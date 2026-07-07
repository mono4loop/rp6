//go:build android || ios

package main

// onMobile reports whether this is a mobile (Android/iOS) build. On mobile there
// is no P-6 USB access, so the app runs the built-in emulator (audio via the
// malgo/miniaudio sink, built with -tags capture) and skips USB-audio capture
// (which would otherwise request the microphone permission).
const onMobile = true
