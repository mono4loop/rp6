package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

// iconSVG is the rp6 app icon (see cmd/rp6/assets/icon.svg — the canonical
// source; web/icon.png and the Flatpak SVG are regenerated from it by
// `make icon`).
//
//go:embed assets/icon.svg
var iconSVG []byte

// appIcon returns the application/window icon as a Fyne resource. Fyne
// rasterizes the SVG for the window title bar and OS taskbar/dock.
func appIcon() fyne.Resource {
	return fyne.NewStaticResource("rp6.svg", iconSVG)
}
