//go:build linux && !android && wayland && !x11

package main

/*
#cgo pkg-config: wayland-client
#include <stdint.h>
#include <wayland-client.h>

// rp6_glfw_window returns the GLFWwindow behind a GLFW-created wl_surface: GLFW
// registers the surface listener with the window as its user data
// (wl_window.c, createNativeSurface).
static void *rp6_glfw_window(uintptr_t surface) {
	return wl_proxy_get_user_data((struct wl_proxy *)surface);
}
*/
import "C"

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/go-gl/glfw/v3.4/glfw"
)

// glfwWindow reads and drives the maximize state of a Fyne window's GLFW
// window, and reads the monitors. Fyne keeps that window private but hands out
// its wl_surface, and GLFW keeps itself as the surface's user data, so the
// lookup needs no Fyne internals. It relies on that GLFW detail; the fork pinned
// in go.mod has it. Every call must run on the main thread (Fyne's UI loop),
// like all GLFW calls.
type glfwWindow struct {
	win  fyne.Window
	view *glfw.Window // resolved lazily: the GLFW window exists once shown
}

func newNativeWindow(w fyne.Window) nativeWindow {
	return &glfwWindow{win: w}
}

func (m *glfwWindow) viewport() *glfw.Window {
	if m.view != nil {
		return m.view
	}
	nw, ok := m.win.(driver.NativeWindow)
	if !ok {
		return nil
	}
	var surface uintptr
	nw.RunNative(func(ctx any) {
		if wc, ok := ctx.(driver.WaylandWindowContext); ok {
			surface = wc.WaylandSurface
		}
	})
	if surface == 0 {
		return nil // not created yet
	}
	p := C.rp6_glfw_window(C.uintptr_t(surface))
	if p == nil {
		return nil
	}
	m.view = glfw.GoWindow(p)
	return m.view
}

func (m *glfwWindow) Maximized() bool {
	v := m.viewport()
	return v != nil && v.GetAttrib(glfw.Maximized) == glfw.True
}

func (m *glfwWindow) Maximize() {
	if v := m.viewport(); v != nil {
		v.Maximize()
	}
}

func (m *glfwWindow) Restore() {
	if v := m.viewport(); v != nil {
		v.Restore()
	}
}

// ScreenPixels is the smallest monitor's current mode. GLFW on Wayland has no
// window-to-monitor mapping and no work area (its "work area" is the whole
// mode), so callers subtract an estimate or a learned work area.
func (m *glfwWindow) ScreenPixels() (int, int, bool) {
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
