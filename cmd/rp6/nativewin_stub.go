//go:build !(linux && !android && wayland && !x11)

package main

import "fyne.io/fyne/v2"

// newNativeWindow has no way to see the maximize state or the screen outside the
// native Wayland build (X11, web, mobile), so the window never counts as
// maximized there, only full screen selects the console layout, and the window
// keeps the design size.
func newNativeWindow(fyne.Window) nativeWindow { return nil }
