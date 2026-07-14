// Command rp6 is a touch-friendly Roland P-6 pad controller.
//
// It shows 24 pads in a 4x6 grid and two buttons that switch between the two
// banks pages: A-D (pads with MIDI notes 48-71) and E-H (notes 72-95). Tapping
// a pad sends a note-on on the P-6's Sampler MIDI channel over USB, triggering
// that pad regardless of which bank is selected on the hardware.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mono4loop/rp6/internal/audio"
	"github.com/mono4loop/rp6/internal/audiofx"
	"github.com/mono4loop/rp6/internal/effects"
	"github.com/mono4loop/rp6/internal/emu"
	"github.com/mono4loop/rp6/internal/midiin"
	_ "github.com/mono4loop/rp6/internal/midiin/arturia"  // register the Arturia keyboard input drivers
	_ "github.com/mono4loop/rp6/internal/midiin/macropad" // register the MacroPad input driver
	"github.com/mono4loop/rp6/internal/recorder"
	"github.com/mono4loop/rp6/internal/sequencer"
	"github.com/mono4loop/rp6/internal/store"
	"github.com/mono4loop/rp6/internal/ui/components"
	"github.com/mono4loop/rp6/internal/ui/layoutlang"
	"github.com/mono4loop/rp6/internal/ui/layoutspec"
	uitheme "github.com/mono4loop/rp6/internal/ui/theme"
	"github.com/mono4loop/rp6/p6"
)

// seqMaxBars is the maximum bar length a sequencer track can have.
const seqMaxBars = 4

// profileOn enables coarse switch-timing logs (RP6_PROFILE=1) to find where the
// time goes when switching sample kits.
var profileOn = os.Getenv("RP6_PROFILE") != ""

// perfLap logs the elapsed time since *t under label and resets *t to now. It's a
// no-op unless RP6_PROFILE is set, so it's free in normal runs.
func perfLap(label string, t *time.Time) {
	if !profileOn {
		return
	}
	now := time.Now()
	log.Printf("rp6perf: %-32s %s", label, now.Sub(*t))
	*t = now
}

type ui struct {
	dev   p6.Controller
	devMu sync.Mutex // guards dev for the off-main roll/fire path

	// midiIns holds the open external MIDI input controllers (e.g. an Adafruit
	// MacroPad and/or an Arturia keyboard), keyed by device node path so several
	// can be used at once and each is opened only once. Touched only on the UI
	// thread (startMIDIInput, the attach poller, close, and the per-device Run
	// goroutine's fyne.Do teardown).
	midiIns map[string]midiin.Device
	// midiInMissLogged suppresses repeating the "no controller" log line on the
	// attach poller (which retries every 2s); reset once one connects.
	midiInMissLogged bool
	// keyboardAutoShown records that an external melodic keyboard's first note
	// already revealed the keyboard rack, so we don't re-show it after a manual hide.
	keyboardAutoShown bool

	// emuDir, when set, holds WAV samples (A1..H6) the P-6 emulator can play.
	// It's the pool the emulator draws from; whether the emulator is *active*
	// is useEmu (togglable at runtime via the device badge's Ctrl+click).
	emuDir string
	// useEmu selects the active backend: the emulator (needs emuDir) when true,
	// the P-6 hardware when false. Initialized from the -emu flag.
	useEmu bool

	// preopened holds an emulator that was loaded off the UI thread (see
	// setEmuSamples), for the next openDevice to adopt instead of loading inline —
	// so decoding/resampling a sample pak doesn't freeze the UI. loadingSamples
	// guards against overlapping background loads.
	preopened      p6.Controller
	loadingSamples atomic.Bool

	bpm float64 // clock tempo for transport

	clock   *p6.Clocker
	fx      *effects.Engine
	seq     *sequencer.Engine
	rec     *recorder.Engine
	store   *store.Store
	seqSlot int
	// pendingSlot is a slot change queued while the sequencer plays, applied at
	// the next bar boundary (0 = none).
	pendingSlot int
	selPad      int // padID of the selected pad, or -1

	activity     *activitySource
	meterSrc     meterSource
	meterStop    chan struct{} // closed to stop the meter animator on shutdown
	audioMeter   *audio.Meter
	audioCap     audio.Capturer
	recOutput    audio.Output
	audioStarted bool
	audioDevice  string
	win          fyne.Window

	// relayoutReq coalesces resize-driven relayout requests (from onCanvasResize)
	// onto a single watcher goroutine started in main() (relayoutWatch), which
	// marshals each through fyne.Do. Routing them through one main()-started
	// goroutine (rather than spawning one per resize) keeps relayout serialized on
	// the UI loop and means tests — which call build(), not main() — never run a
	// background relayout that would race Fyne's shared text shaper.
	relayoutReq  chan struct{}
	relayoutStop chan struct{} // closed to stop the relayout watcher on shutdown

	grid           *components.PadGrid
	padRackObj     fyne.CanvasObject      // pad grid + tool column, wrapped as a toggleable rack
	padGridArea    *fyne.Container        // holds the current grid object (swapped on density toggle)
	padGridFit     *components.ContentFit // sizes only the grid; the rack panel still fills its pane
	padFloatBtn    *components.RackToggle
	midiInBtn      *components.RackToggle
	layoutBtn      *components.RackCycle
	padLayout      padLayout   // how the 48 pads are paged across the grid
	listenMIDI     atomic.Bool // reflect hardware pad presses in the UI
	padWin         fyne.Window // non-nil while the pad rack floats in its own window
	padFloating    bool
	playBtn        *components.TransportButton
	tempo          *components.Knob
	patternStep    *components.Knob
	fxRack         *effectsRack
	keyboardFXRack *keyboardFXRack
	seqRack        *sequencerRack
	recRack        *recorderRack
	keyboardRack   *keyboardRack
	paksRack       *paksRack
	meter          *components.LevelMeter
	meterArea      *fyne.Container   // stable holder; its child is the framed meter (V or H)
	meterHoriz     bool              // meter currently laid out horizontally (compact mode)
	p6Obj          fyne.CanvasObject // the P-6-only rack (transport + PATTERN + Delay/Reverb); hidden on the emulator

	transportRack fyne.CanvasObject
	statusBar     fyne.CanvasObject
	seqSide       bool // sequencer docked as a right-hand column

	// Runtime UI layout (see internal/ui/layoutlang). layoutDoc is the parsed
	// layout program, compiled into the binary from assets/*.layout; its variants
	// are selected per environment (platform + discrete form factor) — see
	// layoutEnv. There is no continuous adaptation: windowed desktop is one fixed
	// size, mobile is phone-or-tablet by device size, desktop full screen is the
	// console (see docs/architecture/layouts.md and resolutions.txt).
	layoutDoc *layoutlang.Document
	// activeVariant is the name of the layout variant currently shown;
	// variantChanged is set for the one relayout where it changes, so a variant
	// can set a rack's default visibility (`show:`) on entry without fighting the
	// user's manual toggles afterwards.
	activeVariant  string
	variantChanged bool

	// activePage is the id of the application page currently attached to the
	// canvas (e.g. "play" or "loop"); pages holds the ordered set declared in the
	// layout document (`page <id> <Label> { … }` blocks). selectLayout resolves the
	// layout via layoutDoc.SelectForPage(activePage, env) — it picks the page block
	// by id, then a variant within it by form factor — and the wired rack objects
	// are reused across page switches (no rack is parented into two trees). See
	// docs/architecture/layouts.md §12 (Pages).
	activePage string
	pages      []layoutlang.Page
	// pageVis remembers each page's content-rack show/hide configuration
	// (pageVis[pageID][rackID] = visible), so the visibility toggles are
	// per-page: showing KEYS on the LOOP page doesn't affect PLAY, etc. Saved
	// when leaving a page and re-applied on return (see savePageVis/loadPageVis).
	// defaultVis is the factory default (captured at build) a page falls back to
	// on its first visit, so a page starts fresh rather than inheriting the
	// previous page's toggles.
	pageVis    map[string]map[string]bool
	defaultVis map[string]bool

	// mobileForTest/tabletForTest override the compile-time platform for the
	// headless layout-inspection harness only (the real platform is a build-tag
	// constant, so a desktop test binary can't otherwise exercise the phone/
	// tablet variants). nil in production; set per scenario in inspection_test.go.
	mobileForTest *bool
	tabletForTest *bool

	// contentHolder is a stable wrapper whose custom layout reports window size
	// changes to onCanvasResize, so the app can relayout when the discrete
	// form-factor variant changes (a tablet's first size, or the desktop console
	// settling after an async SetFullScreen). layoutScale tracks the effective
	// physical pixels per logical unit so a late Wayland scale change forces a
	// real relayout (see canvasScaleChanged).
	contentHolder *fyne.Container
	layoutScale   float32

	// fullScreen is our own console-layout intent (set only by setConsole via
	// F11 / Ctrl+Shift+Enter / the CONSOLE toggle), NOT the window flag — mobile
	// reports FullScreen()==true inherently, and the console layout is desktop-
	// only. On desktop, console means the OS window is full screen while windowed
	// is a single fixed, non-resizable size (see setConsole).
	fullScreen bool
	// relockWindowed is set when leaving the console: the fixed-size lock is
	// dropped for full screen (so Mutter restores geometry like a normal app) and
	// re-applied by onCanvasResize once the windowed size settles.
	relockWindowed bool
	// forced tracks the racks the active layout variant force-shows/hides via a
	// `show:` property, keyed by rack id, each remembering the visibility the
	// rack had *before* the variant forced it. On a variant switch these are
	// restored (so e.g. leaving the console doesn't leave its force-shown racks
	// stuck on), then the new variant's overrides repopulate the map. Generic:
	// any variant + any rack using `show:` is handled, no hardcoded list.
	forced map[string]savedRack

	// bottom-bar visibility toggles (backlit rack labels)
	padBtn      *components.RackToggle
	playMenuBtn *components.RackToggle
	playMenu    *widget.PopUp
	p6Btn       *components.RackToggle
	fxBtn       *components.RackToggle
	padFXBtn    *components.RackToggle
	keysFXBtn   *components.RackToggle
	fxChoices   *widget.PopUp
	seqBtn      *components.RackToggle
	recBtn      *components.RackToggle
	keysBtn     *components.RackToggle
	paksBtn     *components.RackToggle
	meterBtn    *components.RackToggle
	consoleBtn  *components.RackToggle

	// pageBtns are the page-navigation keys (one backlit RackToggle per declared
	// page, the active page lit), keyed by page id. pageNav frames them as a rack
	// strip the layout positions (`pagenav` id). Both nil with <2 pages. Built
	// from u.pages so navigation is data-driven off the layout document.
	pageBtns map[string]*components.RackToggle
	pageNav  fyne.CanvasObject

	statusLED   *components.LED
	p6LED       *components.LED // P-6 connection LED seated in the P-6 rack plate
	root        fyne.CanvasObject
	status      *widget.Label
	controlBar  fyne.CanvasObject       // bottom control bar: section toggles + info button
	jamControls []fyne.CanvasObject     // optional desktop-only controls; empty on mobile/nojam
	deviceBadge *components.DeviceBadge // pad rack tool row: backlit device nameplate
	deviceState components.DeviceState  // last-known connection state, re-applied when the badge is rebuilt (float/dock)
	storeToggle *components.RackToggle  // pad rack tool row: opens the sample-pak store

	// connecting guards connect() against re-entrant / overlapping calls.
	// devGen tags the live connection so a background failure (mid-session
	// unplug) is reported once and stale goroutines from an old connection
	// can't clobber the current UI state.
	connecting  atomic.Bool
	devGen      atomic.Uint64
	devLost     atomic.Bool
	recUIQueued atomic.Bool

	// emuFallback is true while we're on the emulator only because a P-6 was
	// absent/lost (auto-fallback). A background watcher polls for a P-6 and
	// reconnects to it automatically while this is set — so no manual Reconnect
	// is needed. It's false when the emulator was chosen deliberately (-emu flag
	// or Ctrl+click), which suppresses the auto-switch.
	emuFallback atomic.Bool
	watchStop   chan struct{} // closed to stop the device watcher on shutdown

	keyboardFX audiofx.Settings // retained across emulator kit/backend switches
	recProfile string
}

// windowedWidth/windowedHeight are the single fixed, non-resizable desktop
// windowed size (see resolutions.txt: "Thinkpad X13 - 850 x 950 window mode").
// Sticking to one windowed size means the `window` layout variant only has to be
// correct at one geometry — no continuous adaptation. Desktop full screen uses
// the console layout at the display's size instead.
const (
	windowedWidth  = 850
	windowedHeight = 950
)

func newUI() *ui {
	u := &ui{bpm: 120, selPad: -1, padLayout: loadPadLayout()}
	u.activity = &activitySource{}
	u.meterSrc = u.activity
	u.relayoutReq = make(chan struct{}, 1) // buffered: coalesces resize relayout requests
	u.fx = effects.New(u.firePad)
	u.seq = sequencer.New(8, seqMaxBars, u.firePadVel)
	u.rec = recorder.New(2, 48000)
	return u
}

func (u *ui) build(w fyne.Window) {
	// Parse the embedded UI layout (compiled in; no I/O — safe in tests).
	u.loadLayout()

	// Pad area (P-6-specific grid + paging + selection), wrapped as a rack with
	// a slim left column of tool buttons (first one floats the rack to its own
	// window and docks it back).
	u.buildPadRack()
	u.fxRack = newEffectsRack(u.fx, func() { u.grid.RefreshBadges() })
	// Let the layout file arrange this rack's internals; if there's no `rack fx`
	// block, fall back to the rack's own Go composition (composeRack builds only
	// one, never both — see its doc for why that matters on mobile).
	u.fxRack.obj = u.composeRack("fx", layoutspec.Registry{
		"fxRoll": u.fxRack.roll,
		"fxRate": u.fxRack.rate.Object(),
	}, u.fxRack.defaultObject)
	u.keyboardFX = loadKeyboardFX()
	u.keyboardFXRack = newKeyboardFXRack(u.keyboardFX, u.setKeyboardFX)
	u.keyboardFXRack.obj = u.composeRack("keysfx", layoutspec.Registry{
		"keysFXTone":   u.keyboardFXRack.tone.Object(),
		"keysFXComp":   u.keyboardFXRack.comp.Object(),
		"keysFXChorus": u.keyboardFXRack.chorus.Object(),
		"keysFXDelay":  u.keyboardFXRack.delay.Object(),
		"keysFXReverb": u.keyboardFXRack.reverb.Object(),
	}, u.keyboardFXRack.defaultObject)
	u.seqRack = newSequencerRack(u.seq,
		func() {
			if u.root != nil {
				u.root.Refresh()
			}
		}, u.onSeqDock, numSeqSlots, u.selectSlot, u.copyToSlot, u.deleteSlot, u.saveSeq)
	seqObject := u.composeRack("seq", layoutspec.Registry{
		"seqHeader":   u.seqRack.header,
		"seqControls": u.seqRack.header2,
		"seqGrid":     u.seqRack.trackBox,
	}, u.seqRack.defaultObject)
	u.seqRack.obj = u.seqRack.fitObject(seqObject)
	u.seqRack.onStop = u.applyPendingSlot
	u.seq.OnStep = func(tick int) {
		fyne.Do(func() {
			u.maybeApplyPendingAt(tick)
			u.seqRack.setPlayhead(tick)
		})
	}
	u.recRack = newRecorderRack(u.rec, u.setStatus, u.exportRecorderTrack)
	u.recRack.obj = u.composeRack("rec", layoutspec.Registry{
		"recHeader":   u.recRack.header,
		"recControls": u.recRack.controls,
		"recTracks":   u.recRack.trackBox,
	}, u.recRack.defaultObject)
	u.rec.SetOnChange(func(track int) {
		if !u.recUIQueued.CompareAndSwap(false, true) {
			return
		}
		fyne.Do(func() {
			if u.recRack != nil {
				u.recRack.syncAll()
			}
			u.recUIQueued.Store(false)
		})
	})

	// Keyboard rack: a P-6-style chromatic keyboard for the selected sample.
	u.keyboardRack = newKeyboardRack(func(note uint8) { u.playNote(note, p6.DefaultVelocity) })
	u.keyboardRack.obj = u.composeRack("keys", layoutspec.Registry{
		"keyboardOct":  u.keyboardRack.oct.Object(),
		"keyboardKeys": u.keyboardRack.piano,
	}, u.keyboardRack.defaultObject)

	// Paks rack: a kit selector listing the sample paks installed from the store;
	// tapping one loads it into the emulator, and a store key opens the store.
	u.paksRack = newPaksRack(u.installedPakItems, u.setEmuSamples, u.openSampleStore)
	u.paksRack.obj = u.composeRack("paks", layoutspec.Registry{
		"paksHeader": u.paksRack.header,
		"paksList":   u.paksRack.scroll,
	}, u.paksRack.defaultObject)

	// Transport rack: just the TEMPO knob. Play/Stop and PATTERN moved into the
	// P-6-only rack (see below) — TEMPO stays here because it also drives the
	// host-side sequencer/effects clocks, so it's useful on the emulator too.
	u.playBtn = components.NewTransportToggle(func(running bool) {
		if running {
			u.play()
		} else {
			u.stop()
		}
	})

	u.tempo = components.NewKnob(components.KnobConfig{
		Label: "TEMPO", Value: int(u.bpm), Min: 40, Max: 300, Step: 5,
		Accent:   deviceHwAccent, // match the bottom-rack toggles
		Format:   func(v int) string { return fmt.Sprintf("%d BPM", v) },
		OnChange: u.onTempoChange,
	})
	u.patternStep = components.NewKnob(components.KnobConfig{
		Label: "PATTERN", Value: 0, Min: 0, Max: 63, Step: 1,
		Accent:    deviceHwAccent,
		Indicator: components.GridIndicator{Cols: 8, Rows: 8}, // 64 patterns; active highlighted
		Format:    patternName,
		OnChange:  u.onPatternChange,
	})

	// The transport rack internals are laid out from the layout file too; the
	// fallback below (built only when there's no `rack` block) reproduces the
	// stock Go arrangement. composeRack never builds both trees.
	u.transportRack = u.composeRack("transport", layoutspec.Registry{
		"tempo": u.tempo.Object(),
	}, func() fyne.CanvasObject {
		return components.NewRackPanel(container.NewHBox(u.tempo.Object()))
	})

	// The P-6-only rack: Play/Stop, PATTERN and the four Delay/Reverb knobs.
	// Everything here talks to the hardware over MIDI (transport clock, Program
	// Change, global-FX CC) and is a no-op on the emulator, so the whole rack is
	// hidden and its toggle disabled unless the P-6 is the active backend (see
	// applyBackendGating). Delay/Reverb are amount knobs (0..127), so they use
	// the default LED ring indicator like TEMPO. The plate is tinted the P-6's
	// yellow (with a bold "P-6" nameplate) so it echoes the physical unit; the
	// layout `rack p6` block only arranges the controls, and Go wraps them in the
	// tinted plate (device-specific styling stays out of the layout file).
	delayTime := u.fxKnob("DLY TIME", p6.CCDelayTime)
	delayLevel := u.fxKnob("DLY LVL", p6.CCDelayLevel)
	reverbTime := u.fxKnob("REV TIME", p6.CCReverbTime)
	reverbLevel := u.fxKnob("REV LVL", p6.CCReverbLevel)
	// Connection LED seated in the plate (black-bezel, like a mounted indicator),
	// paired with the "P-6" nameplate as its lead; setConnected drives its color.
	u.p6LED = components.NewLEDBordered(ledRed)
	p6Inner := u.recomposeRack("p6", layoutspec.Registry{
		"play":        u.playBtn,
		"pattern":     u.patternStep.Object(),
		"delayTime":   delayTime.Object(),
		"delayLevel":  delayLevel.Object(),
		"reverbTime":  reverbTime.Object(),
		"reverbLevel": reverbLevel.Object(),
	})
	if p6Inner == nil {
		p6Inner = container.NewHBox(
			u.playBtn, widget.NewSeparator(), u.patternStep.Object(), widget.NewSeparator(),
			delayTime.Object(), delayLevel.Object(), reverbTime.Object(), reverbLevel.Object())
	}
	u.p6Obj = components.NewRackPanelTinted(p6Inner, p6PlateColor, "P-6", u.p6LED)

	// Master meter, framed as a rack panel (toggleable). A short "VU" cap keeps
	// it compact. It rides at the top beside TEMPO (default) or along the bottom
	// (compact), always as a horizontal strip — see applyMeterOrientation.
	u.meter = components.NewLevelMeter()
	u.meterArea = container.NewStack()
	u.applyMeterOrientation(false)

	// Visibility toggles on the bottom bar: backlit rack labels (lit = shown,
	// greyed = hidden), tinted in the P-6 amber accent.
	acc := deviceHwAccent
	u.padBtn = components.NewRackToggle("PADS", acc, func() {
		u.hidePlayMenu()
		u.togglePads()
	})
	u.p6Btn = components.NewRackToggle("P-6", acc, u.toggleP6Rack)
	u.fxBtn = components.NewRackToggle("FX", acc, u.toggleFXChoices)
	u.padFXBtn = components.NewRackToggle("PAD FX", acc, func() {
		u.hideFXChoices()
		u.toggleVisible(u.fxRack.Object(), u.padFXBtn)
	})
	u.keysFXBtn = components.NewRackToggle("KEYS FX", deviceEmuAccent, func() {
		u.hideFXChoices()
		u.toggleKeyboardFX()
	})
	u.fxChoices = widget.NewPopUp(popupChoices(u.padFXBtn, u.keysFXBtn), w.Canvas())
	u.fxChoices.Hide()
	u.seqBtn = components.NewRackToggle("SEQ", acc, func() {
		u.hidePlayMenu()
		u.toggleSeqView()
	})
	u.recBtn = components.NewRackToggle("REC", recorderAccent, func() {
		u.hidePlayMenu()
		u.toggleVisible(u.recRack.Object(), u.recBtn)
	})
	u.keysBtn = components.NewRackToggle("KEYS", acc, func() {
		u.hidePlayMenu()
		u.toggleVisible(u.keyboardRack.Object(), u.keysBtn)
	})
	u.playMenuBtn = components.NewRackToggleIcon(theme.GridIcon(), acc, u.togglePlayMenu)
	// The recorder is reached via the LOOP page (see buildPageNav), not a rack
	// toggle, so the PADS/SEQ/KEYS "play menu" no longer lists REC. u.recBtn stays
	// as the state holder the loop variant's `rec(show: true)` drives.
	u.playMenu = widget.NewPopUp(popupChoices(u.padBtn, u.seqBtn, u.keysBtn), w.Canvas())
	u.playMenu.Hide()
	u.paksBtn = components.NewRackToggle("PAKS", acc, func() { u.toggleVisible(u.paksRack.Object(), u.paksBtn) })
	u.meterBtn = components.NewRackToggle("VU", acc, func() { u.toggleVisible(u.meterArea, u.meterBtn) })
	// CONSOLE switches to the "mixing console" layout: full screen on desktop.
	// It's desktop-only — on mobile the phone/tablet layout is chosen by the
	// device size, so the toggle is omitted from the bar there.
	u.consoleBtn = components.NewRackToggle("CONSOLE", acc, u.toggleConsole)
	u.consoleBtn.SetOn(u.fullScreen)
	toggleObjs := []fyne.CanvasObject{u.playMenuBtn, u.p6Btn, u.fxBtn, u.paksBtn, u.meterBtn}
	if !onMobile {
		toggleObjs = append(toggleObjs, u.consoleBtn)
	}
	u.jamControls = u.jamToggles() // absent in -tags nojam / web / mobile builds
	toggleObjs = append(toggleObjs, u.jamControls...)
	toggles := container.NewHBox(toggleObjs...)
	// Page-navigation strip (PLAY / LOOP …): a separate rack the layout places
	// beside the toggles where there's room (desktop/tablet) or on its own row on
	// phones, so the narrow phone bar isn't overfilled. Data-driven off the
	// document's declared pages; nil (and absent from every layout) with <2 pages.
	u.buildPageNav()

	u.status = widget.NewLabel("")
	info := widget.NewButtonWithIcon("", theme.InfoIcon(), u.showInfo)
	info.Importance = widget.LowImportance
	u.statusLED = components.NewLED(ledRed)

	// Control bar: the section toggles (left) + the info button (right).
	u.controlBar = components.NewRackPanel(container.NewBorder(nil, nil, nil, info, toggles))

	// Status strip: the connection LED + status message, in its own slim rack at
	// the very bottom. NewRackPanelThin keeps it just tall enough for the LED and
	// text — no taller — to minimize the vertical space it takes.
	u.statusBar = components.NewRackPanelThin(container.NewBorder(nil, nil, u.statusLED, nil, u.status))

	u.win = w

	// Ctrl+Shift+P/D/F/S/K/M toggle the Pads, P-6, FX, Sequencer, Keyboard, Meter racks.
	u.addRackShortcut(w, fyne.KeyP, u.togglePads)
	u.addRackShortcut(w, fyne.KeyD, u.toggleP6Rack)
	u.addRackShortcut(w, fyne.KeyF, u.toggleFXChoices)
	u.addRackShortcut(w, fyne.KeyS, u.toggleSeqView)
	u.addRackShortcut(w, fyne.KeyK, func() { u.toggleVisible(u.keyboardRack.Object(), u.keysBtn) })
	u.addRackShortcut(w, fyne.KeyA, func() { u.toggleVisible(u.paksRack.Object(), u.paksBtn) })
	u.addRackShortcut(w, fyne.KeyM, func() { u.toggleVisible(u.meterArea, u.meterBtn) })

	// Ctrl+Shift+Left/Right navigate between application pages (PLAY / LOOP …).
	// The recorder lives on the LOOP page now, so there's no REC rack shortcut.
	u.addRackShortcut(w, fyne.KeyLeft, func() { u.cyclePage(-1) })
	u.addRackShortcut(w, fyne.KeyRight, func() { u.cyclePage(1) })

	// F11 toggles full screen, which switches to the "console" layout (Fyne has
	// no built-in full-screen key, and a modifier-less key can't be an
	// AddShortcut, so we handle it via the canvas typed-key hook).
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev.Name == fyne.KeyF11 {
			u.toggleFullScreen()
		}
	})
	// Ctrl+Shift+Enter is the always-works alternative (a modified shortcut fires
	// regardless of keyboard focus, unlike the F11 typed-key above). Bind both the
	// main Return and the numpad Enter.
	u.addRackShortcut(w, fyne.KeyReturn, u.toggleFullScreen)
	u.addRackShortcut(w, fyne.KeyEnter, u.toggleFullScreen)

	// Default visibility: Pads + Meter on; Sequencer, FX and Keyboard off. The P-6
	// rack (transport + PATTERN + Delay/Reverb) is on by default too — it holds
	// Play/Stop — but applyBackendGating below hides it and disables its toggle
	// when the emulator is the active backend. The sequencer does not fit above
	// touch-sized pads in the default-height window; the console layout force-shows it.
	u.setVisible(u.padRackObj, u.padBtn, true)
	u.setVisible(u.p6Obj, u.p6Btn, true)
	u.setVisible(u.fxRack.Object(), u.padFXBtn, false)
	u.setVisible(u.keyboardFXRack.Object(), u.keysFXBtn, false)
	u.setVisible(u.seqRack.Object(), u.seqBtn, false)
	u.setVisible(u.recRack.Object(), u.recBtn, false)
	u.setVisible(u.keyboardRack.Object(), u.keysBtn, false)
	u.updatePlayMenuButton()
	u.setVisible(u.paksRack.Object(), u.paksBtn, false)
	u.setVisible(u.meterArea, u.meterBtn, true)

	// Snapshot these defaults as the fallback a page uses on its first visit, so
	// per-page visibility (savePageVis/loadPageVis) starts each page fresh.
	u.captureDefaultVis()

	// Gate the P-6-only rack for the current backend (hidden + disabled on the
	// emulator). No relayout here — the build's own relayout() below covers it.
	u.applyBackendGating()

	u.relayout()
}

// relayout (re)assembles the window content from the active layout document
// (see internal/ui/layoutlang). The app registers its widgets by stable ID; the
// layout file (embedded default, or the user's editable override) decides where
// they go per form factor, and the current UI state (compact, docked sequencer,
// floating pads, window size) is passed in as the condition environment. The
// layout is data, so rearranging the UI is a file edit — no code change.
func (u *ui) relayout() {
	// The keyboard's keys grow taller in the console (full-screen) layout, where
	// there's vertical room; compact otherwise. Driven here (not from a layout
	// `tall:` prop) so it resets when leaving full screen even though the
	// windowed layout references the rack without properties.
	if u.keyboardRack != nil {
		u.keyboardRack.setTall(u.isFullScreen())
	}
	// Keep the CONSOLE toggle lit whenever the console layout is active, however
	// it was entered (button, F11, or Ctrl+Shift+Enter).
	if u.consoleBtn != nil {
		u.consoleBtn.SetOn(u.isFullScreen())
	}

	reg := layoutspec.Registry{
		"transport": u.transportRack,
		"p6":        u.p6Obj,
		"fx":        u.fxRack.Object(),
		"keysfx":    u.keyboardFXRack.Object(),
		"seq":       u.seqRack.Object(),
		"rec":       u.recRack.Object(),
		"keys":      u.keyboardRack.Object(),
		"paks":      u.paksRack.Object(),
		"pads":      u.padRackObj,
		"vu":        u.meterArea,
		"toggles":   u.controlBar,
		"pagenav":   u.pageNav,
		"status":    u.statusBar,
	}

	u.root = u.selectLayout(reg)
	if u.root == nil {
		// No document, or no variant matched: fall back to a minimal arrangement
		// so the window is never blank.
		u.root = container.NewBorder(u.transportRack, container.NewVBox(u.controlBar, u.statusBar), nil, nil, u.padRackObj)
	}

	// A stable outer holder (created once) watches the window size so we can
	// relayout when the discrete form factor (variant) changes. Swapping its
	// child avoids re-doing SetContent on every relayout.
	if u.contentHolder == nil {
		u.contentHolder = container.New(&sizeWatch{onResize: u.onCanvasResize}, u.root)
		u.win.SetContent(u.contentHolder)
	} else {
		u.contentHolder.Objects = []fyne.CanvasObject{u.root}
		u.contentHolder.Refresh()
	}
	u.layoutScale = u.canvasPhysicalScale()
}

// canvasSize reports the current window canvas size, or the zero size if there's
// no window/canvas yet (e.g. very early startup). It feeds layoutEnv, which
// resolves the size to a discrete form-factor variant (see variantFor).
func (u *ui) canvasSize() fyne.Size {
	if u.win == nil || u.win.Canvas() == nil {
		return fyne.Size{}
	}
	return u.win.Canvas().Size()
}

// applyMeterOrientation re-frames the VU meter for the current form factor. It's
// always a horizontal strip now (a "VU" cap on the left), but the framing differs
// by placement: beside the TEMPO knob (default top) or along the bottom (compact)
// it must NOT stretch the LEDs to the full rack height — that reads as a giant
// meter — so the strip is pinned to its natural height and centered vertically
// within whatever space it's given. The meter widget itself is reused, and
// meterArea is a stable holder so its visibility (the VU toggle) survives the swap.
func (u *ui) applyMeterOrientation(horizontal bool) {
	if u.meterArea != nil && len(u.meterArea.Objects) > 0 && u.meterHoriz == horizontal {
		return
	}
	u.meterHoriz = horizontal
	u.meter.SetHorizontal(horizontal)
	capLabel := widget.NewLabelWithStyle("VU", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	var inner fyne.CanvasObject
	if horizontal {
		// cap + LED strip on one row, kept at its natural height and centered so
		// the LEDs don't balloon to fill a tall rack row beside the TEMPO knob.
		strip := container.NewBorder(nil, nil, capLabel, nil, u.meter)
		inner = container.NewVBox(layout.NewSpacer(), strip, layout.NewSpacer())
	} else {
		inner = container.NewBorder(capLabel, nil, nil, nil, u.meter)
	}
	u.meterArea.Objects = []fyne.CanvasObject{components.NewRackPanel(inner)}
	u.meterArea.Refresh()
}

// onCanvasResize is called by the content holder's layout on every window resize.
// Under the fixed-form-factor policy the layout does NOT adapt continuously to
// pixel size: it relays out only when the discrete variant the size resolves to
// actually changes (see variantFor). That happens in two real cases — a mobile
// device learning its first real size (phone vs tablet), and the desktop console
// settling after an async SetFullScreen — not while a window is dragged (the
// windowed size is fixed, and the console's proportional splits reflow on their
// own without a rebuild).
func (u *ui) onCanvasResize(size fyne.Size) {
	if size.Width <= 1 || size.Height <= 1 {
		return
	}
	// After leaving the console, re-lock the fixed windowed size once the
	// compositor has restored the pre-full-screen geometry (width back near the
	// windowed width — not an intermediate full-screen frame). One-shot, so it
	// can't loop or fight the compositor.
	if u.relockWindowed && !onMobile && !u.isFullScreen() && u.win != nil {
		if absFloat(size.Width-windowedWidth) <= 2 {
			u.relockWindowed = false
			u.win.SetFixedSize(true)
			u.diagRelock(size)
		}
	}
	if u.variantFor(size) == u.activeVariant {
		return
	}
	// Request a relayout off this layout pass (calling relayout synchronously here
	// would re-enter layout). The relayoutWatch goroutine (started in main())
	// marshals it through fyne.Do so it runs serialized on the UI loop. A buffered
	// send coalesces bursts; if the watcher isn't running (tests use build(), not
	// main()), the request is simply dropped and the test drives relayout itself.
	select {
	case u.relayoutReq <- struct{}{}:
	default:
	}
}

// relayoutWatch marshals resize-driven relayout requests (from onCanvasResize)
// onto the UI loop via fyne.Do, one at a time. Started in main() — not build() —
// so the headless tests never run a background relayout (which would race Fyne's
// shared text shaper); it runs until relayoutStop is closed on shutdown.
func (u *ui) relayoutWatch() {
	u.relayoutStop = make(chan struct{})
	go func() {
		for {
			select {
			case <-u.relayoutStop:
				return
			case <-u.relayoutReq:
				fyne.Do(u.relayout)
			}
		}
	}()
}

func (u *ui) canvasPhysicalScale() float32 {
	if u.win == nil || u.win.Canvas() == nil {
		return 0
	}
	c := u.win.Canvas()
	x0, _ := c.PixelCoordinateForPosition(fyne.Position{})
	x1, _ := c.PixelCoordinateForPosition(fyne.NewPos(1000, 0))
	if scale := float32(x1-x0) / 1000; scale > 0 {
		return scale
	}
	return c.Scale()
}

func (u *ui) relayoutIfScaleChanged() {
	if !u.canvasScaleChanged() {
		return
	}
	u.relayout()
}

func (u *ui) requestRelayoutIfScaleChanged() {
	if !u.canvasScaleChanged() {
		return
	}
	select {
	case u.relayoutReq <- struct{}{}:
	default:
	}
}

func (u *ui) canvasScaleChanged() bool {
	current := u.canvasPhysicalScale()
	if current <= 0 {
		return false
	}
	if u.layoutScale == 0 {
		u.layoutScale = current
		return false
	}
	return absFloat(current-u.layoutScale) >= 0.01
}

func absFloat(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

// stopRelayoutWatch halts the relayout watcher (idempotent).
func (u *ui) stopRelayoutWatch() {
	if u.relayoutStop != nil {
		close(u.relayoutStop)
		u.relayoutStop = nil
	}
}

// sizeWatch is a pass-through layout (its single child fills the whole area)
// that reports the container's size to onResize on every layout pass. It lets
// the app react to window resizes (to relayout when the discrete form-factor
// variant changes) without subclassing the window.
type sizeWatch struct {
	onResize func(fyne.Size)
	last     fyne.Size
}

func (s *sizeWatch) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
	if size != s.last {
		s.last = size
		if s.onResize != nil {
			s.onResize(size)
		}
	}
}

func (s *sizeWatch) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var m fyne.Size
	for _, o := range objs {
		m = m.Max(o.MinSize())
	}
	return m
}

// buildPadRack (re)builds the pad rack — the tool column plus the pad grid — as
// a fresh object tree and stores it in u.padRackObj / u.grid / u.padGridArea.
//
// It is rebuilt (rather than re-parented) whenever it changes host window,
// because Fyne associates each CanvasObject with exactly one canvas (a global
// object->canvas cache); moving the same tree between windows corrupts refresh
// routing and per-window textures. Button/selection state is re-applied so the
// rebuild is seamless.
func (u *ui) buildPadRack() {
	u.grid = newPadGrid(u.padLayout, u.onPadTrigger, u.padBadges)
	u.padGridArea = container.NewStack(u.grid.Object())
	u.padGridFit = components.NewContentFit(u.padGridArea, func(available fyne.Size) fyne.Size {
		const bottomInset = 8
		available.Height = max(available.Height-bottomInset, float32(0))
		return u.grid.PreferredSize(available)
	})
	u.padGridFit.SetContentMin(true)

	// Tool strip across the top: backlit icon toggles (lit = active, faded = off).
	tool := deviceHwAccent
	u.padFloatBtn = components.NewRackToggleIcon(theme.ViewRestoreIcon(), tool, u.togglePadFloat)
	u.padFloatBtn.SetOn(u.padFloating)
	u.midiInBtn = components.NewRackToggleIcon(theme.VisibilityIcon(), tool, u.toggleMIDIListen)
	u.updateMIDIInBtn()
	// Layout selector: a 3-state cycle (paged A–D/E–H → two-bank A–B…G–H → dense
	// all-8) whose icon shows the current density.
	u.layoutBtn = components.NewRackCycle(layoutIcons, tool, func(s int) { u.setLayout(padLayout(s)) })
	u.layoutBtn.SetState(int(u.padLayout))

	// Device nameplate at the right end of the tool row. It's rebuilt with the
	// pad rack (each window owns its own object tree), so re-apply the current
	// connection state and wire its actions here rather than in build().
	name, tag, accent := u.deviceIdentity()
	u.deviceBadge = components.NewDeviceBadge(name, tag, accent)
	u.deviceBadge.OnSettings(u.showDeviceSettings)
	u.deviceBadge.OnToggle(u.toggleBackend)
	u.deviceBadge.SetState(u.deviceState)

	// Store toggle, just left of the device badge: opens the sample-pak store
	// (blue accent, kin to the badge). It's an action, not a stateful toggle, so
	// keep it lit (glowing blue border + icon) rather than greyed — tapping fires
	// openSampleStore without changing the lit state. Desktop/mobile open the
	// catalog; the web build is a no-op with a status message (see openSampleStore).
	u.storeToggle = components.NewRackToggleIcon(theme.DownloadIcon(), storeAccent, u.openSampleStore)
	u.storeToggle.SetOn(true)

	// Lay out the pad rack internals from the layout file (`rack pads`), falling
	// back to the stock Go arrangement if there's no block. composeRack builds
	// only one tree, so the sub-widgets are never double-parented (which broke
	// rendering on the Android driver — see composeRack).
	padRack := u.composeRack("pads", layoutspec.Registry{
		"padFloat":   u.padFloatBtn,
		"padListen":  u.midiInBtn,
		"padDensity": u.layoutBtn,
		"store":      u.storeToggle,
		"badge":      u.deviceBadge,
		"padGrid":    u.padGridFit,
	}, func() fyne.CanvasObject {
		padTools := container.NewHBox(u.padFloatBtn, u.midiInBtn, u.layoutBtn,
			layout.NewSpacer(), u.storeToggle, u.deviceBadge)
		return components.NewRackPanel(container.NewBorder(padTools, nil, nil, nil, u.padGridFit))
	})
	u.padRackObj = u.fitPadRack(padRack)

	// Re-apply the selection highlight (no flash) to the fresh grid.
	if u.selPad >= 0 {
		bank, number := padBankNumber(u.selPad)
		u.grid.Select(u.gridPos(bank, number))
	}
}

// fitPadRack keeps horizontal slack outside the rack panel while allowing the
// rack itself to fill the complete vertical pane. Only the square pad grid is
// bounded inside it.
func (u *ui) fitPadRack(object fyne.CanvasObject) fyne.CanvasObject {
	panel, ok := object.(*components.RackPanel)
	if !ok {
		return object
	}
	fit := components.NewContentFit(panel, func(available fyne.Size) fyne.Size {
		frame := panel.SizeForContent(fyne.Size{})
		grid := u.grid.PreferredSize(available.Subtract(frame))
		width := max(panel.MinSize().Width, frame.Width+grid.Width)
		return fyne.NewSize(min(width, available.Width), available.Height)
	})
	// Parent layouts must reserve enough room for six 80px pads. If the complete
	// geometry cannot satisfy that floor, console fit policy hides other racks
	// instead of shrinking or overlapping the pad grid.
	fit.SetContentMin(true)
	return fit
}

// togglePads shows/hides the pad grid rack (it occupies the center, so relayout).
func (u *ui) togglePads() {
	show := !u.padRackObj.Visible()
	u.setVisible(u.padRackObj, u.padBtn, show)
	u.relayout()
}

// togglePadFloat pops the pad rack out into its own window, or docks it back.
func (u *ui) togglePadFloat() {
	if u.padFloating {
		u.dockPad()
	} else {
		u.floatPad()
	}
}

// floatPad moves the pad rack into a separate window (rebuilt for that window).
func (u *ui) floatPad() {
	if u.padFloating {
		return
	}
	u.padFloating = true
	u.buildPadRack() // fresh object tree owned by the new window
	w := fyne.CurrentApp().NewWindow("RP6 — Pads")
	w.SetIcon(appIcon())
	u.padWin = w
	w.SetContent(u.padRackObj)
	w.SetCloseIntercept(func() {
		// Closing the floating window docks the rack back into the main window.
		u.dockPad()
	})
	w.Resize(fyne.NewSize(560, 380))
	w.Show()

	u.relayout() // main window drops the pad rack
}

// dockPad returns the pad rack to the main window (rebuilt for it).
func (u *ui) dockPad() {
	if !u.padFloating {
		return
	}
	u.padFloating = false
	u.buildPadRack() // fresh object tree owned by the main window

	w := u.padWin
	u.padWin = nil
	u.relayout() // main window shows the rebuilt pad rack
	if w != nil {
		w.Close()
	}
}

// onSeqDock docks or undocks the sequencer as a right-hand column.
func (u *ui) onSeqDock(docked bool) {
	u.seqSide = docked
	if docked {
		u.setVisible(u.seqRack.Object(), u.seqBtn, true)
	}
	u.relayout()
}

// toggleSeqView shows/hides the sequencer rack (it re-lays out because it can
// dock to the side column).
func (u *ui) toggleSeqView() {
	show := !u.seqRack.Object().Visible()
	u.setVisible(u.seqRack.Object(), u.seqBtn, show)
	u.relayout()
}

// toggleP6Rack shows/hides the P-6-only rack (transport + PATTERN + Delay/Reverb).
// It's inert while gated off (the toggle is disabled on the emulator), so the
// disabled guard mirrors what the greyed toggle already signals.
func (u *ui) toggleP6Rack() {
	if u.p6Btn.Disabled() {
		return
	}
	u.toggleVisible(u.p6Obj, u.p6Btn)
}

// popupChoices stacks backlit selectors vertically.
func popupChoices(buttons ...fyne.CanvasObject) *fyne.Container {
	return container.NewVBox(buttons...)
}

// togglePlayMenu floats the PADS/SEQ/KEYS rack selectors vertically above the
// bottom bar. The popup is transient; the component-icon light reflects whether any of
// those racks is visible, not whether the popup itself is open.
func (u *ui) togglePlayMenu() {
	if u.playMenu.Visible() {
		u.hidePlayMenu()
		return
	}
	u.hideFXChoices()
	menuHeight := u.playMenu.MinSize().Height
	u.playMenu.ShowAtRelativePosition(fyne.NewPos(0, -menuHeight), u.playMenuBtn)
}

func (u *ui) hidePlayMenu() {
	if u.playMenu != nil {
		u.playMenu.Hide()
	}
}

func (u *ui) updatePlayMenuButton() {
	if u.playMenuBtn == nil || u.padRackObj == nil || u.seqRack == nil || u.keyboardRack == nil {
		return
	}
	u.playMenuBtn.SetOn(u.padRackObj.Visible() || u.seqRack.Object().Visible() || u.keyboardRack.Object().Visible())
}

// toggleFXChoices floats the PAD FX / KEYS FX selectors vertically above the
// bottom bar. The child buttons independently show their corresponding racks.
func (u *ui) toggleFXChoices() {
	show := !u.fxChoices.Visible()
	if show {
		u.hidePlayMenu()
		menuHeight := u.fxChoices.MinSize().Height
		u.fxChoices.ShowAtRelativePosition(fyne.NewPos(0, -menuHeight), u.fxBtn)
	} else {
		u.hideFXChoices()
	}
	u.updateFXButton()
}

func (u *ui) hideFXChoices() {
	if u.fxChoices != nil {
		u.fxChoices.Hide()
	}
}

func (u *ui) updateFXButton() {
	if u.fxBtn == nil || u.fxRack == nil || u.keyboardFXRack == nil {
		return
	}
	u.fxBtn.SetOn(u.fxRack.Object().Visible() || u.keyboardFXRack.Object().Visible())
}

func (u *ui) toggleKeyboardFX() {
	if u.keysFXBtn.Disabled() {
		return
	}
	u.toggleVisible(u.keyboardFXRack.Object(), u.keysFXBtn)
}

// applyBackendGating reveals or hides the P-6-only rack for the active backend.
// Its controls (transport clock, Program Change, global-FX CC) are no-ops on the
// emulator, so on the emulator the rack is hidden and its bottom-bar toggle is
// disabled (greyed, non-clickable); on the P-6 it's shown by default (it holds
// Play/Stop) and the toggle is enabled. Safe to call before the first relayout
// (build()) — it only relays out when there's already content.
func (u *ui) applyBackendGating() {
	onP6 := !u.useEmu
	if u.p6Btn != nil {
		u.p6Btn.SetDisabled(!onP6)
	}
	changed := false
	if u.p6Obj != nil {
		changed = u.p6Obj.Visible() != onP6
		u.setVisible(u.p6Obj, u.p6Btn, onP6)
	}
	if u.keysFXBtn != nil {
		u.keysFXBtn.SetDisabled(onP6)
	}
	if u.keyboardFXRack != nil && onP6 && u.keyboardFXRack.Object().Visible() {
		u.setVisible(u.keyboardFXRack.Object(), u.keysFXBtn, false)
		changed = true
	}
	u.refreshPaksRack() // the loaded-pak highlight clears on the P-6, returns on the emulator
	// Relayout is the expensive part, so only do it when the P-6 rack's presence
	// actually changed (P-6 <-> emulator). An emulator->emulator pak switch leaves
	// the layout identical, so skip it there — that removes a chunk of the per-
	// switch UI-thread hitch.
	if changed && u.root != nil {
		u.relayout()
	}
}

// refreshPaksRack repopulates the paks rack and lights the currently-loaded pak
// (the active emulator samples directory; none on the P-6 or the built-in kit).
func (u *ui) refreshPaksRack() {
	if u.paksRack == nil {
		return
	}
	active := ""
	if u.useEmu {
		active = u.emuDir // "" for the built-in kit — matches no installed pak
	}
	u.paksRack.refresh(active)
}

// toggleFullScreen toggles the "mixing console" layout (F11 / Ctrl+Shift+Enter).
func (u *ui) toggleFullScreen() { u.setConsole(!u.fullScreen) }

// toggleConsole toggles the console layout via the bottom-bar CONSOLE button.
func (u *ui) toggleConsole() { u.setConsole(!u.fullScreen) }

// buildPageNav creates the page-navigation strip: one backlit key per declared
// page (the active page lit), framed as its own rack panel — the epic's
// rack-style page navigation. It's data-driven off u.pages, so declaring a page
// in the layout document adds a key here. It leaves u.pageNav nil (and the
// `pagenav` layout ref unresolved, so it's simply omitted) when the document
// declares fewer than two pages. Called once from build().
func (u *ui) buildPageNav() {
	u.pageBtns = map[string]*components.RackToggle{}
	u.pageNav = nil
	if len(u.pages) < 2 {
		return
	}
	objs := make([]fyne.CanvasObject, 0, len(u.pages))
	for _, pg := range u.pages {
		id := pg.ID
		btn := components.NewRackToggle(pg.Label, deviceHwAccent, func() { u.setPage(id) })
		btn.SetOn(id == u.activePage)
		u.pageBtns[id] = btn
		objs = append(objs, btn)
	}
	u.pageNav = components.NewRackPanel(container.NewHBox(objs...))
}

// updatePageNav lights the active page's navigation key and greys the rest.
func (u *ui) updatePageNav() {
	for id, btn := range u.pageBtns {
		btn.SetOn(id == u.activePage)
	}
}

// setPage switches the active application page and rebuilds the window around
// the *same* wired rack objects — only the container scaffolding changes, so no
// rack is ever parented into two trees (the epic's core constraint). Navigation
// leaves all live state (pad selection, sequencer, recorder, audio, MIDI,
// console/full-screen intent) untouched. Rack show/hide is **per-page**: the
// outgoing page's rack visibility is saved and the incoming page's restored, so
// the visibility toggles configure each page independently (e.g. KEYS shown on
// LOOP but not PLAY). Like leaving the console, it also restores the racks the
// outgoing variant force-showed via `show:`, then relayout applies the incoming
// page's own `show:` defaults. No-op for the current or an unknown page.
func (u *ui) setPage(id string) {
	if id == u.activePage || !u.pageValid(id) {
		return
	}
	u.hidePlayMenu()
	u.hideFXChoices()
	u.savePageVis(u.activePage) // remember this page's rack show/hide config
	u.restoreForcedRacks()      // undo the outgoing page variant's show: overrides
	u.activePage = id
	rememberPage(id)  // resume on this page next launch
	u.loadPageVis(id) // apply the incoming page's remembered show/hide config
	u.updatePageNav()
	u.relayout()
}

// toggleRack pairs a per-page-toggleable content rack with its id + bottom-bar
// toggle, for the per-page visibility snapshot.
type toggleRack struct {
	id  string
	obj fyne.CanvasObject
	btn *components.RackToggle
}

// toggleRacks lists the content racks whose show/hide state is remembered per
// application page (see savePageVis/loadPageVis). The backend-gated racks (`p6`,
// `keysfx`) are excluded — their visibility follows the active backend, not the
// page. Called fresh each time so it reflects a rebuilt pad rack (float/dock).
func (u *ui) toggleRacks() []toggleRack {
	return []toggleRack{
		{"pads", u.padRackObj, u.padBtn},
		{"seq", u.seqRack.Object(), u.seqBtn},
		{"keys", u.keyboardRack.Object(), u.keysBtn},
		{"rec", u.recRack.Object(), u.recBtn},
		{"fx", u.fxRack.Object(), u.padFXBtn},
		{"paks", u.paksRack.Object(), u.paksBtn},
		{"vu", u.meterArea, u.meterBtn},
	}
}

// savePageVis records the current show/hide state of the per-page content racks
// under page, so returning to it restores that configuration. No-op for the
// empty (single-page) id.
func (u *ui) savePageVis(page string) {
	if page == "" {
		return
	}
	if u.pageVis == nil {
		u.pageVis = map[string]map[string]bool{}
	}
	m := make(map[string]bool)
	for _, r := range u.toggleRacks() {
		m[r.id] = r.obj.Visible()
	}
	u.pageVis[page] = m
}

// loadPageVis applies a page's remembered rack show/hide state; on a page's
// first visit it falls back to the factory defaults (defaultVis) so the page
// starts fresh rather than inheriting the previous page's toggles. The incoming
// variant's `show:` overrides still apply on top during relayout. A disabled
// toggle's rack stays hidden.
func (u *ui) loadPageVis(page string) {
	m, ok := u.pageVis[page]
	if !ok {
		m = u.defaultVis
	}
	for _, r := range u.toggleRacks() {
		if v, ok := m[r.id]; ok {
			u.setVisible(r.obj, r.btn, v && !r.btn.Disabled())
		}
	}
}

// captureDefaultVis records the current (build-time default) visibility of the
// per-page content racks as the fallback for a page's first visit. Called once
// from build() after the default visibility is set.
func (u *ui) captureDefaultVis() {
	u.defaultVis = make(map[string]bool)
	for _, r := range u.toggleRacks() {
		u.defaultVis[r.id] = r.obj.Visible()
	}
}

// cyclePage moves delta pages from the active one, wrapping — the keyboard
// navigation behind Ctrl+Shift+Left / Ctrl+Shift+Right. No-op with <2 pages.
func (u *ui) cyclePage(delta int) {
	n := len(u.pages)
	if n < 2 {
		return
	}
	idx := 0
	for i, pg := range u.pages {
		if pg.ID == u.activePage {
			idx = i
			break
		}
	}
	idx = ((idx+delta)%n + n) % n
	u.setPage(u.pages[idx].ID)
}

// setConsole enters (on) or leaves (off) the "mixing console" layout, then
// re-lays out. On desktop it also drives the OS window: the console is full
// screen, while windowed is a single fixed, non-resizable size — so entering
// clears the fixed-size lock before going full screen and leaving restores the
// fixed windowed size. On mobile there's no console (the phone/tablet variants
// are chosen by device size), so setConsole is desktop-only in practice.
//
// The console force-shows some racks (FX, KEYS, PAKS, SEQ via `show: true`). We
// restore them to the user's prior state when leaving the console, so they don't
// leak into the windowed layout. This is done here (a single-threaded user
// action), not in relayout, so a background resize-driven relayout can't hide
// racks mid-build. The set is generic — whatever a variant force-shows via
// `show:` is recorded in u.forced by applyRackShow and restored here.
//
// SetFullScreen is applied asynchronously (Fyne queues it onto the main loop),
// so we can't rely on the canvas size here; the console layout keys off the
// fullscreen intent (not pixel size) and its proportional splits adapt as the
// window settles, so the synchronous relayout below is authoritative.
func (u *ui) setConsole(on bool) {
	if on {
		u.forced = nil // the entering variant's applyRackShow repopulates it
	}
	u.fullScreen = on
	rememberConsole(on) // persist the choice so it's restored next launch
	if !onMobile && u.win != nil {
		if on {
			// Behave like a normal resizable app while full screen: unlock the
			// fixed size so the window isn't a fixed-size window in full screen
			// (which confused Mutter's geometry restore on exit).
			u.win.SetFixedSize(false)
			u.win.SetFullScreen(true)
		} else {
			// Leave full screen. Mutter restores the pre-full-screen frame to the
			// windowed size, but glfw-Wayland doesn't always propagate that to
			// Fyne's canvas, so it can stay laid out at the full-screen size. An
			// explicit Resize reconciles the canvas with the restored frame (both
			// are the windowed size now, so this doesn't fight the compositor). The
			// fixed-size lock is re-applied once it settles (see onCanvasResize).
			u.win.SetFullScreen(false)
			// Set the re-lock flag before Resize so the resize it triggers re-locks
			// the fixed windowed size once it settles (see onCanvasResize).
			u.relockWindowed = true
			u.win.Resize(fyne.NewSize(windowedWidth, windowedHeight))
		}
		u.diagConsole(on)
	}
	if !on {
		u.restoreForcedRacks() // put the force-shown racks back before relayout
	}
	u.relayout() // immediate variant switch; onCanvasResize corrects the sizing
}

// diagConsole logs the console/window transition and the resulting canvas size
// when RP6_DIAG=1, to diagnose compositor-specific full-screen-exit sizing (some
// Wayland compositors don't restore the windowed size). No-op otherwise.
func (u *ui) diagConsole(on bool) {
	if os.Getenv("RP6_DIAG") == "" || u.win == nil || u.win.Canvas() == nil {
		return
	}
	sz := u.win.Canvas().Size()
	log.Printf("rp6: setConsole(%v) -> fullScreen()=%v canvas=%.0fx%.0f (windowed target %dx%d)",
		on, u.win.FullScreen(), sz.Width, sz.Height, windowedWidth, windowedHeight)
}

// diagRelock logs when the fixed windowed size is re-applied after a console
// exit (RP6_DIAG=1), so the compositor's geometry restore can be confirmed.
func (u *ui) diagRelock(size fyne.Size) {
	if os.Getenv("RP6_DIAG") == "" {
		return
	}
	log.Printf("rp6: re-locked windowed size at canvas=%.0fx%.0f", size.Width, size.Height)
}

// savedRack captures a toggleable rack's visibility so it can be restored.
type savedRack struct {
	obj fyne.CanvasObject
	btn *components.RackToggle
	on  bool
}

// restoreForcedRacks undoes the visibility overrides the previous layout variant
// applied via `show:`, restoring each rack to the visibility it had before that
// variant forced it. Called on a variant switch, before the new variant's
// overrides are (re)applied during the build.
func (u *ui) restoreForcedRacks() {
	for _, s := range u.forced {
		u.setVisible(s.obj, s.btn, s.on && !s.btn.Disabled())
	}
	u.forced = nil
}

// addRackShortcut binds Ctrl+Shift+<key> to fn on the window canvas.
func (u *ui) addRackShortcut(w fyne.Window, key fyne.KeyName, fn func()) {
	w.Canvas().AddShortcut(
		&desktop.CustomShortcut{KeyName: key, Modifier: fyne.KeyModifierControl | fyne.KeyModifierShift},
		func(fyne.Shortcut) { fn() },
	)
}

// numSeqSlots is the number of saved-sequence slots.
const numSeqSlots = 16

// prefKeyEmuDir is the app-preferences key holding the last emulator samples
// directory picked at runtime. The sequence store's meta table is profile-
// scoped and can't hold it (we don't know the profile until we know emuDir),
// so this lives in the app's global preferences and survives a restart.
const prefKeyEmuDir = "emu.samplesDir"

const (
	prefKeyFXTone   = "keysfx.tone"
	prefKeyFXComp   = "keysfx.comp"
	prefKeyFXChorus = "keysfx.chorus"
	prefKeyFXDelay  = "keysfx.delay"
	prefKeyFXReverb = "keysfx.reverb"
)

func loadKeyboardFX() audiofx.Settings {
	app := fyne.CurrentApp()
	if app == nil {
		return audiofx.Settings{}
	}
	p := app.Preferences()
	return audiofx.Settings{
		Tone:   float32(p.IntWithFallback(prefKeyFXTone, 0)) / 100,
		Comp:   float32(p.IntWithFallback(prefKeyFXComp, 0)) / 100,
		Chorus: float32(p.IntWithFallback(prefKeyFXChorus, 0)) / 100,
		Delay:  float32(p.IntWithFallback(prefKeyFXDelay, 0)) / 100,
		Reverb: float32(p.IntWithFallback(prefKeyFXReverb, 0)) / 100,
	}
}

func rememberKeyboardFX(s audiofx.Settings) {
	if app := fyne.CurrentApp(); app != nil {
		p := app.Preferences()
		p.SetInt(prefKeyFXTone, int(s.Tone*100))
		p.SetInt(prefKeyFXComp, int(s.Comp*100))
		p.SetInt(prefKeyFXChorus, int(s.Chorus*100))
		p.SetInt(prefKeyFXDelay, int(s.Delay*100))
		p.SetInt(prefKeyFXReverb, int(s.Reverb*100))
	}
}

func (u *ui) setKeyboardFX(settings audiofx.Settings) {
	u.keyboardFX = settings
	rememberKeyboardFX(settings)
	u.devMu.Lock()
	dev := u.dev
	u.devMu.Unlock()
	if target, ok := dev.(interface{ SetKeyboardFX(audiofx.Settings) }); ok {
		target.SetKeyboardFX(settings)
	}
	u.setStatus("keyboard effects updated")
}

type keyboardFXController interface {
	SetKeyboardFX(audiofx.Settings)
	SetKeyboardFXEnabled(bool)
}

type recorderController interface {
	RecorderFormat() (channels, sampleRate int)
	SetRecorder(*recorder.Engine, func([]float32))
}

func (u *ui) applyKeyboardFXEnabled() {
	enabled := u.useEmu && u.keyboardFXRack != nil && u.keyboardFXRack.Object().Visible()
	u.devMu.Lock()
	dev := u.dev
	u.devMu.Unlock()
	if target, ok := dev.(keyboardFXController); ok {
		target.SetKeyboardFXEnabled(enabled)
	}
}

// rememberEmuDir persists the emulator samples directory so the next launch
// reopens that pak instead of the built-in kit.
func (u *ui) rememberEmuDir(dir string) {
	if app := fyne.CurrentApp(); app != nil {
		app.Preferences().SetString(prefKeyEmuDir, strings.TrimSpace(dir))
	}
}

// savedEmuDir returns the last remembered emulator samples directory, or "" if
// none was saved or the saved directory no longer exists (a stale pointer, e.g.
// the pak was moved or deleted — the caller then falls back to the built-in kit).
func (u *ui) savedEmuDir() string {
	app := fyne.CurrentApp()
	if app == nil {
		return ""
	}
	dir := strings.TrimSpace(app.Preferences().String(prefKeyEmuDir))
	if dir == "" {
		return ""
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ""
	}
	return dir
}

// prefKeyPadLayout is the app-preferences key holding the pad-grid layout
// (paged / two-bank / dense). It's a global UI preference — not tied to a
// device profile — so it lives in the app's preferences and survives a restart.
const prefKeyPadLayout = "pad.layout"

// loadPadLayout returns the remembered pad layout, defaulting to the two-bank
// layout when nothing valid is saved (or no app is running, e.g. in tests).
func loadPadLayout() padLayout {
	if app := fyne.CurrentApp(); app != nil {
		v := app.Preferences().IntWithFallback(prefKeyPadLayout, int(layoutPaged))
		if v >= 0 && v < numLayouts {
			return padLayout(v)
		}
	}
	return layoutPaged
}

// rememberPadLayout persists the pad layout so the next launch restores it.
func rememberPadLayout(l padLayout) {
	if app := fyne.CurrentApp(); app != nil {
		app.Preferences().SetInt(prefKeyPadLayout, int(l))
	}
}

// prefKeyConsole holds the console-layout on/off choice (a global UI preference,
// not tied to a device profile), stored as 1/0 with -1 meaning "never set".
const prefKeyConsole = "console.on"

// tabletMinDP is the smallest-side threshold (in Fyne's density-independent
// units) at/above which a touch screen counts as a tablet — the standard Android
// "sw600dp" tablet breakpoint. Tablets default to the wide console layout.
const tabletMinDP = 600

// loadConsolePref returns the remembered console-layout state and whether one was
// ever saved (so first launch can pick a device-appropriate default instead).
func loadConsolePref() (on, saved bool) {
	if app := fyne.CurrentApp(); app != nil {
		switch app.Preferences().IntWithFallback(prefKeyConsole, -1) {
		case 0:
			return false, true
		case 1:
			return true, true
		}
	}
	return false, false
}

// rememberConsole persists an explicit console-layout choice (see setConsole).
func rememberConsole(on bool) {
	if app := fyne.CurrentApp(); app != nil {
		v := 0
		if on {
			v = 1
		}
		app.Preferences().SetInt(prefKeyConsole, v)
	}
}

// prefKeyPage holds the last active application page id (see setPage), a global
// UI preference restored on launch so a session resumes on the page it left.
const prefKeyPage = "ui.page"

// savedPage returns the remembered active-page id, or "" if none was saved.
func savedPage() string {
	if app := fyne.CurrentApp(); app != nil {
		return strings.TrimSpace(app.Preferences().String(prefKeyPage))
	}
	return ""
}

// rememberPage persists the active application page so the next launch restores it.
func rememberPage(id string) {
	if app := fyne.CurrentApp(); app != nil {
		app.Preferences().SetString(prefKeyPage, strings.TrimSpace(id))
	}
}

// isTabletSize reports whether a canvas size (in density-independent units) is a
// tablet-class screen (smallest side ≥ tabletMinDP).
func isTabletSize(size fyne.Size) bool {
	return min(size.Width, size.Height) >= tabletMinDP
}

// storeProfile returns the persistence profile for the active backend, so
// sequences stay scoped to the endpoint they were made for: "p6" for the
// hardware (and any no-emulator run), or "emu:<abs-samples-dir>" for an
// emulator kit (each sample directory keeps its own sequences).
func (u *ui) storeProfile() string {
	if !u.useEmu {
		return store.DefaultProfile
	}
	dir := strings.TrimSpace(u.emuDir)
	if dir == "" {
		return "emu:builtin" // the embedded default kit's own profile
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return "emu:" + filepath.Clean(dir)
}

// deviceIdentity returns the top-rack badge's name, mode tag and accent color
// for the active backend: a cyan "EMULATOR / SOFTWARE" plate when the emulator
// is active, or an amber "P-6 / USB MIDI" plate for the real hardware. The
// accent color doubles as a hardware-vs-emulator cue.
func (u *ui) deviceIdentity() (name, tag string, accent color.NRGBA) {
	if u.useEmu {
		return "EMULATOR", "SOFTWARE", deviceEmuAccent
	}
	return "P-6", "USB MIDI", deviceHwAccent
}

// showDeviceSettings opens the device-specific settings window (the badge's
// gear). The emulator's lets you pick its samples directory; the P-6's is a
// placeholder for now.
func (u *ui) showDeviceSettings() {
	if u.useEmu {
		u.showEmuSettings()
		return
	}
	body := widget.NewLabel(
		"P-6 settings will live here.\n\nComing soon: per-device configuration.\n\nTip: tap the badge to switch to the emulator.")
	body.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustom("P-6 — Settings", "Close", body, u.win)
	d.Resize(fyne.NewSize(360, 220))
	d.Show()
}

// showEmuSettings shows the emulator's settings: the current samples directory
// (and how many pads loaded) plus a folder picker to choose/replace it.
func (u *ui) showEmuSettings() {
	cur := u.emuDir
	if strings.TrimSpace(cur) == "" {
		cur = emu.DefaultKitName
	}
	dirLabel := widget.NewLabel(cur)
	dirLabel.Wrapping = fyne.TextWrapWord

	loaded := "not loaded"
	if e, ok := u.dev.(*emu.Emulator); ok {
		loaded = fmt.Sprintf("%d/%d pads loaded", e.Loaded(), p6.NumPads)
	}
	loadedLabel := widget.NewLabel(loaded)

	var dlg dialog.Dialog
	choose := widget.NewButtonWithIcon("Choose samples folder…", theme.FolderOpenIcon(), func() {
		fd := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				log.Printf("rp6emu: folder pick: uri=%v err=%v", uri, err)
				return
			}
			log.Printf("rp6emu: picked folder uri=%q path=%q", uri.String(), uri.Path())
			dir, err := u.resolveEmuSamples(uri)
			if err != nil {
				log.Printf("rp6emu: resolveEmuSamples failed: %v", err)
				u.setStatus("couldn't load samples: " + err.Error())
				return
			}
			u.setEmuSamples(dir)
			if dlg != nil {
				dlg.Hide()
			}
		}, u.win)
		fd.Show()
	})

	content := container.NewVBox(
		widget.NewLabelWithStyle("Emulator samples directory", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		dirLabel,
		loadedLabel,
		widget.NewLabel("Layout: flat A1.wav..H6.wav, A/1.wav.., or BANK_A/PAD_1/*.wav (WAV or FLAC)"),
		choose,
	)
	for _, o := range u.emuSettingsExtra(func() {
		if dlg != nil {
			dlg.Hide()
		}
	}) {
		content.Add(o)
	}
	dlg = dialog.NewCustom("EMULATOR — Settings", "Close", content, u.win)
	dlg.Resize(fyne.NewSize(440, 280))
	dlg.Show()
}

// fallbackToEmu switches to the emulator (built-in "modular-hits" kit unless a
// samples dir is set) when the P-6 can't be reached — on launch or after a
// mid-session unplug — so the app is always playable. It flags emuFallback so
// the device watcher auto-reconnects to a P-6 once one appears. No-op if already
// on the emulator.
func (u *ui) fallbackToEmu() {
	if u.useEmu {
		return
	}
	u.switchBackend(true)
	u.emuFallback.Store(true) // we want the P-6 back — watcher will reconnect
	u.setStatus("no P-6 — using the built-in emulator (auto-reconnects)")
}

// toggleBackend switches the active backend between the P-6 hardware and the
// emulator (bound to the device badge tap). Switching to the P-6 needs
// one actually connected; switching to the emulator is always allowed (pick its
// samples via the gear if none were given on the command line). Either way this
// is a *deliberate* choice, so it clears the auto-reconnect flag.
func (u *ui) toggleBackend() {
	if u.useEmu {
		if _, err := p6.Discover(); err != nil {
			u.setStatus("no P-6 detected — can't switch (check USB + power)")
			return
		}
		u.switchBackend(false)
		u.emuFallback.Store(false)
		return
	}
	u.switchBackend(true)
	u.emuFallback.Store(false) // deliberate emulator use — don't auto-switch back
}

// switchBackend flips to the requested backend and reconnects. No-op if already
// on it. Switching to the emulator with no samples directory chosen loads the
// built-in "modular-hits" kit; pick another folder from the gear settings.
func (u *ui) switchBackend(useEmu bool) {
	if u.useEmu == useEmu {
		return
	}
	u.useEmu = useEmu
	u.reconnectProfile() // connect() enables/disables input listening for the backend
	name, _, _ := u.deviceIdentity()
	u.setStatus(fmt.Sprintf("switched to %s", name))
}

// setListenDefault turns MIDI-input reflection on by default whenever there's
// input to react to: the P-6's own pad presses (hardware backend), or an
// external MIDI controller (which drives the emulator host-side too). The
// emulator with no controller has no input, so it defaults off. Called from
// connect on every (re)connection and when a controller attaches.
func (u *ui) setListenDefault() {
	u.listenMIDI.Store(!u.useEmu || len(u.midiIns) > 0)
	u.updateMIDIInBtn()
}

// setEmuSamples points the emulator at a new samples directory and reconnects
// (also switching to the emulator backend if not already on it). Each directory
// is its own persistence profile, so this re-scopes and reloads sequences.
//
// Decoding + resampling a pak's samples is the slow part of the switch, so it
// runs off the UI thread: the current backend keeps playing while the new kit
// loads, then it's swapped in via fyne.Do (openDevice adopts u.preopened). The
// loadingSamples guard drops taps that arrive while a load is already running.
func (u *ui) setEmuSamples(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	if !u.loadingSamples.CompareAndSwap(false, true) {
		u.setStatus("still loading a sample pak — one moment…")
		return
	}
	log.Printf("rp6emu: setEmuSamples dir=%q", dir)
	u.setStatus("loading sample pak…")
	go func() {
		t := time.Now()
		dev, err := emu.Open(dir, p6.DefaultConfig()) // decode + resample, off the UI thread
		perfLap("setEmuSamples: emu.Open (bg)", &t)
		fyne.Do(func() {
			st := time.Now()
			defer func() {
				perfLap("setEmuSamples: swap (UI thread)", &st)
				u.loadingSamples.Store(false)
			}()
			if err != nil {
				log.Printf("rp6emu: emu.Open(%q) failed: %v", dir, err)
				u.setStatus("couldn't load samples: " + err.Error())
				return
			}
			u.emuDir = dir
			u.useEmu = true
			u.emuFallback.Store(false) // picking a kit is a deliberate emulator choice
			u.rememberEmuDir(dir)      // survive a restart (see vxrv)
			u.preopened = dev          // connect() adopts this instead of loading inline
			u.reconnectProfile()
			u.setStatus("emulator samples: " + dir)
		})
	}()
}

// reconnectProfile stops transport, persists the outgoing profile's sequence,
// repaints the badge for the current backend, reconnects, re-scopes persistence
// to the (possibly changed) profile and reloads its sequence. The store is
// re-opened from storeProfile(), so callers only set useEmu/emuDir beforehand.
func (u *ui) reconnectProfile() {
	t := time.Now()
	u.seq.Stop()
	u.rec.StopRecordingImmediate()
	u.rec.StopAllImmediate()
	u.playBtn.SetRunning(false)
	u.autosaveSeq() // persist under the still-open outgoing profile
	u.autosaveRecorder()
	perfLap("reconnect: autosave", &t)
	if u.store != nil {
		_ = u.store.Close()
		u.store = nil
	}
	perfLap("reconnect: store.Close", &t)

	name, tag, accent := u.deviceIdentity()
	u.deviceBadge.SetAccent(accent)
	u.deviceBadge.SetName(name, tag)

	u.connect() // open the new backend (also updates badge state)
	perfLap("reconnect: connect", &t)
	u.openStore() // re-scope persistence to the new profile
	perfLap("reconnect: openStore", &t)
	u.loadInitialSequence() // load that profile's last sequence
	u.loadRecorder()
	if u.audioStarted {
		u.startAudio()
	}
	perfLap("reconnect: loadInitialSequence", &t)
	u.applyBackendGating() // show/hide the P-6-only rack for the new backend
	perfLap("reconnect: applyBackendGating", &t)
}

func (u *ui) openStore() {
	path, err := store.DefaultPath()
	// On mobile $HOME/.local isn't writable (scoped storage); use the app's
	// private storage directory instead so sequences persist.
	if onMobile {
		if p, ok := mobileStorePath(); ok {
			path, err = p, nil
		}
	}
	if err != nil {
		log.Printf("rp6: store path: %v (persistence disabled)", err)
		return
	}
	profile := u.storeProfile()
	s, err := store.Open(path, profile)
	if err != nil {
		log.Printf("rp6: store open: %v (persistence disabled)", err)
		return
	}
	u.store = s
	log.Printf("rp6: sequences stored in %s (profile %q)", path, profile)
}

// mobileStorePath returns a writable path for the sequence database inside the
// app's private storage (Android/iOS), or ok=false if it can't be determined.
func mobileStorePath() (string, bool) {
	app := fyne.CurrentApp()
	if app == nil {
		return "", false
	}
	root := app.Storage().RootURI()
	if root == nil || root.Path() == "" {
		return "", false
	}
	return filepath.Join(root.Path(), "rp6.db"), true
}

// loadInitialSequence loads the last-used slot (or slot 1) on startup.
func (u *ui) loadInitialSequence() {
	slot := 1
	if u.store != nil {
		if v, ok, _ := u.store.Meta("last"); ok {
			if n, err := strconv.Atoi(v); err == nil {
				slot = n
			}
		}
	}
	u.loadSlot(slot)
}

// defaultSeqState returns a fresh sequence (default tracks, no steps).
func (u *ui) defaultSeqState() sequencer.State {
	steps := u.seq.MaxBars() * sequencer.StepsPerBar
	st := sequencer.State{Version: 1, Tempo: u.bpm, Tracks: defaultTracks, Data: make([]sequencer.TrackState, u.seq.MaxTracks())}
	for t := range st.Data {
		st.Data[t] = sequencer.TrackState{Pad: t, Bars: 1, Steps: make([]bool, steps)}
	}
	return st
}

// selectSlot handles the sequencer's slot +/- (or a typed value). While the
// sequencer is playing the change is queued and applied at the next bar so the
// switch is musically seamless; otherwise it loads immediately.
func (u *ui) selectSlot(slot int) {
	if u.seq.Running() {
		u.pendingSlot = slot
		u.seqRack.setSlotPending(true) // flash the SEQ knob until the next bar
		u.setStatus(fmt.Sprintf("S%02d queued (next bar)", slot))
		return
	}
	u.loadSlot(slot)
}

// maybeApplyPendingAt loads a queued slot change when tick lands on a bar
// boundary (called from the sequencer's step callback on the main thread).
func (u *ui) maybeApplyPendingAt(tick int) {
	if u.pendingSlot != 0 && tick%sequencer.StepsPerBar == 0 {
		slot := u.pendingSlot
		u.pendingSlot = 0
		u.loadSlot(slot)
	}
}

// applyPendingSlot loads any queued slot change now (e.g. when the sequencer
// stops, so the readout and the loaded sequence stay in sync).
func (u *ui) applyPendingSlot() {
	if u.pendingSlot != 0 {
		slot := u.pendingSlot
		u.pendingSlot = 0
		u.loadSlot(slot)
	}
}

// loadSlot loads a saved sequence (or a fresh one if the slot is empty) into
// the engine and refreshes the sequencer UI.
func (u *ui) loadSlot(slot int) {
	u.pendingSlot = 0               // a direct load supersedes any queued change
	u.seqRack.setSlotPending(false) // resolved — stop the flashing border
	// Autosave the working slot before switching so in-progress edits (track
	// count, steps, mutes, …) survive navigation, like the quit-time autosave.
	if u.store != nil && u.seqSlot >= 1 && u.seqSlot != slot {
		u.autosaveSeq()
	}
	u.seqSlot = slot
	u.seqRack.SetSlot(slot)

	st := u.defaultSeqState()
	name := ""
	if u.store != nil {
		if n, data, ok, err := u.store.Load(slot); err != nil {
			log.Printf("rp6: load slot %d: %v", slot, err)
		} else if ok {
			var loaded sequencer.State
			if err := json.Unmarshal(data, &loaded); err == nil {
				st, name = loaded, n
			}
		}
		_ = u.store.SetMeta("last", strconv.Itoa(slot))
	}

	u.seq.Restore(st)
	u.seqRack.SetSeqName(name)
	if st.Tempo > 0 {
		u.tempo.SetValue(int(st.Tempo)) // syncs bpm/clock/fx/seq via onTempoChange
	}
	u.seqRack.syncFromEngine()
	u.setStatus(fmt.Sprintf("sequence S%02d loaded", slot))
}

// copyToSlot duplicates the current working sequence into slot (Ctrl-click on
// the sequencer's + button). Existing sequences at slot and beyond are shifted
// one slot to the right to make room, so the copy is inserted, not overwritten.
func (u *ui) copyToSlot(slot int) {
	u.pendingSlot = 0 // a copy supersedes any queued change
	u.seqRack.setSlotPending(false)
	if u.store == nil {
		u.setStatus("no storage available")
		u.seqRack.SetSlot(u.seqSlot)
		return
	}
	// Preserve the source slot before inserting.
	if u.seqSlot >= 1 && u.seqSlot != slot {
		u.autosaveSeq()
	}
	switch ok, err := u.store.InsertGap(slot, numSeqSlots); {
	case err != nil:
		u.setStatus("copy error: " + err.Error())
		u.seqRack.SetSlot(u.seqSlot)
		return
	case !ok:
		u.setStatus("no free slot to insert copy")
		u.seqRack.SetSlot(u.seqSlot)
		return
	}
	u.seqSlot = slot
	u.seqRack.SetSlot(slot)
	u.persistSeq()
	u.seqRack.syncFromEngine()
	u.setStatus(fmt.Sprintf("sequence copied to S%02d", slot))
}

// deleteSlot deletes the current sequence (Ctrl-click on the sequencer's Clear
// button): it removes the slot from the store, shifts the following sequences
// left to close the gap, then reloads the current slot position (now the
// shifted-in sequence, or a fresh empty one).
func (u *ui) deleteSlot() {
	if u.store == nil {
		u.seq.Clear()
		u.seqRack.refreshCells()
		u.setStatus("cleared (no storage)")
		return
	}
	slot := u.seqSlot
	if err := u.store.DeleteSlot(slot, numSeqSlots); err != nil {
		u.setStatus("delete error: " + err.Error())
		return
	}
	u.seqSlot = 0    // don't autosave the just-deleted content back on reload
	u.loadSlot(slot) // reload: the shifted-in sequence, or empty
	u.setStatus(fmt.Sprintf("deleted sequence S%02d", slot))
}

// saveSeq prompts for the sequence name, then writes it to the active slot.
func (u *ui) saveSeq() {
	if u.store == nil {
		u.setStatus("no storage available")
		return
	}
	entry := widget.NewEntry()
	entry.SetPlaceHolder("sequence name")
	entry.SetText(u.seqRack.SeqName())
	form := dialog.NewForm(
		fmt.Sprintf("Save sequence S%02d", u.seqSlot), "Save", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Name", entry)},
		func(ok bool) {
			if !ok {
				return
			}
			u.seqRack.SetSeqName(entry.Text)
			u.persistSeq()
		}, u.win)
	form.Resize(fyne.NewSize(320, form.MinSize().Height))
	form.Show()
}

// persistSeq writes the current sequence + name to the active slot.
func (u *ui) persistSeq() {
	data, err := json.Marshal(u.seq.Snapshot())
	if err != nil {
		u.setStatus("save error: " + err.Error())
		return
	}
	if err := u.store.Save(u.seqSlot, u.seqRack.SeqName(), data); err != nil {
		u.setStatus("save error: " + err.Error())
		return
	}
	_ = u.store.SetMeta("last", strconv.Itoa(u.seqSlot))
	u.setStatus(fmt.Sprintf("saved sequence S%02d", u.seqSlot))
}

// autosaveSeq persists the working sequence to its slot (called on quit).
func (u *ui) autosaveSeq() {
	if u.store == nil {
		return
	}
	if data, err := json.Marshal(u.seq.Snapshot()); err == nil {
		_ = u.store.Save(u.seqSlot, u.seqRack.SeqName(), data)
		_ = u.store.SetMeta("last", strconv.Itoa(u.seqSlot))
	}
}

// setVisible sets an object's visibility and lights/greys its rack toggle.
func (u *ui) setVisible(o fyne.CanvasObject, btn *components.RackToggle, visible bool) {
	if visible {
		o.Show()
	} else {
		o.Hide()
	}
	btn.SetOn(visible)
	if btn == u.padBtn || btn == u.seqBtn || btn == u.keysBtn {
		u.updatePlayMenuButton()
	}
	if btn == u.padFXBtn || btn == u.keysFXBtn {
		u.updateFXButton()
	}
	if u.keyboardFXRack != nil && o == u.keyboardFXRack.Object() {
		u.applyKeyboardFXEnabled()
	}
}

// applyRackShow applies a toggle-able rack's `show:` visibility from the layout,
// but only when the variant was just entered (variantChanged) — so a variant
// declares its default visible racks without overriding the user's manual toggle
// while that variant stays on screen. It records the rack's prior visibility in
// u.forced (keyed by id, first time only) so restoreForcedRacks can put it back
// when the console is left (see setConsole). Called from configureComponent.
func (u *ui) applyRackShow(id string, props map[string]string, o fyne.CanvasObject, btn *components.RackToggle) {
	if !u.variantChanged {
		return
	}
	s, ok := props["show"]
	if !ok {
		return
	}
	if u.forced == nil {
		u.forced = map[string]savedRack{}
	}
	if _, seen := u.forced[id]; !seen {
		u.forced[id] = savedRack{obj: o, btn: btn, on: o.Visible()} // remember prior state once
	}
	u.setVisible(o, btn, s == "true")
}

// toggleVisible flips an object's visibility and re-lays out the window.
func (u *ui) toggleVisible(o fyne.CanvasObject, btn *components.RackToggle) {
	u.setVisible(o, btn, !o.Visible())
	if u.root != nil {
		u.root.Refresh()
	}
}

// showInfo opens a dialog with connection, audio, and setup details.
func (u *ui) showInfo() {
	cfg := p6.DefaultConfig()
	midi := "not connected"
	if u.dev != nil {
		cfg = u.dev.Config()
		midi = "connected — `" + u.dev.Path() + "`"
	}
	mode := "MIDI"
	if u.useEmu {
		mode = "Emulator"
	}

	meter := "activity (no audio capture backend — build with `-tags capture`)"
	if u.audioMeter != nil {
		meter = "live capture — " + u.audioDevice
	}

	selected := "none"
	if u.selPad >= 0 {
		b, n := padBankNumber(u.selPad)
		selected = p6.PadLabel(b, n)
	}

	// Sample-kit credits (emulator only): the built-in kit carries an
	// attribution; a user-picked folder just names its path.
	kitSection := ""
	if u.useEmu {
		if strings.TrimSpace(u.emuDir) == "" {
			kitSection = fmt.Sprintf(`

### Sample kit
**%s** — %s`, emu.DefaultKitName, emu.DefaultKitAttribution)
		} else {
			kitSection = fmt.Sprintf(`

### Sample kit
User samples — `+"`%s`", u.emuDir)
		}
	}

	md := fmt.Sprintf(`## RP6 — P-6 Pad Controller

**%s**

### %s
%s

Channels — Sampler **%d** · Granular **%d** · Auto **%d** · Program **%d**

### Audio meter
%s

### State
Tempo **%.0f BPM** · Pattern **%s** · Selected pad **%s**%s

### P-6 setup reminders
- Transport (Play/Stop/tempo) needs MENU → **SYnC = USB**
- Pattern switching needs MENU → **rxPc = On**
- Bank switching, sample-pad params and hardware LOOP have **no MIDI** — the app
  addresses pads absolutely and rolls are host-side retriggers.`,
		version, mode, midi, cfg.SamplerChannel, cfg.GranularChannel, cfg.AutoChannel, cfg.ProgramChannel,
		meter, u.bpm, patternName(u.patternStep.Value()), selected, kitSection)

	content := widget.NewRichTextFromMarkdown(md)
	content.Wrapping = fyne.TextWrapWord
	d := dialog.NewCustom("Info", "Close", content, u.win)
	d.Resize(fyne.NewSize(460, 460))
	d.Show()
}

// startDeviceWatch polls for a P-6 while we're on the emulator due to a missing
// device (emuFallback), and auto-reconnects to hardware the moment one shows up
// — so the user never has to press a Reconnect button. It fires only on a rising
// edge (P-6 newly present), so a present-but-busy P-6 won't cause a retry storm;
// a deliberate emulator choice (emuFallback=false) suppresses it entirely.
func (u *ui) startDeviceWatch() {
	u.watchStop = make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		prevPresent := false
		for {
			select {
			case <-u.watchStop:
				return
			case <-t.C:
				present := false
				if _, err := p6.Discover(); err == nil {
					present = true
				}
				rising := present && !prevPresent
				prevPresent = present
				if !rising || !u.emuFallback.Load() {
					continue
				}
				fyne.Do(func() {
					if !u.emuFallback.Load() {
						return // state changed before this ran
					}
					u.setStatus("P-6 detected — connecting")
					u.switchBackend(false) // to hardware
					u.emuFallback.Store(false)
				})
			}
		}
	}()
}

// stopDeviceWatch halts the P-6 watcher (idempotent).
func (u *ui) stopDeviceWatch() {
	if u.watchStop != nil {
		close(u.watchStop)
		u.watchStop = nil
	}
}

// startMIDIInputWatch periodically (re)attaches external MIDI input controllers —
// so hot-plugging, swapping, or adding a second controller (e.g. a keyboard
// alongside a macropad) is picked up live, without restarting rp6. It shares the
// device watcher's lifecycle (watchStop), so call it after startDeviceWatch.
// startMIDIInput is idempotent per node path (and keeps its own miss-logged guard
// so it doesn't spam the log while nothing is plugged in).
func (u *ui) startMIDIInputWatch() {
	stop := u.watchStop
	if stop == nil {
		return
	}
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				// Attach any newly-plugged controllers on the UI thread (where
				// u.midiIns is only ever touched). startMIDIInput is idempotent
				// per node path, so already-open controllers are left running.
				fyne.Do(func() {
					u.startMIDIInput()
				})
			}
		}
	}()
}

// startMeter runs a lightweight animator that drives the meter from the current
// level source each frame. It runs until stopMeter is closed (on shutdown) so
// it can't keep posting UI updates once the Fyne run loop is tearing down — at
// which point fyne.Do would run inline off the main goroutine and trip Fyne's
// thread checker.
func (u *ui) startMeter() {
	u.meterStop = make(chan struct{})
	go func() {
		t := time.NewTicker(40 * time.Millisecond)
		defer t.Stop()
		var pending atomic.Bool // coalesce: don't flood the UI loop faster than it drains
		for {
			select {
			case <-u.meterStop:
				return
			case <-t.C:
				if pending.Load() {
					continue // last update still queued — skip so the queue can't grow
				}
				pending.Store(true)
				fyne.Do(func() {
					u.requestRelayoutIfScaleChanged()
					src := u.meterSrc
					src.step()
					u.meter.SetLevel(src.level())
					pending.Store(false)
				})
			}
		}
	}()
}

// stopMeter halts the meter animator (idempotent).
func (u *ui) stopMeter() {
	if u.meterStop != nil {
		close(u.meterStop)
		u.meterStop = nil
	}
}

// startDiagnostics (enable with RP6_DIAG=1) logs UI-loop latency and window size
// changes, to diagnose multi-window/maximize performance. It's a no-op unless
// the env var is set, so it has zero cost in normal use.
func (u *ui) startDiagnostics() {
	if os.Getenv("RP6_DIAG") == "" {
		return
	}
	log.Printf("rp6/diag: enabled — logging UI-loop lag and window sizes")
	go func() {
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		var lastMain, lastPad fyne.Size
		for range t.C {
			sched := time.Now()
			fyne.Do(func() {
				// How long did this closure wait to run? High = the render loop
				// is starved (e.g. blocked painting/swapping a maximized window).
				if lag := time.Since(sched); lag > 60*time.Millisecond {
					log.Printf("rp6/diag: UI-loop lag %v", lag.Round(time.Millisecond))
				}
				if u.win != nil {
					if s := u.win.Canvas().Size(); s != lastMain {
						log.Printf("rp6/diag: main window resized -> %.0fx%.0f", s.Width, s.Height)
						lastMain = s
					}
				}
				if u.padWin != nil {
					if s := u.padWin.Canvas().Size(); s != lastPad {
						log.Printf("rp6/diag: pad window resized -> %.0fx%.0f", s.Width, s.Height)
						lastPad = s
					}
				}
			})
		}
	}()

	// Watchdog: ping the render loop; if it doesn't respond within 1.5s it's
	// stalled, so dump every goroutine's stack (which shows what the GLFW loop
	// goroutine is blocked in — PollEvents / SwapBuffers / SetWindowSize / …).
	go func() {
		dumped := false
		for {
			time.Sleep(time.Second)
			done := make(chan struct{})
			fyne.Do(func() { close(done) })
			select {
			case <-done:
				dumped = false // loop healthy again; allow the next dump
			case <-time.After(1500 * time.Millisecond):
				if !dumped {
					buf := make([]byte, 1<<20)
					n := runtime.Stack(buf, true)
					path := "/tmp/rp6-stall.txt"
					_ = os.WriteFile(path, buf[:n], 0o644)
					log.Printf("rp6/diag: render loop stalled >1.5s — goroutine dump -> %s", path)
					dumped = true
				}
				<-done // wait for the loop to recover before pinging again
			}
		}
	}()
}

// startAudio attaches the recorder to the active backend. Hardware uses one
// strict P-6 capture stream plus a host output; the emulator shares its existing
// output callback, avoiding duplicate devices and feedback.
func (u *ui) startAudio() {
	u.audioStarted = true
	u.stopAudio()
	u.devMu.Lock()
	dev := u.dev
	u.devMu.Unlock()
	if target, ok := dev.(recorderController); ok {
		channels, rate := target.RecorderFormat()
		u.rec.SetFormat(channels, rate)
		m := audio.NewMeter(nil)
		target.SetRecorder(u.rec, func(samples []float32) {
			m.Process(samples)
			u.rec.Capture(samples)
		})
		u.audioMeter = m
		u.meterSrc = &audioSource{m: m}
		u.audioDevice = "emulator output"
		return
	}

	output, err := audio.OpenOutput()
	if err == nil {
		format := output.Format()
		u.rec.SetFormat(format.Channels, format.SampleRate)
		if err := output.Start(u.rec.Mix); err != nil {
			_ = output.Close()
			log.Printf("rp6: recorder output unavailable: %v", err)
		} else {
			u.recOutput = output
		}
	} else {
		log.Printf("rp6: recorder output unavailable: %v", err)
	}

	cap, err := audio.OpenCapture("P-6")
	if err != nil {
		log.Printf("rp6: audio capture unavailable: %v (using activity meter)", err)
		return
	}
	format := cap.Format()
	u.rec.SetFormat(format.Channels, format.SampleRate)
	m := audio.NewMeter(nil)
	if err := cap.Start(func(samples []float32) {
		m.Process(samples)
		u.rec.Capture(samples)
	}); err != nil {
		log.Printf("rp6: audio capture start failed: %v", err)
		_ = cap.Close()
		return
	}
	u.audioMeter = m
	u.audioCap = cap
	u.meterSrc = &audioSource{m: m}
	u.audioDevice = cap.Name()
	log.Printf("rp6: metering P-6 audio output (%s)", u.audioDevice)
	u.setStatus("metering P-6 audio output")
}

func (u *ui) stopAudio() {
	if u.audioCap != nil {
		_ = u.audioCap.Stop()
		_ = u.audioCap.Close()
		u.audioCap = nil
	}
	if u.recOutput != nil {
		_ = u.recOutput.Stop()
		_ = u.recOutput.Close()
		u.recOutput = nil
	}
	u.devMu.Lock()
	dev := u.dev
	u.devMu.Unlock()
	if target, ok := dev.(recorderController); ok {
		target.SetRecorder(nil, nil)
	}
	u.audioMeter = nil
	u.audioDevice = ""
	u.meterSrc = u.activity
}

// padSelected records a pad as the current selection (for the effects rack) and
// offers it to the sequencer: if a track is armed for assignment, the pad
// becomes that track's sample. Call from the UI thread.
func (u *ui) padSelected(id int) {
	u.selPad = id
	if u.fxRack != nil {
		u.fxRack.show(id)
	}
	if u.seqRack != nil {
		u.seqRack.PadSelected(id)
	}
}

// bumpMeter raises the activity meter (used when live audio is unavailable).
// Safe to call from any goroutine (roll fires come from background goroutines).
func (u *ui) bumpMeter(velocity uint8) {
	u.activity.bump(float64(velocity) / 127)
}

// onPadTrigger is invoked by the pad grid when a pad is tapped. It selects the
// pad (for the effects rack), routes the tap through the effects engine (which
// fires the note, possibly starting/stopping a roll), and reports status.
func (u *ui) onPadTrigger(bank, number int) {
	id := padID(bank, number)
	u.rec.TriggerRecord()
	u.padSelected(id)
	u.jamBroadcastPad(id, p6.DefaultVelocity) // share this live hit with jam peers (no-op in -tags nojam / web / mobile builds)

	u.fx.Tap(id) // fires via firePad (which bumps the meter); toggles roll if assigned

	if u.dev == nil {
		u.setStatus("P-6 not connected — press Reconnect")
		return
	}
	if u.fx.IsRolling(id) {
		u.setStatus(fmt.Sprintf("rolling %s @ %s",
			p6.PadLabel(bank, number), effects.Divisions[u.fx.State(id).RollDiv].Name))
		return
	}
	note, _ := p6.NoteFor(bank, number)
	u.setStatus(fmt.Sprintf("triggered %s  (note %d, ch %d)",
		p6.PadLabel(bank, number), note, u.dev.Config().SamplerChannel))
}

// firePad sends a pad's note over MIDI. It is the effects engine's Trigger
// callback and may run on a background (roll) goroutine, so it reads the device
// under devMu; p6.Device.Send itself is already concurrency-safe.
func (u *ui) firePad(id int) { u.firePadVel(id, p6.DefaultVelocity) }

// firePadVel fires a pad at a given velocity. Used by the effects engine (via
// firePad) and the step sequencer; both may call it from background goroutines.
func (u *ui) firePadVel(id int, velocity uint8) {
	bank, number := padBankNumber(id)
	u.devMu.Lock()
	dev := u.dev
	u.devMu.Unlock()
	if dev != nil {
		if err := dev.TriggerPadVelocity(bank, number, velocity); err != nil {
			u.deviceFailed(u.devGen.Load(), err) // device went away mid-fire
		}
	}
	u.bumpMeter(velocity) // every fire (tap, roll, or sequencer step) drives the meter
}

// playNote plays a chromatic note (keyboard rack) via the device's Auto channel
// (hardware pitches its selected pad; the emulator pitches the last pad played).
// Safe to call from the UI thread; reads the device under devMu.
func (u *ui) playNote(note, velocity uint8) {
	u.devMu.Lock()
	dev := u.dev
	u.devMu.Unlock()
	if dev != nil {
		if err := dev.PlayNote(note, velocity); err != nil {
			u.deviceFailed(u.devGen.Load(), err)
		}
	}
	u.bumpMeter(velocity)
}

var noteNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// noteName renders a MIDI note number as a pitch name (middle C = C4 = 60).
func noteName(n uint8) string {
	return fmt.Sprintf("%s%d", noteNames[n%12], int(n)/12-1)
}

// padBadges returns the effect icons for a pad (for the grid's badge row).
func (u *ui) padBadges(bank, number int) []image.Image {
	st := u.fx.State(padID(bank, number))
	var icons []image.Image
	for _, k := range st.Slots {
		if ic := k.Icon(); ic != nil {
			icons = append(icons, ic)
		}
	}
	return icons
}

// play starts the sequencer: MIDI Start plus a stream of clock pulses (required
// when the P-6 is set to SYnC = USB).
func (u *ui) play() {
	if u.clock == nil {
		u.setStatus("P-6 not connected — press Reconnect")
		u.playBtn.SetRunning(false)
		return
	}
	if err := u.clock.Start(); err != nil {
		u.playBtn.SetRunning(false)
		u.deviceFailed(u.devGen.Load(), err)
		return
	}
	u.playBtn.SetRunning(true)
	u.setStatus(fmt.Sprintf("▶ Play at %.0f BPM (needs P-6 MENU → SYnC = USB)", u.bpm))
}

// stop halts the clock pulses and sends MIDI Stop.
func (u *ui) stop() {
	if u.clock == nil {
		u.setStatus("P-6 not connected — press Reconnect")
		u.playBtn.SetRunning(false)
		return
	}
	if err := u.clock.Stop(); err != nil {
		u.playBtn.SetRunning(false)
		u.deviceFailed(u.devGen.Load(), err)
		return
	}
	u.playBtn.SetRunning(false)
	u.setStatus("■ Stop sent")
}

// onTempoChange is fired by the tempo stepper (buttons or keyboard).
func (u *ui) onTempoChange(bpm int) {
	u.bpm = float64(bpm)
	if u.clock != nil {
		u.clock.SetTempo(u.bpm)
	}
	u.fx.SetTempo(u.bpm)
	u.seq.SetTempo(u.bpm)
	u.rec.SetTempo(u.bpm)
	u.setStatus(fmt.Sprintf("tempo %d BPM", bpm))
}

// onPatternChange is fired by the pattern stepper: send a Program Change.
func (u *ui) onPatternChange(idx int) {
	if u.dev == nil {
		u.setStatus("P-6 not connected — press Reconnect")
		return
	}
	if err := u.dev.ProgramChange(uint8(idx)); err != nil {
		u.deviceFailed(u.devGen.Load(), err)
		return
	}
	u.setStatus(fmt.Sprintf("pattern %s (PC %d, needs rxPc = On)", patternName(idx), idx))
}

// patternName formats a 0-based pattern index as the P-6's "bank-slot" label,
// e.g. 0 -> "1-01", 63 -> "4-16".
func patternName(idx int) string {
	return fmt.Sprintf("%d-%02d", idx/16+1, idx%16+1)
}

// fxKnob builds a 0..127 rotary knob (LED-ring indicator, like TEMPO) that sends
// cc on the Auto channel as it turns. Used for the global Delay/Reverb amounts —
// continuous magnitudes, so the ring fills with the value.
func (u *ui) fxKnob(name string, cc uint8) *components.Knob {
	return components.NewKnob(components.KnobConfig{
		Label: name, Value: 0, Min: 0, Max: 127, Step: 1,
		Width:    132, // a touch narrower than TEMPO/PATTERN: four of them share the P-6 rack row
		Accent:   deviceHwAccent,
		OnChange: func(v int) { u.sendFX(name, cc, uint8(v)) },
	})
}

// sendFX transmits a global-FX control change on the Auto channel.
func (u *ui) sendFX(name string, cc, value uint8) {
	if u.dev == nil {
		u.setStatus("P-6 not connected — press Reconnect")
		return
	}
	if err := u.dev.AutoCC(cc, value); err != nil {
		u.deviceFailed(u.devGen.Load(), err)
		return
	}
	u.setStatus(fmt.Sprintf("%s = %d (CC%d)", name, value, cc))
}

// openDevice opens the endpoint the app talks to: the P-6 emulator (playing
// WAV samples from u.emuDir) when -emu/RP6_EMU_SAMPLES is set, otherwise the
// real hardware over USB MIDI.
func (u *ui) openDevice() (p6.Controller, error) {
	// A kit already loaded off the UI thread (setEmuSamples) — adopt it rather
	// than re-decoding inline, so the swap is instant.
	if u.preopened != nil {
		dev := u.preopened
		u.preopened = nil
		return dev, nil
	}
	if u.useEmu {
		if strings.TrimSpace(u.emuDir) == "" {
			return emu.OpenDefault(p6.DefaultConfig()) // built-in modular-hits kit
		}
		dev, err := emu.Open(u.emuDir, p6.DefaultConfig())
		if err != nil {
			log.Printf("rp6emu: emu.Open(%q) failed: %v", u.emuDir, err)
		} else {
			log.Printf("rp6emu: emu.Open(%q) ok", u.emuDir)
		}
		return dev, err
	}
	return p6.Open()
}

// connect (re)opens the P-6, replacing any existing connection. It is guarded
// against re-entrant/overlapping calls (rapid Reconnect taps) and reports
// friendly, cause-specific status for the common failure modes (busy port,
// permissions, nothing plugged in).
func (u *ui) connect() {
	if !u.connecting.CompareAndSwap(false, true) {
		return // a connect is already in flight
	}
	defer u.connecting.Store(false)

	u.fx.StopAll() // stop any rolls before swapping the device out
	if u.audioStarted {
		u.stopAudio()
	}
	if u.clock != nil {
		_ = u.clock.Stop()
		u.clock = nil
	}
	ct := time.Now()
	u.devMu.Lock()
	oldDev := u.dev
	u.dev = nil
	u.devMu.Unlock()
	if oldDev != nil {
		_ = oldDev.Close()
	}
	perfLap("connect: close old device", &ct)

	// New connection generation: retires stale background goroutines (Listen /
	// clock) from any prior device so their failures can't touch the UI.
	gen := u.devGen.Add(1)
	u.devLost.Store(false)

	if u.deviceBadge != nil {
		u.setDeviceState(components.DeviceSearching)
	}

	dev, err := u.openDevice()
	perfLap("connect: openDevice", &ct)
	if err != nil {
		u.setConnected(false)
		u.setStatus(u.connectErrorMessage(err))
		return
	}
	u.devMu.Lock()
	u.dev = dev
	u.devMu.Unlock()
	if target, ok := dev.(keyboardFXController); ok {
		target.SetKeyboardFX(u.keyboardFX)
		target.SetKeyboardFXEnabled(u.keyboardFXRack != nil && u.keyboardFXRack.Object().Visible())
	}
	u.clock = p6.NewClocker(dev, u.bpm)
	u.clock.SetOnError(func(err error) { u.deviceFailed(gen, err) })
	u.setConnected(true)
	u.setStatus("connected: " + dev.Path())
	// Reflect the P-6's own pad presses in the UI by default whenever it's
	// connected (the emulator has no MIDI input, so this turns off for it).
	u.setListenDefault()

	// React to hardware pad presses: read incoming MIDI and reflect hits in the
	// UI. Runs until this device is closed (reconnect/quit). A read error while
	// this is still the live connection means the device went away.
	go func() {
		err := dev.Listen(func(ev p6.Event) { u.onMIDIIn(dev, ev) })
		if err == nil || errors.Is(err, p6.ErrNoInput) {
			return
		}
		log.Printf("rp6: MIDI input stopped: %v", err)
		u.deviceFailed(gen, err)
	}()
}

// connectErrorMessage turns an openDevice error into a concise, actionable
// status line, matching on the p6 library's classified sentinels.
func (u *ui) connectErrorMessage(err error) string {
	switch {
	case u.useEmu:
		return "emulator failed to load samples: " + err.Error()
	case errors.Is(err, p6.ErrBusy):
		return "P-6 port busy — close other MIDI apps, then Reconnect"
	case errors.Is(err, p6.ErrPermission):
		return "P-6 permission denied — add your user to the 'audio' group"
	case errors.Is(err, p6.ErrNotFound):
		return "P-6 not found — check USB + power, then Reconnect"
	default:
		return "not connected: " + err.Error()
	}
}

// deviceFailed handles a background write/read failure from the live device
// (e.g. the P-6 unplugged mid-session). It marks the UI disconnected exactly
// once per connection (guarded by the connection generation + devLost), and is
// safe to call from any goroutine. A subsequent connect() clears the guard.
func (u *ui) deviceFailed(gen uint64, err error) {
	if gen != u.devGen.Load() { // a newer connection superseded this one
		return
	}
	if !u.devLost.CompareAndSwap(false, true) {
		return // already reported for this connection
	}
	log.Printf("rp6: device failed: %v", err)
	if u.clock != nil {
		_ = u.clock.Stop()
	}
	fyne.Do(func() {
		if gen != u.devGen.Load() {
			return
		}
		u.setConnected(false)
		u.playBtn.SetRunning(false)
		if !u.useEmu {
			// The P-6 vanished — fall back to the emulator so the app stays
			// usable (tap the badge to go back once it's replugged).
			u.setStatus("P-6 disconnected — switching to emulator")
			u.fallbackToEmu()
			return
		}
		u.setStatus("device disconnected — press Reconnect")
	})
}

// onMIDIIn reflects an incoming MIDI message from the hardware in the UI. It runs
// on the device's read goroutine, so UI work is marshalled onto the main thread.
// A pad press (Note On on the Sampler channel) selects and flashes that pad and
// nudges the meter — it does NOT re-trigger the note (the hardware already
// played it).
func (u *ui) onMIDIIn(dev p6.Controller, ev p6.Event) {
	if !u.listenMIDI.Load() {
		return
	}
	if ev.Type != p6.EventNoteOn || ev.Channel != dev.Config().SamplerChannel {
		return
	}
	bank, number, err := p6.PadForNote(ev.Data1)
	if err != nil {
		return // not a pad note
	}
	id := padID(bank, number)
	u.rec.TriggerRecord()
	page, row, col := u.gridPos(bank, number)
	u.bumpMeter(ev.Data2)           // meter reacts to hardware hits (any goroutine)
	u.jamBroadcastPad(id, ev.Data2) // share the hardware hit with jam peers (no-op in -tags nojam / web / mobile builds)
	fyne.Do(func() {
		u.grid.Highlight(page, row, col)
		u.padSelected(id)
		u.setStatus(fmt.Sprintf("hardware %s", p6.PadLabel(bank, number)))
	})
}

// startMIDIInput opens every supported external MIDI input controller that's
// plugged in (e.g. an Adafruit MacroPad *and* an Arturia keyboard together) and
// runs each: pad hits are fired into the active P-6/emulator and reflected in the
// UI, and transport control drives Play/Stop. It's idempotent — a controller
// already open (tracked by its node path) is left alone — so the attach poller
// can call it repeatedly to pick up newly-plugged devices. Best-effort: the
// absence of any controller is not an error. Call on the UI thread.
func (u *ui) startMIDIInput() {
	if u.midiIns == nil {
		u.midiIns = map[string]midiin.Device{}
	}
	found := midiin.Present()
	if len(found) == 0 {
		if len(u.midiIns) == 0 && !u.midiInMissLogged {
			log.Printf("rp6: no MIDI input controller: %v", midiin.ErrNotFound)
			u.midiInMissLogged = true // don't spam the 2s retry poller
		}
		return
	}
	for _, f := range found {
		if _, running := u.midiIns[f.Path]; running {
			continue // already open — don't reopen the exclusive node
		}
		dev, err := f.Open()
		if err != nil {
			log.Printf("rp6: opening MIDI input %s (%s): %v", f.Name, f.Path, err)
			continue
		}
		u.midiInMissLogged = false
		u.midiIns[f.Path] = dev
		log.Printf("rp6: MIDI input controller: %s (%s)", dev.Name(), dev.Path())
		u.setStatus(fmt.Sprintf("%s connected (%s)", dev.Name(), dev.Path()))
		// A controller is an input source even on the emulator, so default to
		// listening (the eye toggle) now that one is attached; the user can still
		// switch it off to stop the pads reacting.
		u.setListenDefault()

		path := f.Path
		go func() {
			if err := dev.Run(u.midiInHandlers()); err != nil {
				log.Printf("rp6: MIDI input (%s) stopped: %v", dev.Name(), err)
			}
			_ = dev.Close() // release the node so a replug/swap can reopen cleanly
			// Drop the handle on the UI thread (serialised with close() and the
			// attach poller) so a reconnect can pick it up again.
			fyne.Do(func() {
				if u.midiIns[path] == dev {
					delete(u.midiIns, path)
				}
			})
		}()
	}
}

// midiInHandlers builds the callback surface external controllers drive. The same
// handlers are shared by every open controller (they act on shared app state).
func (u *ui) midiInHandlers() midiin.Handlers {
	return midiin.Handlers{
		TriggerPad: u.fireExternalPad,
		PlayNote:   u.playExternalNote,
		Transport: func(playing bool) {
			fyne.Do(func() {
				u.playBtn.SetRunning(playing)
				if playing {
					u.play()
				} else {
					u.stop()
				}
			})
		},
	}
}

// fireExternalPad handles a pad trigger from an external controller: it plays
// the pad (through the P-6 or emulator, same as a screen tap) and reflects the
// hit in the UI (select + flash). Unlike onMIDIIn — which mirrors the P-6's own
// pad presses without re-triggering — the controller produces no sound itself,
// so here we DO fire the note. Runs on the controller's read goroutine.
//
// The MIDI-input "listen" toggle (the eye) gates this too: switch it off and the
// pads stop reacting to the external controller (e.g. when you'd rather play the
// controller into something else without RP6 chiming in).
func (u *ui) fireExternalPad(id int, velocity uint8) {
	if !u.listenMIDI.Load() {
		return
	}
	u.rec.TriggerRecord()
	u.firePadVel(id, velocity)      // sound (also bumps the meter); concurrency-safe
	u.jamBroadcastPad(id, velocity) // share this live hit with jam peers (no-op in -tags nojam / web / mobile builds)
	bank, number := padBankNumber(id)
	page, row, col := u.gridPos(bank, number)
	fyne.Do(func() {
		u.grid.Highlight(page, row, col)
		u.padSelected(id)
		u.setStatus(fmt.Sprintf("pad %s", p6.PadLabel(bank, number)))
	})
}

// playExternalNote handles a note from an external melodic keyboard (e.g. an
// Arturia KeyStep/MicroLab): it plays the note through rp6's keyboard path (the
// selected sample, pitched — same as the on-screen keys) and reflects it on the
// on-screen keyboard, revealing that rack the first time so the controller
// visibly drives it. Runs on the controller's read goroutine. Gated by the
// listen (eye) toggle, like fireExternalPad.
func (u *ui) playExternalNote(note, velocity uint8) {
	if !u.listenMIDI.Load() {
		return
	}
	u.playNote(note, velocity) // sound (Auto channel on hardware; pitched on the emulator); concurrency-safe
	fyne.Do(func() {
		if u.keyboardRack == nil {
			return
		}
		if !u.keyboardAutoShown && !u.keyboardRack.Object().Visible() {
			u.setVisible(u.keyboardRack.Object(), u.keysBtn, true)
			u.keyboardAutoShown = true // reveal once; don't fight a later manual hide
			u.relayout()
		}
		u.keyboardRack.reflectNote(note)
		u.setStatus(fmt.Sprintf("♪ %s", noteName(note)))
	})
}

// gridPos maps a pad (0-based bank, 1-based number) to its (page,row,col) in the
// current pad-grid layout (paged A-D/E-H, two-bank tabs, or the dense single
// page).
func (u *ui) gridPos(bank, number int) (page, row, col int) {
	bpp := banksForLayout(u.padLayout)
	return bank / bpp, bank % bpp, number - 1
}

// setLayout switches the pad grid to a new layout (paged / two-bank / dense),
// rebuilding the grid and swapping it into the holder.
func (u *ui) setLayout(l padLayout) { u.applyPadLayout(l, true) }

// applyPadLayout switches the pad grid to layout l, rebuilding the grid and
// swapping it into the stable holder. persist writes it to the global preference
// (true for a user cycle-button change; false for a variant default applied via
// the `pads(layout: …)` layout property, which shouldn't clobber the pref).
func (u *ui) applyPadLayout(l padLayout, persist bool) {
	if persist {
		rememberPadLayout(l) // global preference — survives a restart
	}
	if u.padLayout == l && u.grid != nil {
		return // already showing this layout — no rebuild needed
	}
	u.padLayout = l
	u.grid = newPadGrid(u.padLayout, u.onPadTrigger, u.padBadges)
	u.padGridArea.Objects = []fyne.CanvasObject{u.grid.Object()}
	u.padGridArea.Refresh()
	if u.layoutBtn != nil {
		u.layoutBtn.SetState(int(u.padLayout))
	}
	// Re-apply the selection highlight to the rebuilt grid.
	if u.selPad >= 0 {
		bank, number := padBankNumber(u.selPad)
		u.grid.Highlight(u.gridPos(bank, number))
	}
}

// parsePadLayout maps a `pads(layout: …)` property value to a padLayout.
func parsePadLayout(s string) (padLayout, bool) {
	switch s {
	case "paged":
		return layoutPaged, true
	case "twobank":
		return layoutTwoBank, true
	case "dense":
		return layoutDense, true
	default:
		return 0, false
	}
}

// toggleMIDIListen enables/disables reacting to MIDI input — the P-6's own pad
// presses (reflected in the UI) and external controllers (which trigger pads).
func (u *ui) toggleMIDIListen() {
	on := !u.listenMIDI.Load()
	u.listenMIDI.Store(on)
	u.updateMIDIInBtn()
	if on {
		u.setStatus("listening to MIDI input")
	} else {
		u.setStatus("ignoring MIDI input")
	}
}

// updateMIDIInBtn reflects the listen state on the tool-column toggle: a lit
// open eye when listening, a faded struck-through eye when ignoring input.
func (u *ui) updateMIDIInBtn() {
	on := u.listenMIDI.Load()
	if on {
		u.midiInBtn.SetIcon(theme.VisibilityIcon())
	} else {
		u.midiInBtn.SetIcon(theme.VisibilityOffIcon())
	}
	u.midiInBtn.SetOn(on)
}

// setConnected updates the status LED and device badge: green/online when
// connected, red/offline otherwise.
func (u *ui) setConnected(ok bool) {
	col := ledRed
	if ok {
		col = ledGreen
	}
	if u.statusLED != nil {
		u.statusLED.SetColor(col)
	}
	if u.p6LED != nil {
		u.p6LED.SetColor(col)
	}
	if ok {
		u.setDeviceState(components.DeviceOnline)
	} else {
		u.setDeviceState(components.DeviceOffline)
	}
}

// setDeviceState records the connection state and reflects it on the device
// badge. The state is stored on the ui so it survives a pad-rack rebuild (the
// badge lives in the pad rack and is recreated when it floats out / docks back).
func (u *ui) setDeviceState(s components.DeviceState) {
	u.deviceState = s
	if u.deviceBadge != nil {
		u.deviceBadge.SetState(s)
	}
}

func (u *ui) setStatus(msg string) {
	if u.status != nil {
		u.status.SetText(msg)
	}
}

func (u *ui) close() {
	u.stopMeter() // stop UI animators before the run loop tears down
	u.stopRelayoutWatch()
	u.stopDeviceWatch()
	u.stopJam()
	u.fx.StopAll()
	u.seq.Stop()
	u.rec.StopRecordingImmediate()
	u.rec.StopAllImmediate()
	u.seqRack.setSlotPending(false) // stop the SEQ knob flash goroutine
	u.autosaveSeq()
	u.autosaveRecorder()
	if u.store != nil {
		_ = u.store.Close()
	}
	u.stopAudio()
	if u.statusLED != nil {
		u.statusLED.StopPulse()
	}
	if u.clock != nil {
		_ = u.clock.Stop()
	}
	// Retire the connection generation so the Listen/clock goroutines closing
	// below don't marshal a "disconnected" update onto the tearing-down loop.
	u.devGen.Add(1)
	u.devMu.Lock()
	dev := u.dev
	u.dev = nil
	u.devMu.Unlock()
	if dev != nil {
		_ = dev.Close()
	}
	for path, dev := range u.midiIns {
		_ = dev.Close() // unblocks each input Run goroutine
		delete(u.midiIns, path)
	}
	if u.padWin != nil {
		w := u.padWin
		u.padWin = nil
		w.Close()
	}
}

func main() {
	// `rp6 pak <create|install|list> …` runs an offline sample-pak command and
	// exits (desktop only; no-op stub elsewhere) before the GUI starts.
	maybeRunPakCLI()

	// -emu runs the software P-6 emulator against a directory of WAV samples
	// laid out like the hardware's 48 slots (A1..H6), instead of connecting to
	// real hardware — handy when the P-6 isn't around. Defaults to the
	// RP6_EMU_SAMPLES environment variable.
	emuDir := flag.String("emu", os.Getenv("RP6_EMU_SAMPLES"),
		"run the P-6 emulator using WAV samples from this directory (A1..H6) instead of the hardware")
	// -pak installs a .rp6sp sample pak into the rp6 samples directory and
	// launches the emulator on it (desktop only).
	pakPath := flag.String("pak", "", "install this .rp6sp sample pak and run the emulator on it")
	// -timing controls the emulator's voice-start timing (no effect on real
	// hardware). "sample" (default) starts each triggered sample at its exact
	// sub-buffer position, so near-simultaneous pad hits (e.g. a chord from a
	// MacroPad) keep their true relative timing; "buffer" aligns starts to the
	// audio-buffer boundary (coarser, can flam near-simultaneous hits).
	timing := flag.String("timing", "sample", "emulator voice timing: sample|buffer")
	flag.Parse()

	emu.SampleAccurate = *timing != "buffer"

	a := app.NewWithID(appID)
	a.Settings().SetTheme(uitheme.Amber{})
	a.SetIcon(appIcon())
	w := a.NewWindow("RP6 — P-6 Pad Controller")
	w.SetIcon(appIcon())

	u := newUI()
	u.emuDir = *emuDir
	// Start on the emulator when samples were given, and always on mobile (no
	// P-6 USB access there). On the web we still try the P-6 first (over Web
	// MIDI); when it isn't reachable the connect below falls back to the
	// emulator, and the device watcher connects to it if it appears.
	u.useEmu = *emuDir != "" || onMobile
	// -pak installs a sample pak and points the emulator at it (overriding -emu).
	if *pakPath != "" {
		u.installAndSelectPak(*pakPath)
	}
	// If the CLI didn't pin a pak, reuse the last one picked at runtime so the
	// emulator reopens that pak instead of the built-in kit. Backend selection
	// (useEmu) is deliberately left untouched — see vxrv.
	if strings.TrimSpace(u.emuDir) == "" {
		u.emuDir = u.savedEmuDir()
	}
	w.Resize(fyne.NewSize(windowedWidth, windowedHeight))
	// Restore the remembered console-layout choice. On desktop the windowed size
	// is a single fixed, non-resizable size (see resolutions.txt); the lock stays
	// on the whole time (glfw full-screen ignores the size limits, and keeping
	// them present helps the compositor restore the windowed size on exit — see
	// setConsole). Mobile picks phone-or-tablet from the device size (see the
	// `phone`/`tablet` variants), so there's no console choice or fixed size.
	startConsole := false
	if on, saved := loadConsolePref(); saved && !onMobile {
		startConsole = on
	}
	if !onMobile {
		u.fullScreen = startConsole
		if startConsole {
			// Start full screen unlocked (like a normal app), so leaving the
			// console later restores the windowed size and re-locks it — see
			// setConsole / onCanvasResize.
			u.relockWindowed = true
			w.SetFullScreen(true)
		} else {
			w.SetFixedSize(true) // windowed is a single fixed, non-resizable size
		}
	}
	u.build(w)
	u.connect()
	if !u.useEmu && u.dev == nil {
		// No P-6 at launch — fall back to the emulator (which re-scopes the
		// store + loads its sequence itself), so skip the P-6 store setup here.
		u.fallbackToEmu()
	} else {
		u.openStore()
		u.loadInitialSequence()
	}
	u.startMeter()
	u.relayoutWatch() // marshal resize-driven relayouts onto the UI loop (main() only)
	// USB-audio VU capture needs raw device access (and the mic permission on
	// mobile) — desktop only. External MIDI controllers (MacroPad) and the P-6
	// device watcher use ALSA (desktop) or Web MIDI (web); Android reaches USB
	// MIDI a different way (see startAndroidMIDI); iOS has no MIDI path.
	u.loadRecorder()
	u.startAudio()
	if !onMobile {
		u.startMIDIInput()
		u.startDeviceWatch()
		u.startMIDIInputWatch() // re-attach a controller on hot-plug/swap
	}
	u.startAndroidMIDI() // no-op except on Android
	u.startJam()         // join a shared jam session if RP6_JAM_CODE is set (no-op in -tags nojam / web / mobile builds)
	u.statusLED.StartPulse()
	u.startDiagnostics()

	w.SetCloseIntercept(func() {
		u.close()
		w.Close()
	})

	// Ctrl+Q quits the app.
	w.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyQ,
		Modifier: fyne.KeyModifierControl,
	}, func(fyne.Shortcut) {
		u.close()
		a.Quit()
	})

	w.ShowAndRun()
}
