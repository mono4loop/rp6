//go:build !android && !ios && !js

package main

import (
	"fyne.io/fyne/v2"
	"github.com/go-gl/glfw/v3.4/glfw"

	"code.rbel.co/rubiojr/fade/toplevel"
)

// desktopWindow is the nativeWindow of a desktop build: fade's toplevel package
// for the maximize state, requests, events and bounds, and GLFW's monitors for
// the screen. Every call runs on the UI loop, like all GLFW calls.
type desktopWindow struct {
	win fyne.Window
}

func newNativeWindow(w fyne.Window) nativeWindow {
	return &desktopWindow{win: w}
}

func (d *desktopWindow) Maximized() bool { return toplevel.Maximized(d.win) }
func (d *desktopWindow) Maximize()       { toplevel.SetMaximized(d.win, true) }
func (d *desktopWindow) Restore()        { toplevel.SetMaximized(d.win, false) }

func (d *desktopWindow) OnMaximize(f func(bool)) { toplevel.OnMaximize(d.win, f) }

func (d *desktopWindow) Bounds() ([2]float32, bool) {
	size, ok := toplevel.Bounds(d.win)
	return [2]float32{size.Width, size.Height}, ok
}

// ScreenPixels is the smallest monitor's current mode. GLFW has no
// window-to-monitor mapping and, on Wayland, no work area (its "work area" is
// the whole mode), so callers subtract an estimate or a learned work area
// where Bounds isn't known.
func (d *desktopWindow) ScreenPixels() (int, int, bool) {
	var width, height int
	for _, monitor := range glfw.GetMonitors() {
		mode := monitor.GetVideoMode()
		if mode == nil {
			continue
		}
		if width == 0 || mode.Width*mode.Height < width*height {
			width, height = mode.Width, mode.Height
		}
	}
	return width, height, width > 0
}
