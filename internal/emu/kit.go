package emu

import (
	"embed"
	"io/fs"
)

// defaultKitFS embeds the built-in "modular-hits" sample kit (48 one-shot pads,
// A1..H6) so the emulator is playable out of the box with no external samples.
// See assets/modular-hits/CREDITS.txt for provenance/licensing.
//
//go:embed assets/modular-hits/*.wav
var defaultKitFS embed.FS

// defaultKitCredits is the built-in kit's provenance/licensing text.
//
//go:embed assets/modular-hits/CREDITS.txt
var defaultKitCredits string

// DefaultKitName is the display/profile name of the built-in sample kit.
const DefaultKitName = "modular-hits (built-in)"

// DefaultKitAttribution is a one-line credit for the built-in kit, suitable for
// compact UI (the info dialog). See DefaultKitCredits for the full text.
const DefaultKitAttribution = "One-shot hits from the “Modular-Hits” pack by publicsamples — https://github.com/publicsamples/Modular-Hits"

// DefaultKitCredits returns the full credits/attribution text for the built-in
// "modular-hits" kit (from its bundled CREDITS.txt).
func DefaultKitCredits() string { return defaultKitCredits }

const defaultKitDir = "assets/modular-hits"

// defaultKitFSSub returns the embedded kit rooted at its directory, so pad files
// appear as "A1.wav".."H6.wav" at the FS root (matching the flat layout the
// sample scanner expects).
func defaultKitFSSub() fs.FS {
	sub, err := fs.Sub(defaultKitFS, defaultKitDir)
	if err != nil {
		// defaultKitDir is a compile-time constant that is embedded above, so
		// this can only fail if the embed pattern is wrong — a build-time bug.
		panic("emu: embedded default kit missing: " + err.Error())
	}
	return sub
}
