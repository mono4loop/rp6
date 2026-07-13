//go:build js

package main

// Browser recorder clips remain session-local; WAV export is still available.
func recorderBaseDir() (string, bool) { return "", false }
