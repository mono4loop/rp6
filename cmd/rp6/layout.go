package main

import (
	_ "embed"
	"log"
	"strconv"

	"fyne.io/fyne/v2"

	"github.com/mono4loop/rp6/internal/ui/components"
	"github.com/mono4loop/rp6/internal/ui/layoutlang"
	"github.com/mono4loop/rp6/internal/ui/layoutspec"
)

// defaultLayoutSrc holds the PLAY page (its `page play PLAY { … }` block) plus
// the shared `rack …` blocks; loopLayoutSrc holds the LOOP page. Both are
// compiled into the binary (edit the files and rebuild to change the UI); see
// layoutSource for how they combine.
//
//go:embed assets/default.layout
var defaultLayoutSrc string

//go:embed assets/loop.layout
var loopLayoutSrc string

// layoutSource is the full layout program: the PLAY page (default.layout) first,
// then the LOOP page (loop.layout), so Pages() reports them in that order (the
// nav shows PLAY then LOOP). Each page is a self-contained `page … { … }` block,
// so concatenation just appends pages; rack blocks are top-level and shared.
func layoutSource() string {
	return defaultLayoutSrc + "\n" + loopLayoutSrc
}

// loadLayout parses the embedded layout into u.layoutDoc. It does no I/O, so it's
// safe to call from build() (including tests), and guarantees relayout always has
// a document to work with. It also records the document's declared pages and
// resolves the initial active page (remembered, else the first declared page).
func (u *ui) loadLayout() {
	doc, err := layoutlang.Parse(layoutSource())
	if err != nil {
		// The embedded layout must always parse (a test guards this); log and
		// continue with a nil doc (relayout has a minimal fallback).
		log.Printf("rp6: built-in layout failed to parse: %v", err)
		return
	}
	u.layoutDoc = doc
	u.pages = doc.Pages()
	u.initActivePage()
}

// initActivePage sets u.activePage to the remembered page when it is still a
// declared page, otherwise the first declared page (empty when the document
// declares none — the app then runs single-page). Called from loadLayout, once
// the page list is known.
func (u *ui) initActivePage() {
	if u.pageValid(u.activePage) {
		return // already a valid page (e.g. preset by an inspection scene)
	}
	if saved := savedPage(); u.pageValid(saved) {
		u.activePage = saved
		return
	}
	if len(u.pages) > 0 {
		u.activePage = u.pages[0].ID
		return
	}
	u.activePage = ""
}

// pageValid reports whether id names one of the document's declared pages.
func (u *ui) pageValid(id string) bool {
	if id == "" {
		return false
	}
	for _, pg := range u.pages {
		if pg.ID == id {
			return true
		}
	}
	return false
}

// layoutEnv builds the condition environment for variant selection at a given
// canvas size. It carries the boot-time platform (mobile/web/desktop — decided at
// compile time by build tags) and the discrete form factor the size resolves to
// (a tablet-class touchscreen, plus the fullscreen/console intent) so a size maps
// to exactly one designed variant. There is deliberately no continuous "compact"
// adaptation: windowed desktop is a single fixed size, mobile is phone-or-tablet
// by device size, and desktop fullscreen is the console. See docs/architecture/
// layouts.md (fixed-form-factor policy) and resolutions.txt for the target set.
func (u *ui) layoutEnv(size fyne.Size) layoutlang.Env {
	mobile := onMobile
	if u.mobileForTest != nil {
		mobile = *u.mobileForTest
	}
	tablet := isTabletSize(size)
	if u.tabletForTest != nil {
		tablet = *u.tabletForTest
	}
	env := layoutlang.Env{
		Bools: map[string]bool{
			// Platform, detected at boot (compile-time build tags).
			"mobile":  mobile,
			"web":     onWeb,
			"desktop": !mobile && !onWeb,
			// Discrete form factor + UI state.
			"fullscreen":   u.isFullScreen(),
			"tablet":       tablet,
			"seq_docked":   u.seqSide,
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
	return env
}

// selectLayout compiles the active layout document for the current environment,
// resolving all `when`/`if` conditions. Returns nil if there's no document or no
// variant matches (relayout then uses a minimal fallback).
func (u *ui) selectLayout(reg layoutspec.Registry) fyne.CanvasObject {
	if u.layoutDoc == nil {
		return nil
	}
	env := u.layoutEnv(u.canvasSize())
	// Detect a variant switch so `show:` properties apply only on entry (see
	// configureComponent). SelectForPage and SelectedNameForPage evaluate the
	// active page's variants.
	name, _ := u.layoutDoc.SelectedNameForPage(u.activePage, env)
	u.variantChanged = name != u.activeVariant
	u.activeVariant = name
	root := layoutspec.BuildConfig(reg, u.configureComponent, u.layoutDoc.SelectForPage(u.activePage, env))
	u.variantChanged = false
	return root
}

// variantFor reports which layout variant a given canvas size would select,
// without building it. onCanvasResize uses it to relayout only when the discrete
// form factor actually changes (e.g. a tablet's first real size, or entering the
// desktop console), rather than continuously adapting to every pixel.
func (u *ui) variantFor(size fyne.Size) string {
	if u.layoutDoc == nil {
		return ""
	}
	name, _ := u.layoutDoc.SelectedNameForPage(u.activePage, u.layoutEnv(size))
	return name
}

// configureComponent applies a component's layout-file properties (from a
// `name(key: value)` ref) to the actual widget. It's the app-specific half of
// the generic layoutspec property mechanism: layoutspec carries the strings, the
// app knows what they mean.
//
//   - vu(orientation: horizontal|vertical) — the VU meter's orientation (always).
//   - fx/keys/paks(show: true|false)        — the rack's default visibility, set
//     only when the layout variant is entered (u.variantChanged), so the user's
//     toggles still work while that variant is showing.
//   - seq(tracks: N)                         — the sequencer's default track count
//     for this variant (also variant-entry only).
//   - pads(layout: paged|twobank|dense)      — the pad grid's default paging for
//     this variant (variant-entry only; doesn't clobber the saved preference).
//   - pads/seq(expand: horizontal|vertical|both) — fill that axis with the rack
//     frame (children stay natural size); applied every relayout.
func (u *ui) configureComponent(id string, props map[string]string) {
	switch id {
	case "vu":
		if o, ok := props["orientation"]; ok {
			u.applyMeterOrientation(o == "horizontal")
		}
	case "fx":
		u.applyRackShow("fx", props, u.fxRack.Object(), u.padFXBtn)
	case "keysfx":
		u.applyRackShow("keysfx", props, u.keyboardFXRack.Object(), u.keysFXBtn)
	case "keys":
		u.applyRackShow("keys", props, u.keyboardRack.Object(), u.keysBtn)
	case "paks":
		u.applyRackShow("paks", props, u.paksRack.Object(), u.paksBtn)
	case "rec":
		u.applyRackShow("rec", props, u.recRack.Object(), u.recBtn)
		u.applyDefaultRecorderTracks(props)
	case "seq":
		u.applyRackShow("seq", props, u.seqRack.Object(), u.seqBtn)
		u.applyDefaultTracks(props)
		u.applyRackExpand(u.seqRack.Object(), props)
	case "pads":
		u.applyDefaultPadLayout(props)
		u.applyRackExpand(u.padRackObj, props)
	}
}

// applyRackExpand applies a `expand: horizontal|vertical|both` property, making
// the rack's frame fill that axis of its allocation (its children stay their
// natural size, centered). Applied on every relayout (idempotent) so a variant
// without the property resets the rack back to content-sized.
func (u *ui) applyRackExpand(obj fyne.CanvasObject, props map[string]string) {
	h, v := parseExpand(props["expand"])
	if cf, ok := obj.(*components.ContentFit); ok {
		cf.SetExpand(h, v)
	}
}

// parseExpand maps an `expand: …` property value to horizontal/vertical flags.
func parseExpand(s string) (horizontal, vertical bool) {
	switch s {
	case "horizontal":
		return true, false
	case "vertical":
		return false, true
	case "both":
		return true, true
	default:
		return false, false
	}
}

// applyDefaultTracks applies a `seq(tracks: N)` property — the variant's default
// sequencer track count — only when the variant is entered, so it establishes a
// default without fighting the user's TRK knob while that variant stays shown.
func (u *ui) applyDefaultTracks(props map[string]string) {
	if !u.variantChanged {
		return
	}
	v, ok := props["tracks"]
	if !ok {
		return
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return
	}
	u.seqRack.SetTrackCount(n)
}

// applyDefaultRecorderTracks applies a `rec(tracks: N)` property — the variant's
// default number of visible recorder (looper) tracks — only when the variant is
// entered, mirroring applyDefaultTracks for the sequencer. The recorder engine
// keeps its full capacity; this governs how many track rows the rack shows.
func (u *ui) applyDefaultRecorderTracks(props map[string]string) {
	if !u.variantChanged {
		return
	}
	v, ok := props["tracks"]
	if !ok {
		return
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return
	}
	u.recRack.SetTrackCount(n)
}

// applyDefaultPadLayout applies a `pads(layout: …)` property — the variant's
// default pad paging — only when the variant is entered. It does not persist to
// the global preference (that's reserved for the user's density button).
func (u *ui) applyDefaultPadLayout(props map[string]string) {
	if !u.variantChanged {
		return
	}
	v, ok := props["layout"]
	if !ok {
		return
	}
	if l, ok := parsePadLayout(v); ok {
		u.applyPadLayout(l, false)
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
