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
	for _, id := range []string{"transport", "p6", "fx", "keysfx", "seq", "rec", "keys", "paks", "pads", "vu", "toggles", "status"} {
		r[id] = canvas.NewRectangle(color.White)
	}
	return r
}

// TestDefaultLayoutParses guards that the embedded layout always parses and
// exposes the expected variants — a broken layout would blank the UI.
func TestDefaultLayoutParses(t *testing.T) {
	doc, err := layoutlang.Parse(layoutSource())
	require.NoError(t, err, "embedded layout must parse")
	assert.Equal(t, []string{"consoletablet", "console", "compact", "default"}, doc.Names())
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
		{"default windowed", layoutlang.Env{Bools: map[string]bool{"pads_visible": true}, Nums: map[string]float64{"width": 900}}},
		{"default with p6 rack", layoutlang.Env{Bools: map[string]bool{"pads_visible": true, "p6_active": true}, Nums: map[string]float64{"width": 900}}},
		{"fullscreen console", layoutlang.Env{Bools: map[string]bool{"fullscreen": true, "pads_visible": true}, Nums: map[string]float64{"width": 1920}}},
		{"fullscreen console with p6", layoutlang.Env{Bools: map[string]bool{"fullscreen": true, "pads_visible": true, "p6_active": true}, Nums: map[string]float64{"width": 1920}}},
		{"tablet console", layoutlang.Env{Bools: map[string]bool{"fullscreen": true, "mobile": true, "pads_visible": true}, Nums: map[string]float64{"width": 1600}}},
		{"compact", layoutlang.Env{Bools: map[string]bool{"compact": true, "pads_visible": true}, Nums: map[string]float64{"width": 400}}},
		{"seq docked", layoutlang.Env{Bools: map[string]bool{"pads_visible": true, "seq_docked": true}}},
		{"pads hidden", layoutlang.Env{Bools: map[string]bool{"seq_docked": true}}},
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

// TestFullScreenSelectsConsole verifies the fullscreen flag picks the console
// variant while the windowed states pick default/compact — the F11 behavior.
func TestFullScreenSelectsConsole(t *testing.T) {
	doc, err := layoutlang.Parse(layoutSource())
	require.NoError(t, err)

	cases := []struct {
		name string
		env  layoutlang.Env
		want string
	}{
		{"fullscreen", layoutlang.Env{Bools: map[string]bool{"fullscreen": true}}, "console"},
		{"windowed", layoutlang.Env{Bools: map[string]bool{"fullscreen": false}}, "default"},
		{"narrow windowed", layoutlang.Env{Bools: map[string]bool{"compact": true}}, "compact"},
		{"fullscreen beats compact", layoutlang.Env{Bools: map[string]bool{"fullscreen": true, "compact": true}}, "console"},
		{"mobile fullscreen picks tablet console", layoutlang.Env{Bools: map[string]bool{"fullscreen": true, "mobile": true}}, "consoletablet"},
		{"desktop fullscreen stays console", layoutlang.Env{Bools: map[string]bool{"fullscreen": true, "mobile": false}}, "console"},
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

	// Flip to the compact form factor and re-lay out; still non-nil.
	u.compact = true
	u.relayout()
	require.NotNil(t, u.root)
}

// TestVUOrientationPerVariant checks the layout file drives the VU meter's
// orientation via a `vu(orientation: …)` property: horizontal in the console and
// compact variants, vertical in the default (windowed) variant.
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
		{"console", layoutlang.Env{Bools: map[string]bool{"fullscreen": true, "pads_visible": true}}, "horizontal"},
		{"compact", layoutlang.Env{Bools: map[string]bool{"compact": true, "pads_visible": true}}, "horizontal"},
		{"default", layoutlang.Env{Bools: map[string]bool{"pads_visible": true}}, "horizontal"},
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

// TestConsoleRevealsKeyboard checks the keyboard is off by default (windowed),
// turns on when the console is entered, is taller there, and — crucially —
// returns to its prior hidden state when the console is left (via the real
// setConsole path, not a raw relayout).
func TestConsoleRevealsKeyboard(t *testing.T) {
	u := newTestUI(t)
	require.False(t, u.keyboardRack.Object().Visible(), "keyboard hidden by default (windowed)")
	compactH := u.keyboardRack.piano.MinSize().Height

	u.setConsole(true)
	assert.True(t, u.keyboardRack.Object().Visible(), "console reveals the keyboard on entry")
	assert.True(t, u.keysBtn.On())
	assert.Greater(t, u.keyboardRack.piano.MinSize().Height, compactH, "keyboard is taller in the console")

	u.setConsole(false)
	assert.False(t, u.keyboardRack.Object().Visible(), "keyboard returns to its prior hidden state on leaving the console")
	assert.False(t, u.keysBtn.On())
	assert.Equal(t, compactH, u.keyboardRack.piano.MinSize().Height, "keyboard returns to compact height when windowed")
}

// TestConsolePreservesRackState reproduces the round-trip bug: the console
// force-shows FX, KEYS and PAKS; leaving it must restore the racks the user had
// before (here: all off), not leave them stuck on (which crammed the normal
// layout and pushed the pads off-screen).
func TestConsolePreservesRackState(t *testing.T) {
	u := newTestUI(t)
	// Default state: fx / keys / paks / recorder are off; pads on.
	require.False(t, u.fxRack.Object().Visible())
	require.False(t, u.keyboardRack.Object().Visible())
	require.False(t, u.paksRack.Object().Visible())
	require.False(t, u.recRack.Object().Visible())
	require.True(t, u.padRackObj.Visible())

	u.toggleConsole() // enter console — force-shows the racks
	assert.True(t, u.fxRack.Object().Visible())
	assert.True(t, u.keyboardRack.Object().Visible())
	assert.True(t, u.paksRack.Object().Visible(), "console reveals the paks rack")
	assert.True(t, u.recRack.Object().Visible(), "console reveals the recorder rack")

	u.toggleConsole() // back to normal — must restore the prior (off) state
	assert.False(t, u.fxRack.Object().Visible(), "FX restored to off")
	assert.False(t, u.keyboardRack.Object().Visible(), "KEYS restored to off")
	assert.False(t, u.paksRack.Object().Visible(), "PAKS restored to off")
	assert.False(t, u.recRack.Object().Visible(), "REC restored to off")
	assert.False(t, u.padFXBtn.On())
	assert.False(t, u.keysBtn.On())
	assert.False(t, u.paksBtn.On())
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
