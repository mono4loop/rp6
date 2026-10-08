//go:build android || ios || js

package main

import "fyne.io/fyne/v2"

// newNativeWindow has no window frame to drive on the web or on mobile, so the
// window never counts as maximized there, only full screen selects the console
// layout, and the window keeps the design size.
func newNativeWindow(fyne.Window) nativeWindow { return nil }
