package main

// nativeWindow is the seam to the desktop window state Fyne doesn't expose: the
// maximize state and requests, the bounds the compositor suggests, and the
// screen size. A maximized window shows the console layout, the same as full
// screen, so a title-bar double-click switches layouts (see syncMaximized);
// the screen size fits the windowed window to short screens (see
// windowedSize). The desktop implementation is fade's toplevel package plus
// GLFW's monitors (nativewin_desktop.go); web and mobile get nil, never report
// a maximized window and keep the design size. Tests substitute a fake.
type nativeWindow interface {
	// Maximized reports whether the window is maximized right now.
	Maximized() bool
	// Maximize and Restore ask the compositor to maximize or un-maximize the
	// window. Both are asynchronous: the state and size change arrive later,
	// and OnMaximize reports them.
	Maximize()
	Restore()
	// OnMaximize calls f, on the UI loop, whenever the window is maximized or
	// restored, by the user or by Maximize/Restore.
	OnMaximize(f func(maximized bool))
	// Bounds is the largest content size the compositor suggests for the
	// window: its work area. ok is false where it isn't known (GNOME, whose
	// libdecor keeps it; X11; before the window shows).
	Bounds() (size [2]float32, ok bool)
	// ScreenPixels is the smallest monitor's mode in physical pixels; ok is
	// false when no monitor is known.
	ScreenPixels() (width, height int, ok bool)
}
