package main

import (
	_ "embed"
	"log"

	"fyne.io/fyne/v2"

	"github.com/mono4loop/rp6/internal/ui/layoutlang"
	"github.com/mono4loop/rp6/internal/ui/layoutspec"
)

// defaultLayoutSrc holds the DEFAULT (original top-bar) layout variants plus the
// shared `rack …` blocks. consoleLayoutSrc holds the full-screen "mixing
// console" variant; consoleTabletLayoutSrc the tablet console variant. All are
// compiled into the binary (edit the files and rebuild to change the UI); see
// layoutSource for how they combine.
//
//go:embed assets/default.layout
var defaultLayoutSrc string

//go:embed assets/console.layout
var consoleLayoutSrc string

//go:embed assets/console-tablet.layout
var consoleTabletLayoutSrc string

// layoutSource is the full layout program: the tablet console FIRST (its
// `when fullscreen && mobile` guard is more specific than the desktop console's
// `when fullscreen`, and Select takes the first match), then the desktop console,
// then the default file's variants and the shared rack blocks. Concatenation is
// valid because a document is just a sequence of `layout`/`rack` blocks.
func layoutSource() string {
	return consoleTabletLayoutSrc + "\n" + consoleLayoutSrc + "\n" + defaultLayoutSrc
}

// loadLayout parses the embedded layout into u.layoutDoc. It does no I/O, so it's
// safe to call from build() (including tests), and guarantees relayout always has
// a document to work with.
func (u *ui) loadLayout() {
	doc, err := layoutlang.Parse(layoutSource())
	if err != nil {
		// The embedded layout must always parse (a test guards this); log and
		// continue with a nil doc (relayout has a minimal fallback).
		log.Printf("rp6: built-in layout failed to parse: %v", err)
		return
	}
	u.layoutDoc = doc
}

// selectLayout compiles the active layout document for the current environment,
// resolving all `when`/`if` conditions. The environment carries the boot-time
// platform (mobile/web/desktop — decided at compile time by build tags) and the
// live form factor (compact + window width/height) plus UI state, so the layout
// can adapt to the device it launched on. Returns nil if there's no document or
// no variant matches (relayout then uses a minimal fallback).
func (u *ui) selectLayout(reg layoutspec.Registry) fyne.CanvasObject {
	if u.layoutDoc == nil {
		return nil
	}
	size := u.canvasSize()
	env := layoutlang.Env{
		Bools: map[string]bool{
			// Platform, detected at boot (compile-time build tags).
			"mobile":  onMobile,
			"web":     onWeb,
			"desktop": !onMobile && !onWeb,
			// Live form factor + UI state.
			"fullscreen":   u.isFullScreen(),
			"compact":      u.compact,
			"seq_docked":   u.seqSide && u.seqRack.Object().Visible(),
			"pads_visible": u.padRackObj.Visible() && !u.padFloating,
			// The P-6-only rack (transport + PATTERN + Delay/Reverb) is laid out
			// only when the P-6 is the active backend; its controls are no-ops on
			// the emulator (see applyBackendGating).
			"p6_active": !u.useEmu,
		},
		Nums: map[string]float64{
			"width":  float64(size.Width),
			"height": float64(size.Height),
		},
	}
	// Detect a variant switch so `show:` properties apply only on entry (see
	// configureComponent). Select and SelectedName evaluate the same variants.
	name, _ := u.layoutDoc.SelectedName(env)
	u.variantChanged = name != u.activeVariant
	u.activeVariant = name
	root := layoutspec.BuildConfig(reg, u.configureComponent, u.layoutDoc.Select(env))
	u.variantChanged = false
	return root
}

// configureComponent applies a component's layout-file properties (from a
// `name(key: value)` ref) to the actual widget. It's the app-specific half of
// the generic layoutspec property mechanism: layoutspec carries the strings, the
// app knows what they mean.
//
//   - vu(orientation: horizontal|vertical) — the VU meter's orientation (always).
//   - fx/keys(show: true|false)            — the rack's default visibility, set
//     only when the layout variant is entered (u.variantChanged), so the user's
//     FX/KEYS toggles still work while that variant is showing.
func (u *ui) configureComponent(id string, props map[string]string) {
	switch id {
	case "vu":
		if o, ok := props["orientation"]; ok {
			u.applyMeterOrientation(o == "horizontal")
		}
	case "fx":
		u.applyRackShow("fx", props, u.fxRack.Object(), u.fxBtn)
	case "keys":
		u.applyRackShow("keys", props, u.keyboardRack.Object(), u.keysBtn)
	}
}

// isFullScreen reports whether the "mixing console" layout is active — our own
// intent (set by toggleFullScreen on desktop via F11 / Ctrl+Shift+Enter, or by
// the CONSOLE button on any platform), NOT the window's FullScreen() flag. On
// mobile it's off by default (the console is a wide layout that doesn't suit a
// phone), but a user on a large tablet can turn it on with the CONSOLE button.
func (u *ui) isFullScreen() bool {
	return u.fullScreen
}

// recomposeRack arranges a rack's *internal* controls from its `rack NAME { … }`
// block in the layout document, resolving the rack's sub-widget IDs against reg.
// It returns nil when the document has no block for name — the caller then keeps
// its own Go-built composition, so rack blocks are optional overrides. This is
// what lets the layout file lay out the controls *inside* a rack, not just
// position the racks. Called once per rack at build (and on pad float/dock).
func (u *ui) recomposeRack(name string, reg layoutspec.Registry) fyne.CanvasObject {
	if u.layoutDoc == nil {
		return nil
	}
	node := u.layoutDoc.Rack(name, u.rackEnv())
	if node == nil {
		return nil
	}
	return layoutspec.BuildConfig(reg, u.configureComponent, node)
}

// composeRack builds a rack's object from its DSL `rack NAME { … }` block if
// present, otherwise from the Go fallback — NEVER both. Building both would put
// the rack's sub-widgets into two container trees at once, and Fyne keys the
// object→canvas association 1:1 (LoadOrStore): the throwaway tree corrupts
// refresh routing / textures, which the mobile (Android) driver renders as
// missing widgets even though desktop tolerates it. The fallback is only invoked
// when there's no rack block, so each sub-widget is parented exactly once.
func (u *ui) composeRack(name string, reg layoutspec.Registry, fallback func() fyne.CanvasObject) fyne.CanvasObject {
	if o := u.recomposeRack(name, reg); o != nil {
		return o
	}
	return fallback()
}

// rackEnv is the condition environment for rack-internal blocks. Rack internals
// are composed once (not on every resize), so only the boot-time platform flags
// are meaningful here.
func (u *ui) rackEnv() layoutlang.Env {
	return layoutlang.Env{Bools: map[string]bool{
		"mobile":  onMobile,
		"web":     onWeb,
		"desktop": !onMobile && !onWeb,
	}}
}
