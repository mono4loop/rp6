package main

// nativeWindow is the seam to the desktop window state Fyne doesn't expose: the
// maximize state (no maximize API, no maximize event) and the screen size. A
// maximized window shows the console layout, the same as full screen, so a
// title-bar double-click switches layouts (see syncMaximized); the screen size
// fits the windowed window to short screens (see windowedSize). The real
// implementation reaches GLFW through the window's Wayland surface
// (nativewin_wayland.go); other builds get nil, never report a maximized window
// and keep the design size. Tests substitute a fake.
type nativeWindow interface {
	// Maximized reports whether the window is maximized right now.
	Maximized() bool
	// Maximize and Restore ask the compositor to maximize or un-maximize the
	// window. Both are asynchronous: the state and size change arrive later.
	Maximize()
	Restore()
	// ScreenPixels is the smallest monitor's mode in physical pixels; ok is
	// false when no monitor is known.
	ScreenPixels() (width, height int, ok bool)
}
