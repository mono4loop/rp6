package main

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	"github.com/mono4loop/rp6/internal/ui/layoutlang"
	"github.com/mono4loop/rp6/internal/ui/layoutspec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rp6Registry stands in for the app's real widgets with plain rectangles, so the
// embedded layout can be built without a running UI.
func rp6Registry() layoutspec.Registry {
	r := layoutspec.Registry{}
	for _, id := range []string{"transport", "p6", "fx", "keysfx", "seq", "rec", "keys", "paks", "pads", "vu", "toggles", "pagenav", "status"} {
		r[id] = canvas.NewRectangle(color.White)
	}
	return r
}

// TestDefaultLayoutParses guards that the embedded layout always parses and
// exposes the expected variants + pages — a broken layout would blank the UI.
func TestDefaultLayoutParses(t *testing.T) {
	doc, err := layoutlang.Parse(layoutSource())
	require.NoError(t, err, "embedded layout must parse")
	assert.Equal(t, []string{
		"tablet", "console", "phone", "window",
		"loop-tablet", "loop-console", "loop-phone", "loop-window",
	}, doc.Names())
	assert.Equal(t, []layoutlang.Page{{ID: "play", Label: "PLAY"}, {ID: "loop", Label: "LOOP"}}, doc.Pages(),
		"PLAY and LOOP pages are declared, in order")
}

// TestPageVariantsSelectByFormFactor verifies each page's own variants are
// selected by form factor: the PLAY page block yields window/console/phone/tablet
// and the LOOP page block yields the matching loop-* variant.
func TestPageVariantsSelectByFormFactor(t *testing.T) {
	doc, err := layoutlang.Parse(layoutSource())
	require.NoError(t, err)

	cases := []struct {
		name       string
		bools      map[string]bool
		play, loop string
	}{
		{"window", map[string]bool{"desktop": true}, "window", "loop-window"},
		{"console", map[string]bool{"desktop": true, "fullscreen": true}, "console", "loop-console"},
		{"phone", map[string]bool{"mobile": true}, "phone", "loop-phone"},
		{"tablet", map[string]bool{"mobile": true, "tablet": true}, "tablet", "loop-tablet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := doc.SelectedNameForPage("play", layoutlang.Env{Bools: c.bools})
			require.True(t, ok)
			assert.Equal(t, c.play, got, "PLAY page variant")

			got, ok = doc.SelectedNameForPage("loop", layoutlang.Env{Bools: c.bools})
			require.True(t, ok)
			assert.Equal(t, c.loop, got, "LOOP page variant")
		})
	}
}

// TestDefaultLayoutBuilds checks the embedded layout builds a non-empty tree in
// every environment/state, including the full-screen console variant.
func TestDefaultLayoutBuilds(t *testing.T) {
	doc, err := layoutlang.Parse(layoutSource())
	require.NoError(t, err)
	reg := rp6Registry()

	states := []struct {
		name string
		env  layoutlang.Env
	}{
		{"window", layoutlang.Env{Bools: map[string]bool{"desktop": true, "pads_visible": true}, Nums: map[string]float64{"width": 850}}},
		{"window with p6 rack", layoutlang.Env{Bools: map[string]bool{"desktop": true, "pads_visible": true, "p6_active": true}, Nums: map[string]float64{"width": 850}}},
		{"fullscreen console", layoutlang.Env{Bools: map[string]bool{"desktop": true, "fullscreen": true, "pads_visible": true}, Nums: map[string]float64{"width": 1920}}},
		{"fullscreen console with p6", layoutlang.Env{Bools: map[string]bool{"desktop": true, "fullscreen": true, "pads_visible": true, "p6_active": true}, Nums: map[string]float64{"width": 1920}}},
		{"tablet", layoutlang.Env{Bools: map[string]bool{"mobile": true, "tablet": true, "pads_visible": true}, Nums: map[string]float64{"width": 1600}}},
		{"phone", layoutlang.Env{Bools: map[string]bool{"mobile": true, "pads_visible": true}, Nums: map[string]float64{"width": 400}}},
		{"seq docked", layoutlang.Env{Bools: map[string]bool{"desktop": true, "pads_visible": true, "seq_docked": true}}},
		{"pads hidden", layoutlang.Env{Bools: map[string]bool{"desktop": true, "seq_docked": true}}},
	}
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			obj := layoutspec.Build(reg, doc.Select(s.env))
			require.NotNil(t, obj, "layout should build a non-nil object")
			_, ok := obj.(*fyne.Container)
			require.True(t, ok, "root is a container")
		})
	}
}

// TestFullScreenSelectsConsole verifies the discrete variant selection: desktop
// full screen picks console, desktop windowed picks window, and mobile picks
// phone or tablet by device size (never console).
func TestFullScreenSelectsConsole(t *testing.T) {
	doc, err := layoutlang.Parse(layoutSource())
	require.NoError(t, err)

	cases := []struct {
		name string
		env  layoutlang.Env
		want string
	}{
		{"desktop fullscreen", layoutlang.Env{Bools: map[string]bool{"desktop": true, "fullscreen": true}}, "console"},
		{"desktop windowed", layoutlang.Env{Bools: map[string]bool{"desktop": true, "fullscreen": false}}, "window"},
		{"phone", layoutlang.Env{Bools: map[string]bool{"mobile": true}}, "phone"},
		{"tablet", layoutlang.Env{Bools: map[string]bool{"mobile": true, "tablet": true}}, "tablet"},
		{"phone ignores fullscreen intent", layoutlang.Env{Bools: map[string]bool{"mobile": true, "fullscreen": true}}, "phone"},
		{"tablet ignores fullscreen intent", layoutlang.Env{Bools: map[string]bool{"mobile": true, "tablet": true, "fullscreen": true}}, "tablet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := doc.SelectedName(c.env)
			require.True(t, ok, "a variant should match")
			assert.Equal(t, c.want, got)
		})
	}
}

// TestDefaultRackBlocks guards that the embedded layout defines a `rack` block
// for every rack that build()/buildPadRack recomposes, and that each builds a
// non-nil tree from its sub-widget IDs — so a typo in a rack block (which would
// silently fall back to the Go composition) is caught here.
func TestDefaultRackBlocks(t *testing.T) {
	doc, err := layoutlang.Parse(layoutSource())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"transport", "p6", "fx", "keysfx", "pads", "seq", "rec", "keys", "paks"}, doc.RackNames())

	racks := map[string][]string{
		"transport": {"tempo"},
		"p6":        {"play", "pattern", "delayTime", "delayLevel", "reverbTime", "reverbLevel"},
		"fx":        {"fxRoll", "fxRate"},
		"keysfx":    {"keysFXTone", "keysFXComp", "keysFXChorus", "keysFXDelay", "keysFXReverb"},
		"pads":      {"padFloat", "padListen", "padDensity", "badge", "padGrid"},
		"seq":       {"seqHeader", "seqControls", "seqGrid"},
		"rec":       {"recHeader", "recControls", "recTracks"},
		"keys":      {"keyboardOct", "keyboardKeys"},
		"paks":      {"paksHeader", "paksList"},
	}
	for name, ids := range racks {
		t.Run(name, func(t *testing.T) {
			r := layoutspec.Registry{}
			for _, id := range ids {
				r[id] = canvas.NewRectangle(color.White)
			}
			obj := layoutspec.Build(r, doc.Rack(name, layoutlang.Env{}))
			require.NotNil(t, obj, "rack %q should build from its sub-widget IDs", name)
		})
	}
}

// TestRelayoutUsesDocument drives the real ui through relayout in both form
// factors and asserts it produces content (i.e. the layout pipeline is wired).
func TestRelayoutUsesDocument(t *testing.T) {
	u := newTestUI(t) // build() calls loadLayout + an initial relayout
	require.NotNil(t, u.layoutDoc, "embedded layout parsed in build()")
	require.NotNil(t, u.root, "relayout produced content")
	require.Equal(t, "window", u.activeVariant, "desktop windowed variant")

	// Enter the console layout and re-lay out; still non-nil.
	u.setConsole(true)
	require.NotNil(t, u.root)
	require.Equal(t, "console", u.activeVariant, "console variant when full screen")
}

// TestVUOrientationPerVariant checks the layout file drives the VU meter's
// orientation via a `vu(orientation: …)` property: horizontal in every shipped
// variant (console, phone, window).
func TestVUOrientationPerVariant(t *testing.T) {
	doc, err := layoutlang.Parse(layoutSource())
	require.NoError(t, err)
	reg := rp6Registry()

	orientationFor := func(env layoutlang.Env) (string, bool) {
		var got string
		var seen bool
		layoutspec.BuildConfig(reg, func(id string, props map[string]string) {
			if id == "vu" {
				got, seen = props["orientation"], true
			}
		}, doc.Select(env))
		return got, seen
	}

	cases := []struct {
		name string
		env  layoutlang.Env
		want string
	}{
		{"console", layoutlang.Env{Bools: map[string]bool{"desktop": true, "fullscreen": true, "pads_visible": true}}, "horizontal"},
		{"phone", layoutlang.Env{Bools: map[string]bool{"mobile": true, "pads_visible": true}}, "horizontal"},
		{"window", layoutlang.Env{Bools: map[string]bool{"desktop": true, "pads_visible": true}}, "horizontal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, seen := orientationFor(c.env)
			require.True(t, seen, "vu should be configured in the %s variant", c.name)
			assert.Equal(t, c.want, got)
		})
	}
}

// TestConsoleRevealsFx checks the `show: true` property in the console layout
// reveals the FX rack on entry, and that a manual toggle while the console stays
// shown is respected (not clobbered on the next relayout).
func TestConsoleRevealsFx(t *testing.T) {
	u := newTestUI(t) // starts windowed; fx hidden by default
	require.False(t, u.fxRack.Object().Visible(), "fx hidden by default")

	// Enter the console layout (full screen), via the real toggle path.
	u.setConsole(true)
	assert.True(t, u.fxRack.Object().Visible(), "console reveals fx on entry")

	// Toggling FX off while still in the console must stick (same variant, so the
	// `show:` default is not re-applied on relayout).
	u.setVisible(u.fxRack.Object(), u.padFXBtn, false)
	u.relayout()
	assert.False(t, u.fxRack.Object().Visible(), "manual toggle respected within the console")
}

// TestConsoleShowsKeyboard checks the console force-shows the keyboard on entry
// (taller than windowed) and restores its prior hidden state on leaving.
func TestConsoleShowsKeyboard(t *testing.T) {
	u := newTestUI(t)
	require.False(t, u.keyboardRack.Object().Visible(), "keyboard hidden by default (windowed)")
	windowedH := u.keyboardRack.piano.MinSize().Height
	u.win.Resize(fyne.NewSize(1920, 1080))

	u.setConsole(true)
	assert.True(t, u.keyboardRack.Object().Visible(), "console reveals the keyboard on entry")
	assert.True(t, u.keysBtn.On())
	assert.Greater(t, u.keyboardRack.piano.MinSize().Height, windowedH, "keyboard is taller in the console")

	u.setConsole(false)
	assert.False(t, u.keyboardRack.Object().Visible(), "keyboard returns to its prior hidden state on leaving the console")
	assert.False(t, u.keysBtn.On())
	assert.Equal(t, windowedH, u.keyboardRack.piano.MinSize().Height, "keyboard returns to windowed height")
}

func TestConsoleRepeatedRoundTripKeepsSequencerAvailable(t *testing.T) {
	u := newTestUI(t)
	u.win.Resize(fyne.NewSize(1920, 1080))
	for cycle := range 3 {
		u.setConsole(true)
		assert.True(t, u.seqRack.Object().Visible(), "cycle %d: console shows sequencer", cycle+1)
		u.setConsole(false)
		assert.True(t, u.seqRack.Object().Visible(), "cycle %d: windowed mode also shows the sequencer by default", cycle+1)
	}
}

// TestConsolePreservesRackState reproduces the round-trip bug: the console
// force-shows FX, KEYS and PAKS; leaving it must restore the racks the user had
// before (here: FX/KEYS/PAKS off), not leave them stuck on (which crammed the
// normal layout and pushed the pads off-screen). The sequencer is a shown-by-
// default rack in the window variant, so it stays on across the round trip.
func TestConsolePreservesRackState(t *testing.T) {
	u := newTestUI(t)
	// Default window state: fx / keys / paks / recorder are off; pads + sequencer on.
	require.False(t, u.fxRack.Object().Visible())
	require.False(t, u.keyboardRack.Object().Visible())
	require.False(t, u.paksRack.Object().Visible())
	require.False(t, u.recRack.Object().Visible())
	require.True(t, u.seqRack.Object().Visible(), "sequencer shown by default in windowed mode")
	require.True(t, u.padRackObj.Visible())

	u.toggleConsole() // enter console — force-shows the racks
	assert.True(t, u.fxRack.Object().Visible())
	assert.True(t, u.keyboardRack.Object().Visible(), "console reveals the keyboard")
	assert.True(t, u.paksRack.Object().Visible(), "console reveals the paks rack")
	assert.True(t, u.seqRack.Object().Visible(), "console shows the sequencer")
	assert.False(t, u.recRack.Object().Visible(), "recorder stays off in the console (toggle-only)")

	u.toggleConsole() // back to windowed — restore the prior FX/KEYS/PAKS (off) state
	assert.False(t, u.fxRack.Object().Visible(), "FX restored to off")
	assert.False(t, u.keyboardRack.Object().Visible(), "KEYS restored to off")
	assert.False(t, u.paksRack.Object().Visible(), "PAKS restored to off")
	assert.True(t, u.seqRack.Object().Visible(), "SEQ stays on (windowed default)")
	assert.False(t, u.recRack.Object().Visible(), "REC restored to off")
	assert.False(t, u.padFXBtn.On())
	assert.False(t, u.keysBtn.On())
	assert.False(t, u.paksBtn.On())
	assert.True(t, u.seqBtn.On())
	assert.False(t, u.recBtn.On())
	assert.True(t, u.padRackObj.Visible(), "pads still visible")
}

// TestConsoleKeepsUserToggleWithinConsole checks that turning a force-shown rack
// off while the console stays open is remembered as the state to restore.
func TestConsoleKeepsUserToggleWithinConsole(t *testing.T) {
	u := newTestUI(t)
	u.toggleConsole() // enter — fx forced on
	require.True(t, u.fxRack.Object().Visible())

	// User turns FX off while in the console; it should stay off there.
	u.toggleVisible(u.fxRack.Object(), u.padFXBtn)
	assert.False(t, u.fxRack.Object().Visible())

	u.toggleConsole() // leave — fx should remain off (matches pre-console)
	assert.False(t, u.fxRack.Object().Visible())
}

// TestConsoleReturnsToWindowVariant verifies that leaving the console restores
// the windowed variant along with its window-only defaults (twobank pads, four
// tracks, horizontally-expanded pad rack) — the "use the windowed layout on exit"
// behavior.
func TestConsoleReturnsToWindowVariant(t *testing.T) {
	u := newTestUI(t)
	require.Equal(t, "window", u.activeVariant)
	require.Equal(t, layoutTwoBank, u.padLayout, "window defaults to twobank")

	u.setConsole(true)
	require.Equal(t, "console", u.activeVariant)
	assert.Equal(t, layoutPaged, u.padLayout, "console switches to paged")
	assert.Equal(t, 6, u.seq.Tracks(), "console default track count")

	u.setConsole(false)
	assert.Equal(t, "window", u.activeVariant, "leaving the console returns to the window variant")
	assert.Equal(t, layoutTwoBank, u.padLayout, "window twobank restored on exit")
	assert.Equal(t, 4, u.seq.Tracks(), "window track default restored on exit")

	// The pad rack expands horizontally again in the window variant.
	allocation := fyne.NewSize(1400, 900)
	u.padRackObj.Resize(allocation)
	u.padRackObj.Refresh()
	assert.Equal(t, allocation.Width, fittedContent(u.padRackObj).Size().Width,
		"pad rack expands horizontally after returning to the window variant")
}

// TestToggleFullScreenRelayouts exercises the F11 path against the real widgets:
// toggling full screen must re-lay out without panicking and keep content.
func TestToggleFullScreenRelayouts(t *testing.T) {
	u := newTestUI(t)
	require.NotPanics(t, u.toggleFullScreen)
	require.NotNil(t, u.root, "content after entering full screen")
	require.NotPanics(t, u.toggleFullScreen)
	require.NotNil(t, u.root, "content after leaving full screen")
}

// TestConsoleButtonTogglesLayout checks the bottom-bar CONSOLE button switches
// the console layout on and off and keeps its lit state in sync.
func TestConsoleButtonTogglesLayout(t *testing.T) {
	u := newTestUI(t)
	require.False(t, u.isFullScreen(), "console off by default")
	require.False(t, u.consoleBtn.On())

	u.toggleConsole()
	assert.True(t, u.isFullScreen(), "CONSOLE turns the console layout on")
	assert.True(t, u.consoleBtn.On(), "button lights when console is active")

	u.toggleConsole()
	assert.False(t, u.isFullScreen(), "CONSOLE turns it back off")
	assert.False(t, u.consoleBtn.On())

	// Entering via the F11 path also lights the button (synced in relayout).
	u.toggleFullScreen()
	assert.True(t, u.consoleBtn.On(), "F11 also lights the CONSOLE button")
}

// TestConsoleLayoutSmoke drives the real widgets through the state transitions
// the console layout's conditions react to (racks shown/hidden, sequencer
// docked, pads hidden then floating), asserting every relayout produces content
// without panicking — the closest a headless test gets to running the layout.
func TestConsoleLayoutSmoke(t *testing.T) {
	u := newTestUI(t)

	steps := []struct {
		name string
		do   func()
	}{
		{"show p6 rack", func() { u.toggleP6Rack() }},
		{"show fx", func() { u.toggleVisible(u.fxRack.Object(), u.padFXBtn) }},
		{"dock sequencer", func() { u.onSeqDock(true) }},
		{"undock sequencer", func() { u.onSeqDock(false) }},
		{"hide pads", func() { u.togglePads() }},
		{"show pads", func() { u.togglePads() }},
		{"float pads", func() { u.floatPad() }},
		{"dock pads", func() { u.dockPad() }},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			require.NotPanics(t, s.do)
			u.relayout()
			require.NotNil(t, u.root, "content after %q", s.name)
		})
	}
}

// --- Windowed-mode sequencer + console round-trip (issues reported by the user) ---

// Issue 1: windowed mode enables the sequencer rack by default.
func TestWindowShowsSequencerByDefault(t *testing.T) {
	u := newTestUI(t)
	require.Equal(t, "window", u.activeVariant)
	assert.True(t, u.seqRack.Object().Visible(), "sequencer is shown by default in windowed mode")
	assert.True(t, u.seqBtn.On(), "SEQ toggle is lit")
}

// Issue 2: with the sequencer shown, the window content fits the fixed 850x950
// window (emulator backend — the P-6 hardware rack makes its window a few px
// taller), and the sequencer's last track is not clipped by the pads below it.
func TestWindowContentFitsWithSequencer(t *testing.T) {
	u := newTestUI(t)
	u.useEmu = true
	u.applyBackendGating()
	u.relayout()
	require.True(t, u.seqRack.Object().Visible(), "precondition: sequencer shown")

	min := u.root.MinSize()
	assert.LessOrEqualf(t, min.Width, float32(windowedWidth),
		"window content min width %.0f must fit the fixed %d window", min.Width, windowedWidth)
	assert.LessOrEqualf(t, min.Height, float32(windowedHeight),
		"window content min height %.0f must fit the fixed %d window", min.Height, windowedHeight)

	// Lay out at the window size (two passes so the width-aware sequencer minimum
	// settles) and verify the last track isn't clipped by the scroll viewport.
	for range 2 {
		u.contentHolder.Resize(fyne.NewSize(windowedWidth, windowedHeight))
		u.contentHolder.Refresh()
	}
	last := u.seqRack.blocks[u.seq.Tracks()-1]
	lastBottom := last.Position().Y + last.Size().Height
	assert.LessOrEqualf(t, lastBottom, u.seqRack.trackBox.Size().Height+0.5,
		"last sequencer track (bottom %.1f) fits inside the scroll viewport (%.1f) — not clipped under the pads",
		lastBottom, u.seqRack.trackBox.Size().Height)
}

// Issue 3: leaving the console returns to the window variant and, once the canvas
// settles back to the windowed size, every visible rack lays out inside the
// window (no rack stranded at the previous full-screen geometry).
func TestConsoleExitRelaysOutWindow(t *testing.T) {
	u := newTestUI(t)

	u.setConsole(true)
	u.contentHolder.Resize(fyne.NewSize(1920, 1080)) // full-screen settle
	u.relayout()
	require.Equal(t, "console", u.activeVariant)

	u.setConsole(false)
	// Simulate the compositor shrinking the window back to the windowed size
	// (the async settle) with no explicit relayout — the content must reflow.
	u.contentHolder.Resize(fyne.NewSize(windowedWidth, windowedHeight))
	u.contentHolder.Refresh()

	assert.Equal(t, "window", u.activeVariant, "back to the window variant")
	win := fyne.NewSize(windowedWidth, windowedHeight)
	racks := map[string]fyne.CanvasObject{
		"pads": u.padRackObj, "sequencer": u.seqRack.Object(), "transport": u.transportRack,
		"navigation": u.controlBar, "status": u.statusBar,
	}
	for name, r := range racks {
		if !r.Visible() {
			continue
		}
		assert.LessOrEqualf(t, r.Position().X+r.Size().Width, win.Width+0.5, "%s right edge within window", name)
		assert.LessOrEqualf(t, r.Position().Y+r.Size().Height, win.Height+0.5, "%s bottom edge within window", name)
	}
}
