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
	"github.com/mono4loop/rp6/internal/effects"
	"github.com/mono4loop/rp6/internal/emu"
	"github.com/mono4loop/rp6/internal/midiin"
	_ "github.com/mono4loop/rp6/internal/midiin/macropad" // register the MacroPad input driver
	"github.com/mono4loop/rp6/internal/sequencer"
	"github.com/mono4loop/rp6/internal/store"
	"github.com/mono4loop/rp6/internal/ui/components"
	uitheme "github.com/mono4loop/rp6/internal/ui/theme"
	"github.com/mono4loop/rp6/p6"
)

// seqMaxBars is the maximum bar length a sequencer track can have.
const seqMaxBars = 4

type ui struct {
	dev   p6.Controller
	devMu sync.Mutex // guards dev for the off-main roll/fire path

	// midiIn is an optional external MIDI input controller (e.g. an Adafruit
	// MacroPad) that drives the pads/transport host-side; nil when none is found.
	midiIn midiin.Device
	// midiInMissLogged suppresses repeating the "no controller" log line on the
	// Android attach poller (which retries every 2s); reset once one connects.
	midiInMissLogged bool

	// emuDir, when set, holds WAV samples (A1..H6) the P-6 emulator can play.
	// It's the pool the emulator draws from; whether the emulator is *active*
	// is useEmu (togglable at runtime via the device badge's Ctrl+click).
	emuDir string
	// useEmu selects the active backend: the emulator (needs emuDir) when true,
	// the P-6 hardware when false. Initialized from the -emu flag.
	useEmu bool

	bpm float64 // clock tempo for transport

	clock   *p6.Clocker
	fx      *effects.Engine
	seq     *sequencer.Engine
	store   *store.Store
	seqSlot int
	// pendingSlot is a slot change queued while the sequencer plays, applied at
	// the next bar boundary (0 = none).
	pendingSlot int
	selPad      int // padID of the selected pad, or -1

	activity    *activitySource
	meterSrc    meterSource
	meterStop   chan struct{} // closed to stop the meter animator on shutdown
	audioMeter  *audio.Meter
	audioDevice string
	win         fyne.Window

	grid        *components.PadGrid
	padRackObj  fyne.CanvasObject // pad grid + tool column, wrapped as a toggleable rack
	padGridArea *fyne.Container   // holds the current grid object (swapped on density toggle)
	padFloatBtn *components.RackToggle
	midiInBtn   *components.RackToggle
	layoutBtn   *components.RackCycle
	padLayout   padLayout   // how the 48 pads are paged across the grid
	listenMIDI  atomic.Bool // reflect hardware pad presses in the UI
	padWin      fyne.Window // non-nil while the pad rack floats in its own window
	padFloating bool
	playBtn     *components.TransportButton
	tempo       *components.Knob
	patternStep *components.Knob
	fxRack      *effectsRack
	seqRack     *sequencerRack
	meter       *components.LevelMeter
	meterArea   fyne.CanvasObject
	dlyRevObj   fyne.CanvasObject // the Delay/Reverb rack panel (toggleable)

	transportRack fyne.CanvasObject
	statusBar     fyne.CanvasObject
	seqSide       bool // sequencer docked as a right-hand column

	// bottom-bar visibility toggles (backlit rack labels)
	padBtn    *components.RackToggle
	dlyRevBtn *components.RackToggle
	fxBtn     *components.RackToggle
	seqBtn    *components.RackToggle
	meterBtn  *components.RackToggle

	statusLED   *components.LED
	root        *fyne.Container
	status      *widget.Label
	deviceBadge *components.DeviceBadge // top rack: backlit device nameplate

	// connecting guards connect() against re-entrant / overlapping calls.
	// devGen tags the live connection so a background failure (mid-session
	// unplug) is reported once and stale goroutines from an old connection
	// can't clobber the current UI state.
	connecting atomic.Bool
	devGen     atomic.Uint64
	devLost    atomic.Bool

	// emuFallback is true while we're on the emulator only because a P-6 was
	// absent/lost (auto-fallback). A background watcher polls for a P-6 and
	// reconnects to it automatically while this is set — so no manual Reconnect
	// is needed. It's false when the emulator was chosen deliberately (-emu flag
	// or Ctrl+click), which suppresses the auto-switch.
	emuFallback atomic.Bool
	watchStop   chan struct{} // closed to stop the device watcher on shutdown
}

func newUI() *ui {
	u := &ui{bpm: 120, selPad: -1, padLayout: loadPadLayout()}
	u.activity = &activitySource{}
	u.meterSrc = u.activity
	u.fx = effects.New(u.firePad)
	u.seq = sequencer.New(8, seqMaxBars, u.firePadVel)
	return u
}

func (u *ui) build(w fyne.Window) {
	// Pad area (P-6-specific grid + paging + selection), wrapped as a rack with
	// a slim left column of tool buttons (first one floats the rack to its own
	// window and docks it back).
	u.buildPadRack()
	u.fxRack = newEffectsRack(u.fx, func() { u.grid.RefreshBadges() })
	u.seqRack = newSequencerRack(u.seq,
		func() {
			if u.root != nil {
				u.root.Refresh()
			}
		}, u.onSeqDock, numSeqSlots, u.selectSlot, u.copyToSlot, u.deleteSlot, u.saveSeq)
	u.seqRack.onStop = u.applyPendingSlot
	u.seq.OnStep = func(tick int) {
		fyne.Do(func() {
			u.maybeApplyPendingAt(tick)
			u.seqRack.setPlayhead(tick)
		})
	}

	// Transport rack: a single Play/Stop toggle key, tempo, pattern.
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

	transport := container.NewHBox(u.playBtn)
	name, tag, accent := u.deviceIdentity()
	u.deviceBadge = components.NewDeviceBadge(name, tag, accent)
	u.deviceBadge.OnSettings(u.showDeviceSettings)
	u.deviceBadge.OnToggle(u.toggleBackend)
	toolbar := container.NewHBox(transport, widget.NewSeparator(),
		u.tempo.Object(), widget.NewSeparator(), u.patternStep.Object(),
		layout.NewSpacer(), u.deviceBadge)

	fxRow := container.NewGridWithColumns(4,
		u.fxSlider("Delay Time", p6.CCDelayTime),
		u.fxSlider("Delay Level", p6.CCDelayLevel),
		u.fxSlider("Reverb Time", p6.CCReverbTime),
		u.fxSlider("Reverb Level", p6.CCReverbLevel),
	)
	u.dlyRevObj = components.NewRackPanel(fxRow)

	u.transportRack = components.NewRackPanel(toolbar)

	// Vertical master meter on the right, framed as a rack panel (toggleable).
	// A short "VU" cap keeps the rack column narrow.
	u.meter = components.NewLevelMeter()
	u.meterArea = components.NewRackPanel(container.NewBorder(
		widget.NewLabelWithStyle("VU", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		nil, nil, nil, u.meter))

	// Visibility toggles on the bottom bar: backlit rack labels (lit = shown,
	// greyed = hidden), tinted in the P-6 amber accent.
	acc := deviceHwAccent
	u.padBtn = components.NewRackToggle("PADS", acc, u.togglePads)
	u.dlyRevBtn = components.NewRackToggle("DLY/REV", acc, func() { u.toggleVisible(u.dlyRevObj, u.dlyRevBtn) })
	u.fxBtn = components.NewRackToggle("FX", acc, func() { u.toggleVisible(u.fxRack.Object(), u.fxBtn) })
	u.seqBtn = components.NewRackToggle("SEQ", acc, u.toggleSeqView)
	u.meterBtn = components.NewRackToggle("VU", acc, func() { u.toggleVisible(u.meterArea, u.meterBtn) })
	toggleObjs := []fyne.CanvasObject{u.padBtn, u.dlyRevBtn, u.fxBtn, u.seqBtn, u.meterBtn}
	toggleObjs = append(toggleObjs, u.jamToggles()...) // JAM button (absent in -tags nojam / web / mobile builds)
	toggles := container.NewHBox(toggleObjs...)

	u.status = widget.NewLabel("")
	info := widget.NewButtonWithIcon("", theme.InfoIcon(), u.showInfo)
	info.Importance = widget.LowImportance
	u.statusLED = components.NewLED(ledRed)
	u.statusBar = components.NewRackPanel(container.NewBorder(nil, nil,
		u.statusLED,
		container.NewHBox(toggles, widget.NewSeparator(), info),
		u.status))

	u.win = w

	// Ctrl+Shift+P/D/F/S/M toggle the Pads, Delay-Reverb, FX, Sequencer, Meter racks.
	u.addRackShortcut(w, fyne.KeyP, u.togglePads)
	u.addRackShortcut(w, fyne.KeyD, func() { u.toggleVisible(u.dlyRevObj, u.dlyRevBtn) })
	u.addRackShortcut(w, fyne.KeyF, func() { u.toggleVisible(u.fxRack.Object(), u.fxBtn) })
	u.addRackShortcut(w, fyne.KeyS, u.toggleSeqView)
	u.addRackShortcut(w, fyne.KeyM, func() { u.toggleVisible(u.meterArea, u.meterBtn) })

	// Default visibility: Pads + Sequencer + Meter on; Delay/Reverb, Effects off.
	// The sequencer shows above the pads (undocked) by default.
	u.setVisible(u.padRackObj, u.padBtn, true)
	u.setVisible(u.dlyRevObj, u.dlyRevBtn, false)
	u.setVisible(u.fxRack.Object(), u.fxBtn, false)
	u.setVisible(u.seqRack.Object(), u.seqBtn, true)
	u.setVisible(u.meterArea, u.meterBtn, true)

	u.relayout()
}

// relayout (re)assembles the window content, honoring which racks are visible
// and the sequencer's docked (right-hand column) mode.
func (u *ui) relayout() {
	seqDocked := u.seqSide && u.seqRack.Object().Visible()

	topObjs := []fyne.CanvasObject{u.transportRack, u.dlyRevObj, u.fxRack.Object()}
	if !seqDocked {
		topObjs = append(topObjs, u.seqRack.Object())
	}
	top := container.NewVBox(topObjs...)

	padsVisible := u.padRackObj.Visible() && !u.padFloating
	var center fyne.CanvasObject
	switch {
	case padsVisible && seqDocked:
		// Sequencer docked as a right-hand column; both sides adapt (the pads
		// shrink) to fit — no scrollbars.
		sp := container.NewHSplit(u.padRackObj, u.seqRack.Object())
		sp.SetOffset(0.5)
		center = sp
	case padsVisible:
		center = u.padRackObj
	case seqDocked:
		center = u.seqRack.Object()
	}

	var centerObjs []fyne.CanvasObject
	if center != nil {
		centerObjs = []fyne.CanvasObject{center}
	}

	u.root = container.NewBorder(top, u.statusBar, nil, u.meterArea, centerObjs...)
	u.win.SetContent(u.root)
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

	// Slim left tool strip: backlit icon toggles (lit = active, faded = off).
	tool := deviceHwAccent
	u.padFloatBtn = components.NewRackToggleIcon(theme.ViewRestoreIcon(), tool, u.togglePadFloat)
	u.padFloatBtn.SetOn(u.padFloating)
	u.midiInBtn = components.NewRackToggleIcon(theme.VisibilityIcon(), tool, u.toggleMIDIListen)
	u.updateMIDIInBtn()
	// Layout selector: a 3-state cycle (paged A–D/E–H → two-bank A–B…G–H → dense
	// all-8) whose icon shows the current density.
	u.layoutBtn = components.NewRackCycle(layoutIcons, tool, func(s int) { u.setLayout(padLayout(s)) })
	u.layoutBtn.SetState(int(u.padLayout))

	padTools := container.NewVBox(u.padFloatBtn, u.midiInBtn, u.layoutBtn)
	padBody := container.NewBorder(nil, nil, padTools, nil, u.padGridArea)
	u.padRackObj = components.NewRackPanel(padBody)

	// Re-apply the selection highlight (no flash) to the fresh grid.
	if u.selPad >= 0 {
		bank, number := padBankNumber(u.selPad)
		u.grid.Select(u.gridPos(bank, number))
	}
}

// togglePads shows/hides the pad grid rack (it occupies the center, so relayout).
func (u *ui) togglePads() {
	u.setVisible(u.padRackObj, u.padBtn, !u.padRackObj.Visible())
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
	u.setVisible(u.seqRack.Object(), u.seqBtn, !u.seqRack.Object().Visible())
	u.relayout()
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
		v := app.Preferences().IntWithFallback(prefKeyPadLayout, int(layoutTwoBank))
		if v >= 0 && v < numLayouts {
			return padLayout(v)
		}
	}
	return layoutTwoBank
}

// rememberPadLayout persists the pad layout so the next launch restores it.
func rememberPadLayout(l padLayout) {
	if app := fyne.CurrentApp(); app != nil {
		app.Preferences().SetInt(prefKeyPadLayout, int(l))
	}
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
		widget.NewLabel("Layout: flat A1.wav..H6.wav, A/1.wav.., or BANK_A/PAD_1/*.wav"),
		choose,
	)
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

// setListenDefault turns hardware-input reflection on when the active backend is
// the P-6 (so its pad presses show in the UI without having to tap the eye
// toggle) and off for the emulator, which has no MIDI input. Called from connect
// on every (re)connection, so a connected P-6 always listens by default.
func (u *ui) setListenDefault() {
	u.listenMIDI.Store(!u.useEmu)
	u.updateMIDIInBtn()
}

// setEmuSamples points the emulator at a new samples directory and reconnects
// (also switching to the emulator backend if not already on it). Each directory
// is its own persistence profile, so this re-scopes and reloads sequences.
func (u *ui) setEmuSamples(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	log.Printf("rp6emu: setEmuSamples dir=%q", dir)
	u.emuDir = dir
	u.useEmu = true
	u.emuFallback.Store(false) // picking a kit is a deliberate emulator choice
	u.rememberEmuDir(dir)      // survive a restart (see vxrv)
	u.reconnectProfile()
	u.setStatus("emulator samples: " + dir)
}

// reconnectProfile stops transport, persists the outgoing profile's sequence,
// repaints the badge for the current backend, reconnects, re-scopes persistence
// to the (possibly changed) profile and reloads its sequence. The store is
// re-opened from storeProfile(), so callers only set useEmu/emuDir beforehand.
func (u *ui) reconnectProfile() {
	u.seq.Stop()
	u.playBtn.SetRunning(false)
	u.autosaveSeq() // persist under the still-open outgoing profile
	if u.store != nil {
		_ = u.store.Close()
		u.store = nil
	}

	name, tag, accent := u.deviceIdentity()
	u.deviceBadge.SetAccent(accent)
	u.deviceBadge.SetName(name, tag)

	u.connect()             // open the new backend (also updates badge state)
	u.openStore()           // re-scope persistence to the new profile
	u.loadInitialSequence() // load that profile's last sequence
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

// If capture is unavailable (no "portaudio" build tag, no device, or an error),
// it silently leaves the activity-based meter in place.
func (u *ui) startAudio() {
	cap, err := audio.OpenCapture("P-6")
	if err != nil {
		log.Printf("rp6: audio capture unavailable: %v (using activity meter)", err)
		return
	}
	m := audio.NewMeter(cap)
	if err := m.Start(); err != nil {
		log.Printf("rp6: audio meter start failed: %v", err)
		_ = cap.Close()
		return
	}
	u.audioMeter = m
	u.meterSrc = &audioSource{m: m}
	u.audioDevice = m.Name()
	log.Printf("rp6: metering P-6 audio output (%s)", u.audioDevice)
	u.setStatus("metering P-6 audio output")
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

// fxSlider builds a labeled 0-127 slider with a red 7-segment readout that
// sends cc on the Auto channel as it moves.
func (u *ui) fxSlider(name string, cc uint8) fyne.CanvasObject {
	seg := components.NewSevenSeg(3)
	s := widget.NewSlider(0, 127)
	s.Step = 1
	s.OnChanged = func(v float64) {
		seg.SetValue(int(v))
		u.sendFX(name, cc, uint8(v))
	}
	label := widget.NewLabel(name)
	// Label on top; slider fills the width; 7-seg readout on the right.
	return container.NewBorder(label, nil, nil, seg, s)
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
	if u.clock != nil {
		_ = u.clock.Stop()
		u.clock = nil
	}
	u.devMu.Lock()
	if u.dev != nil {
		_ = u.dev.Close()
		u.dev = nil
	}
	u.devMu.Unlock()

	// New connection generation: retires stale background goroutines (Listen /
	// clock) from any prior device so their failures can't touch the UI.
	gen := u.devGen.Add(1)
	u.devLost.Store(false)

	if u.deviceBadge != nil {
		u.deviceBadge.SetState(components.DeviceSearching)
	}

	dev, err := u.openDevice()
	if err != nil {
		u.setConnected(false)
		u.setStatus(u.connectErrorMessage(err))
		return
	}
	u.devMu.Lock()
	u.dev = dev
	u.devMu.Unlock()
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
	page, row, col := u.gridPos(bank, number)
	u.bumpMeter(ev.Data2)           // meter reacts to hardware hits (any goroutine)
	u.jamBroadcastPad(id, ev.Data2) // share the hardware hit with jam peers (no-op in -tags nojam / web / mobile builds)
	fyne.Do(func() {
		u.grid.Highlight(page, row, col)
		u.padSelected(id)
		u.setStatus(fmt.Sprintf("hardware %s", p6.PadLabel(bank, number)))
	})
}

// startMIDIInput detects a supported external MIDI input controller (e.g. an
// Adafruit MacroPad) and, if found, runs it: pad hits are fired into the active
// P-6/emulator and reflected in the UI, and its transport control drives Play/
// Stop. It is best-effort — absence of a controller is not an error.
func (u *ui) startMIDIInput() {
	dev, err := midiin.Detect()
	if err != nil {
		if !u.midiInMissLogged {
			log.Printf("rp6: no MIDI input controller: %v", err)
			u.midiInMissLogged = true // don't spam the Android 2s retry poller
		}
		return
	}
	u.midiInMissLogged = false
	u.midiIn = dev
	log.Printf("rp6: MIDI input controller: %s (%s)", dev.Name(), dev.Path())
	u.setStatus(fmt.Sprintf("%s connected (%s)", dev.Name(), dev.Path()))

	h := midiin.Handlers{
		TriggerPad: u.fireExternalPad,
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
	go func() {
		if err := dev.Run(h); err != nil {
			log.Printf("rp6: MIDI input (%s) stopped: %v", dev.Name(), err)
		}
		// Clear the handle on the UI thread (serialised with close() and the
		// Android re-attach poller) so a reconnect can pick a controller up again.
		fyne.Do(func() {
			if u.midiIn == dev {
				u.midiIn = nil
			}
		})
	}()
}

// fireExternalPad handles a pad trigger from an external controller: it plays
// the pad (through the P-6 or emulator, same as a screen tap) and reflects the
// hit in the UI (select + flash). Unlike onMIDIIn — which mirrors the P-6's own
// pad presses without re-triggering — the controller produces no sound itself,
// so here we DO fire the note. Runs on the controller's read goroutine.
func (u *ui) fireExternalPad(id int, velocity uint8) {
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

// gridPos maps a pad (0-based bank, 1-based number) to its (page,row,col) in the
// current pad-grid layout (paged A-D/E-H, two-bank tabs, or the dense single
// page).
func (u *ui) gridPos(bank, number int) (page, row, col int) {
	bpp := banksForLayout(u.padLayout)
	return bank / bpp, bank % bpp, number - 1
}

// setLayout switches the pad grid to a new layout (paged / two-bank / dense),
// rebuilding the grid and swapping it into the holder.
func (u *ui) setLayout(l padLayout) {
	u.padLayout = l
	rememberPadLayout(l) // global preference — survives a restart
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

// toggleMIDIListen enables/disables reflecting hardware pad presses in the UI.
func (u *ui) toggleMIDIListen() {
	on := !u.listenMIDI.Load()
	u.listenMIDI.Store(on)
	u.updateMIDIInBtn()
	if on {
		u.setStatus("listening to P-6 input")
	} else {
		u.setStatus("ignoring P-6 input")
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
	if u.statusLED != nil {
		if ok {
			u.statusLED.SetColor(ledGreen)
		} else {
			u.statusLED.SetColor(ledRed)
		}
	}
	if u.deviceBadge != nil {
		if ok {
			u.deviceBadge.SetState(components.DeviceOnline)
		} else {
			u.deviceBadge.SetState(components.DeviceOffline)
		}
	}
}

func (u *ui) setStatus(msg string) {
	if u.status != nil {
		u.status.SetText(msg)
	}
}

func (u *ui) close() {
	u.stopMeter() // stop UI animators before the run loop tears down
	u.stopDeviceWatch()
	u.stopJam()
	u.fx.StopAll()
	u.seq.Stop()
	u.seqRack.setSlotPending(false) // stop the SEQ knob flash goroutine
	u.autosaveSeq()
	if u.store != nil {
		_ = u.store.Close()
	}
	if u.audioMeter != nil {
		_ = u.audioMeter.Stop()
		_ = u.audioMeter.Close()
	}
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
	if u.dev != nil {
		_ = u.dev.Close()
	}
	u.devMu.Unlock()
	if u.midiIn != nil {
		_ = u.midiIn.Close() // unblocks the input Run goroutine
		u.midiIn = nil
	}
	if u.padWin != nil {
		w := u.padWin
		u.padWin = nil
		w.Close()
	}
}

func main() {
	// -emu runs the software P-6 emulator against a directory of WAV samples
	// laid out like the hardware's 48 slots (A1..H6), instead of connecting to
	// real hardware — handy when the P-6 isn't around. Defaults to the
	// RP6_EMU_SAMPLES environment variable.
	emuDir := flag.String("emu", os.Getenv("RP6_EMU_SAMPLES"),
		"run the P-6 emulator using WAV samples from this directory (A1..H6) instead of the hardware")
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
	// If the CLI didn't pin a pak, reuse the last one picked at runtime so the
	// emulator reopens that pak instead of the built-in kit. Backend selection
	// (useEmu) is deliberately left untouched — see vxrv.
	if strings.TrimSpace(u.emuDir) == "" {
		u.emuDir = u.savedEmuDir()
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
	// USB-audio VU capture needs raw device access (and the mic permission on
	// mobile) — desktop only. External MIDI controllers (MacroPad) and the P-6
	// device watcher use ALSA (desktop) or Web MIDI (web); Android reaches USB
	// MIDI a different way (see startAndroidMIDI); iOS has no MIDI path.
	if !onWeb && !onMobile {
		u.startAudio()
	}
	if !onMobile {
		u.startMIDIInput()
		u.startDeviceWatch()
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

	w.Resize(fyne.NewSize(858, 900))
	w.ShowAndRun()
}
