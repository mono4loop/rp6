package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/theme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gnomeChrome is the logical height GNOME takes from a maximized window: the
// top bar (32) plus the libdecor title bar (37), measured on the ThinkPad X13 at
// 1.25 (a 1536x960 screen maximizes to 1536x891).
const gnomeChrome = 69

// desktopScreen is one modern desktop display rp6 must fit: its panel in
// physical pixels and the desktop scale. resolutions.txt lists the same rows.
type desktopScreen struct {
	name        string
	width       int
	height      int
	scale       float32
	unsupported string // why the screen can't be supported (skipped by the budget test)
}

func (s desktopScreen) label() string {
	return fmt.Sprintf("%dx%d@%g", s.width, s.height, s.scale)
}

// logical is the screen in Fyne (compositor logical) units.
func (s desktopScreen) logical() fyne.Size {
	return fyne.NewSize(float32(s.width)/s.scale, float32(s.height)/s.scale)
}

// screenMatrix is the set of desktop screens every desktop variant must fit:
// the window variant in the windowed size, the console both maximized (the
// screen less gnomeChrome) and full screen.
var screenMatrix = []desktopScreen{
	{name: "hd-laptop", width: 1366, height: 768, scale: 1, unsupported: "neither variant fits 1366x768 at 1x: the console needs ~1600 logical width and the window ~800 height"},
	{name: "fhd", width: 1920, height: 1080, scale: 1},
	{name: "fhd-125", width: 1920, height: 1080, scale: 1.25},
	{name: "thinkpad-x13-100", width: 1920, height: 1200, scale: 1},
	{name: "thinkpad-x13-125", width: 1920, height: 1200, scale: 1.25},
	{name: "framework-13", width: 2256, height: 1504, scale: 1.5},
	{name: "qhd", width: 2560, height: 1440, scale: 1},
	{name: "qhd-125", width: 2560, height: 1440, scale: 1.25},
	{name: "wqxga-2x", width: 2560, height: 1600, scale: 2},
	{name: "retina-2x", width: 2880, height: 1800, scale: 2},
	{name: "uhd-150", width: 3840, height: 2160, scale: 1.5},
	{name: "uhd-2x", width: 3840, height: 2160, scale: 2},
	{name: "ultrawide", width: 3440, height: 1440, scale: 1},
}

// screenBudget is the canvas a variant gets on a screen in one window mode.
func screenBudget(s desktopScreen, mode string) fyne.Size {
	l := s.logical()
	switch mode {
	case "window":
		return windowedSizeFor(l, gnomeChrome)
	case "maximized":
		return fyne.NewSize(l.Width, l.Height-gnomeChrome)
	default: // "fullscreen"
		return l
	}
}

// TestContentFitsScreens is the screen budget gate: every desktop variant's
// content minimum (both backends, both pages, default rack visibility) plus the
// window padding must fit the canvas it gets on every supported screen. A
// minimum over the budget means Fyne grows the window past the screen and
// Mutter refuses to size a maximize (the title-bar double-click bug), or full
// screen squeezes racks below their minimum.
func TestContentFitsScreens(t *testing.T) {
	pad := 2 * theme.Padding()
	for _, s := range screenMatrix {
		t.Run(s.name+"-"+s.label(), func(t *testing.T) {
			if s.unsupported != "" {
				t.Skip(s.unsupported)
			}
			for _, page := range []string{"play", "loop"} {
				for _, emu := range []bool{true, false} {
					for _, mode := range []string{"window", "maximized", "fullscreen"} {
						budget := screenBudget(s, mode)
						minSize := measureContentMin(t, s.scale, budget, page, emu, mode)
						name := fmt.Sprintf("%s page, emu=%v, %s", page, emu, mode)
						assert.LessOrEqualf(t, minSize.Width+pad, budget.Width+0.5,
							"%s: content min width %.1f (+%.0f padding) exceeds the %.1f budget", name, minSize.Width, pad, budget.Width)
						assert.LessOrEqualf(t, minSize.Height+pad, budget.Height+0.5,
							"%s: content min height %.1f (+%.0f padding) exceeds the %.1f budget", name, minSize.Height, pad, budget.Height)
					}
				}
			}
		})
	}
}

// measureContentMin builds the UI headlessly at a scale and canvas size in one
// window mode and returns the content minimum.
func measureContentMin(t *testing.T, scale float32, canvas fyne.Size, page string, emu bool, mode string) fyne.Size {
	t.Helper()
	u, w := newInspectionUI(t)
	u.useEmu = emu
	u.applyBackendGating()
	u.activePage = page
	switch mode {
	case "maximized":
		u.maximized = true
	case "fullscreen":
		u.fullScreen = true
	}
	c, ok := w.Canvas().(software.WindowlessCanvas)
	require.True(t, ok)
	c.SetScale(scale)
	w.Resize(canvas)
	u.relayout()
	// Two passes: the sequencer's minimum depends on the width it was laid out at.
	for range 2 {
		u.contentHolder.Refresh()
		w.Resize(canvas)
	}
	return u.contentHolder.MinSize()
}

// TestResolutionsListsScreens keeps resolutions.txt (the human list) in step
// with screenMatrix.
func TestResolutionsListsScreens(t *testing.T) {
	data, err := os.ReadFile("../../resolutions.txt")
	require.NoError(t, err)
	text := string(data)
	for _, s := range screenMatrix {
		assert.Truef(t, strings.Contains(text, s.label()), "resolutions.txt lists %s (%s)", s.label(), s.name)
	}
}
