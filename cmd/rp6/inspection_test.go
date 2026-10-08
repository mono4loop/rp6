package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"

	uiinspect "github.com/mono4loop/rp6/internal/ui/inspect"
	uitheme "github.com/mono4loop/rp6/internal/ui/theme"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const layoutArtifactEnv = "RP6_UPDATE_LAYOUT_ARTIFACTS"

type layoutScenario struct {
	name         string
	formFactor   string
	page         string // active application page ("" = the default PLAY page)
	pixel        uiinspect.PixelSize
	scale        float32
	initialScale float32
	console      bool
	maximized    bool
	mobile       bool
	tablet       bool
	insets       screenInsets // pixels the platform keeps content out of (phones: phoneInsets)
	configure    func(*ui)
	required     []string
	hidden       []string
	fit          []string
	overlaps     []string
	touch        []string
	notes        []string
	padPixels    [2]int
	stepPixels   [2]int
}

// screenInsets are the physical pixels a platform keeps app content out of, so
// a scenario measures the area the app really lays out in rather than the panel.
type screenInsets struct{ top, right, bottom, left int }

// apply returns the content area left of a panel size after the insets.
func (in screenInsets) apply(p uiinspect.PixelSize) uiinspect.PixelSize {
	return uiinspect.PixelSize{Width: p.Width - in.left - in.right, Height: p.Height - in.top - in.bottom}
}

// phoneInsets is what an Android phone takes off its panel before Fyne lays the
// app out: the status bar / display cutout (200px on the Pixel 10 Pro and Pro
// XL, per `dumpsys window`), the gesture navigation bar (72px), and the mobile
// driver padding the content by theme.Padding (4dp = 12px at 3x) on every side
// (Fyne's mobile canvas sizes content to its InteractiveArea). Without these the
// harness is ~90dp taller than the phone and passes layouts the device crams —
// which is how the phone layout regressed.
var phoneInsets = screenInsets{top: 200 + 12, right: 12, bottom: 72 + 12, left: 12}

// layoutScenarios mirror resolutions.txt: the fixed set of supported form
// factors (see docs/architecture/layouts.md). Each target maps to exactly one
// designed variant — window (fixed desktop), console (desktop full screen),
// phone (mobile portrait) or tablet (mobile landscape) — with no continuous
// adaptation. The stretched window and the final entry (a Wayland scale-change
// regression guard) are not supported resolutions.
var layoutScenarios = []layoutScenario{
	{
		name:       "thinkpad-x13-window-850x950",
		formFactor: "desktop-window",
		pixel:      uiinspect.PixelSize{Width: 850, Height: 950},
		scale:      1,
		configure:  productionScene,
		required:   []string{"rack.transport", "rack.sequencer", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.sequencer", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid"},
		overlaps:   []string{"rack.transport", "rack.sequencer", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(4, 16)...),
		notes:      []string{"Desktop windowed size (resolutions.txt: 850x950); the window opens here and can't be dragged smaller. The sequencer (4 tracks) is shown by default above the 12-pad grid."},
		padPixels:  [2]int{80, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "thinkpad-x13-window-p6-850x950",
		formFactor: "desktop-window-p6",
		pixel:      uiinspect.PixelSize{Width: 850, Height: 950},
		scale:      1,
		configure:  p6WindowScene,
		required:   []string{"rack.transport", "rack.p6", "rack.sequencer", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.pad-fx", "rack.keys-fx", "rack.keyboard", "rack.paks"},
		// rack.pads / pads.grid are left out of the fit (under-min) check: this is
		// the tightest desktop window — the P-6 hardware rack (144px the emulator
		// lacks) plus the 4-track sequencer above the 12 pads squeezes the
		// (mouse-driven) pads below their preferred min. The sequencer is never
		// clipped and the padPixels physical contract below still guards the pads.
		fit:        []string{"rack.transport", "rack.p6", "rack.sequencer", "rack.vu", "rack.navigation", "rack.status", "sequencer.grid"},
		overlaps:   []string{"rack.transport", "rack.p6", "rack.sequencer", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(3, 16)...),
		notes:      []string{"Same 850x950 window with the P-6 hardware backend: PATTERN sits beside TEMPO so the P-6 rack (Play + the four Delay/Reverb knobs) is one row, and the sequencer reserves three step rows (seq(rows: 3)) with the fourth track scrolling inside it, so the layout also fits 1920x1080 at 1.25."},
		padPixels:  [2]int{66, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "fhd-125-window-p6-1060x993",
		formFactor: "desktop-window-p6-125",
		pixel:      uiinspect.PixelSize{Width: 1060, Height: 993},
		scale:      1.25,
		configure:  p6WindowScene,
		required:   []string{"rack.transport", "rack.p6", "rack.sequencer", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.pad-fx", "rack.keys-fx", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.p6", "rack.sequencer", "rack.vu", "rack.navigation", "rack.status", "sequencer.grid"},
		overlaps:   []string{"rack.transport", "rack.p6", "rack.sequencer", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(3, 16)...),
		notes:      []string{"The tightest windowed case: the P-6 window on a 1920x1080 screen at 1.25 (1536x864 logical), where the window fits 848x794 under GNOME's top bar and title bar. PATTERN beside TEMPO and the three reserved sequencer rows are what make it fit."},
		padPixels:  [2]int{66, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "thinkpad-x13-window-stretched-1100x1000",
		formFactor: "desktop-window-stretched",
		pixel:      uiinspect.PixelSize{Width: 1100, Height: 1000},
		scale:      1,
		configure:  productionScene,
		required:   []string{"rack.transport", "rack.sequencer", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.sequencer", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid"},
		overlaps:   []string{"rack.transport", "rack.sequencer", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(4, 16)...),
		notes:      []string{"Not a supported resolution: a guard that the window variant tolerates being dragged larger than 850x950 (the window is resizable so that it can be maximized)."},
		padPixels:  [2]int{80, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "thinkpad-x13-fullscreen-p6-1920x1200",
		formFactor: "laptop-fullscreen-p6",
		pixel:      uiinspect.PixelSize{Width: 1920, Height: 1200},
		scale:      1,
		console:    true,
		configure:  p6ConsoleScene,
		required:   []string{"rack.transport", "rack.p6", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.keys-fx"},
		fit:        []string{"rack.transport", "rack.p6", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.p6", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(6, 16)...),
		notes:      []string{"Desktop console with the P-6 hardware backend: the P-6 rack spans the bottom with Play, PATTERN and the four Delay/Reverb knobs on a single row (the console is the one desktop arrangement wide enough), above the keyboard."},
		padPixels:  [2]int{80, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "thinkpad-x13-fullscreen-1920x1200",
		formFactor: "laptop-fullscreen",
		pixel:      uiinspect.PixelSize{Width: 1920, Height: 1200},
		scale:      1,
		console:    true,
		configure:  desktopConsoleScene,
		required:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.keys-fx"},
		fit:        []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(6, 16)...),
		notes:      []string{"Desktop full-screen mixing console (resolutions.txt: ThinkPad X13 1920x1200)."},
		padPixels:  [2]int{80, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "thinkpad-x13-maximized-1920x1130",
		formFactor: "laptop-maximized",
		pixel:      uiinspect.PixelSize{Width: 1920, Height: 1130},
		scale:      1,
		maximized:  true,
		configure:  desktopConsoleScene,
		required:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.keys-fx"},
		fit:        []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(6, 16)...),
		notes:      []string{"The console in a maximized window (title-bar double-click): the 1920x1200 X13 at 1x, less GNOME's top bar and the libdecor title bar."},
		padPixels:  [2]int{80, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "thinkpad-x13-125-maximized-1920x1114",
		formFactor: "laptop-maximized-125",
		pixel:      uiinspect.PixelSize{Width: 1920, Height: 1114},
		scale:      1.25,
		maximized:  true,
		configure:  desktopConsoleScene,
		required:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.keys-fx"},
		fit:        []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(6, 16)...),
		notes:      []string{"The console maximized on the ThinkPad X13 at GNOME's 1.25 scale, as measured live: 1536x891 logical (the 1920x1200 panel less the top bar and the libdecor title bar)."},
		padPixels:  [2]int{80, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "wqxga-2x-maximized-2560x1462",
		formFactor: "laptop-maximized-2x",
		pixel:      uiinspect.PixelSize{Width: 2560, Height: 1462},
		scale:      2,
		maximized:  true,
		configure:  desktopConsoleScene,
		required:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.keys-fx"},
		fit:        []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      desktopTouchTargets(),
		notes:      []string{"The smallest supported screen: a 2560x1600 laptop at 2x (1280x800 logical) with the console maximized, 1280x731 once GNOME's top bar and the title bar are taken off."},
		padPixels:  [2]int{80, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "asus-rog-3440x1440",
		formFactor: "ultrawide-fullscreen",
		pixel:      uiinspect.PixelSize{Width: 3440, Height: 1440},
		scale:      1,
		console:    true,
		configure:  desktopConsoleScene,
		required:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.keys-fx"},
		fit:        []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(6, 16)...),
		notes:      []string{"Ultrawide full-screen console (resolutions.txt: Asus ROG 3440x1440, 21:9); the same console variant as 16:10, proportional splits absorb the wider aspect."},
		padPixels:  [2]int{80, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "pixel-10-pro-xl-1344x2992",
		formFactor: "phone-portrait",
		pixel:      uiinspect.PixelSize{Width: 1344, Height: 2992},
		scale:      3,
		mobile:     true,
		insets:     phoneInsets,
		configure:  phoneScene,
		required:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      phoneTouchTargets(),
		notes:      []string{"486 ppi maps to Fyne's Android 3x scale bucket; the app's content area is 440x898.7 once the status bar, gesture nav and driver padding are taken off the 448x997.3 panel (phoneInsets). Page nav beside TEMPO; sequencer and optional racks left off; the pads fill the rack width (pads(cells: fill), 184px cells)."},
		padPixels:  [2]int{165, 190},
	},
	{
		name:       "pixel-10-pro-xl-racks-1344x2992",
		formFactor: "phone-portrait-racks",
		pixel:      uiinspect.PixelSize{Width: 1344, Height: 2992},
		scale:      3,
		mobile:     true,
		insets:     phoneInsets,
		configure:  phoneRacksScene,
		required:   []string{"rack.transport", "rack.paks", "rack.keyboard", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer"},
		fit:        []string{"rack.transport", "rack.paks", "rack.keyboard", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.paks", "rack.keyboard", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      phoneTouchTargets(),
		notes:      []string{"Pixel 10 Pro XL with the PAKS + KEYS racks toggled on above the pads — the state the phone was found crammed in. The two-key pak list (paks(rows: 2)) and the page nav beside TEMPO leave the 4x6 pads height-bound but still above their usual 130px ceiling."},
		padPixels:  [2]int{80, 190},
	},
	{
		name:       "pixel-10-pro-1280x2856",
		formFactor: "phone-portrait",
		pixel:      uiinspect.PixelSize{Width: 1280, Height: 2856},
		scale:      3,
		mobile:     true,
		insets:     phoneInsets,
		configure:  phoneScene,
		required:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      phoneTouchTargets(),
		notes:      []string{"495 ppi maps to Fyne's Android 3x scale bucket; the app's content area is 418.7x853.3 once the status bar, gesture nav and driver padding are taken off the 426.7x952 panel (phoneInsets). Page nav beside TEMPO; sequencer and optional racks left off; the pads fill the rack width (pads(cells: fill), 173px cells)."},
		padPixels:  [2]int{165, 190},
	},
	{
		name:       "pixel-10-pro-racks-1280x2856",
		formFactor: "phone-portrait-racks",
		pixel:      uiinspect.PixelSize{Width: 1280, Height: 2856},
		scale:      3,
		mobile:     true,
		insets:     phoneInsets,
		configure:  phoneRacksScene,
		required:   []string{"rack.transport", "rack.paks", "rack.keyboard", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer"},
		fit:        []string{"rack.transport", "rack.paks", "rack.keyboard", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.paks", "rack.keyboard", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      phoneTouchTargets(),
		notes:      []string{"The smaller Pixel 10 Pro with the PAKS + KEYS racks toggled on above the pads: the tightest supported phone state (height-bound, ~123px cells). Adding the FX rack on top of these two is the one phone state that drops the pads below the 80px floor."},
		padPixels:  [2]int{80, 190},
	},
	{
		name:       "pixel-10-pro-p6-1280x2856",
		formFactor: "phone-portrait-p6",
		pixel:      uiinspect.PixelSize{Width: 1280, Height: 2856},
		scale:      3,
		mobile:     true,
		insets:     phoneInsets,
		configure:  phoneP6Scene,
		required:   []string{"rack.transport", "rack.p6", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.p6", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.p6", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      phoneTouchTargets(),
		notes:      []string{"The Pixel 10 Pro driving a P-6 over USB-C: the P-6 rack's four Delay/Reverb knobs split 2+2 so the rack fits the phone's width (one row of four overflowed it), above the pads."},
		padPixels:  [2]int{80, 190},
	},
	{
		name:       "oneplus-pad-3-3392x2400",
		formFactor: "tablet-landscape",
		pixel:      uiinspect.PixelSize{Width: 3392, Height: 2400},
		scale:      2,
		mobile:     true,
		tablet:     true,
		configure:  tabletScene,
		required:   []string{"rack.transport", "rack.paks", "rack.sequencer", "rack.pads", "rack.keyboard", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx"},
		fit:        []string{"rack.transport", "rack.paks", "rack.sequencer", "rack.pads", "rack.keyboard", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.paks", "rack.sequencer", "rack.pads", "rack.keyboard", "rack.vu", "rack.navigation", "rack.status"},
		// The sequencer steps are validated by stepPixels (physical) rather than
		// the 32-logical touch min: at 2x a 40-50px step is only 20-25 logical, so
		// forcing 32 logical would blow the step budget for 16 steps on a tablet.
		touch:      desktopTouchTargets(),
		notes:      []string{"315 ppi maps to Fyne's Android 2x scale bucket; logical canvas is 1696x1200 (resolutions.txt: OnePlus Pad 3, 7:5). Paks rail beside a seq-over-pads column."},
		padPixels:  [2]int{80, 130},
		stepPixels: [2]int{40, 50},
	},
	{
		name:       "thinkpad-x13-loop-window-850x950",
		formFactor: "desktop-window-loop",
		page:       "loop",
		pixel:      uiinspect.PixelSize{Width: 850, Height: 950},
		scale:      1,
		configure:  loopScene,
		required:   []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.recorder", "rack.vu", "rack.navigation", "rack.status"},
		overlaps:   []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      loopTouchTargets(),
		notes:      []string{"LOOP page in the fixed 850x950 window: the eight-track recorder + TEMPO/VU on top, the two-bank pads below. The second application page (see loop.layout)."},
		padPixels:  [2]int{80, 130},
	},
	{
		name:       "thinkpad-x13-loop-fullscreen-1920x1200",
		formFactor: "laptop-fullscreen-loop",
		page:       "loop",
		pixel:      uiinspect.PixelSize{Width: 1920, Height: 1200},
		scale:      1,
		console:    true,
		configure:  loopScene,
		required:   []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      loopTouchTargets(),
		notes:      []string{"LOOP page full screen (ThinkPad 1920x1200): TEMPO/VU on the left rail, the pads and the recorder sharing the centre split."},
		padPixels:  [2]int{80, 130},
	},
	{
		name:       "pixel-10-pro-xl-loop-1344x2992",
		formFactor: "phone-portrait-loop",
		page:       "loop",
		pixel:      uiinspect.PixelSize{Width: 1344, Height: 2992},
		scale:      3,
		mobile:     true,
		insets:     phoneInsets,
		configure:  loopPhoneScene,
		required:   []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      withoutConsole(loopTouchTargets()),
		notes:      []string{"LOOP page on a phone (Pixel 10 Pro XL, content area less phoneInsets): page nav beside TEMPO, the recorder above the pads (height-bound pads, ~145px), VU + toggles along the bottom."},
		padPixels:  [2]int{80, 190},
	},
	{
		name:       "oneplus-pad-3-loop-3392x2400",
		formFactor: "tablet-landscape-loop",
		page:       "loop",
		pixel:      uiinspect.PixelSize{Width: 3392, Height: 2400},
		scale:      2,
		mobile:     true,
		tablet:     true,
		configure:  loopScene,
		required:   []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      loopTouchTargets(),
		notes:      []string{"LOOP page on a tablet (OnePlus Pad 3): the recorder stacked over the pads in the centre column, TEMPO/VU on top."},
		padPixels:  [2]int{80, 130},
	},
	{
		name:         "regression-scale-transition-3072x1920",
		formFactor:   "desktop-hidpi-fullscreen",
		pixel:        uiinspect.PixelSize{Width: 3072, Height: 1920},
		scale:        2,
		initialScale: 1.25,
		console:      true,
		configure:    desktopConsoleScene,
		required:     []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:       []string{"rack.p6", "rack.keys-fx"},
		fit:          []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid"},
		overlaps:     []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:        desktopTouchTargets(),
		notes:        []string{"Regression guard (not a supported resolution): a late Wayland scale transition from 1.25x layout geometry to 2x rendering must force a real relayout."},
		padPixels:    [2]int{80, 130},
		stepPixels:   [2]int{40, 50},
	},
}

func TestInspectionTargetsHaveUniqueIDs(t *testing.T) {
	u, _ := newInspectionUI(t)
	seen := map[string]bool{}
	objects := map[fyne.CanvasObject]string{}
	for _, target := range u.inspectionTargets() {
		assert.NotEmpty(t, target.ID)
		assert.False(t, seen[target.ID], "duplicate semantic ID %q", target.ID)
		seen[target.ID] = true
		if target.Object != nil {
			assert.Empty(t, objects[target.Object], "semantic IDs %q and %q refer to the same object", objects[target.Object], target.ID)
			objects[target.Object] = target.ID
		}
	}
	assert.Greater(t, len(seen), 400, "inspection surface includes generated pads and sequencer cells")
}

func TestCurrentLayoutsAtTargetResolutions(t *testing.T) {
	for _, scenario := range layoutScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			bundle := captureLayoutScenario(t, scenario)
			// The page-navigation strip and its keys are present in every scenario
			// (the document declares PLAY + LOOP), so validate them universally.
			contract := uiinspect.Contract{
				Required:       append([]string{"rack.pagenav"}, scenario.required...),
				Hidden:         scenario.hidden,
				Fit:            append([]string{"rack.pagenav"}, scenario.fit...),
				NonOverlapping: append([]string{"rack.pagenav"}, scenario.overlaps...),
				TouchTargets:   append([]string{"navigation.page.play", "navigation.page.loop"}, scenario.touch...),
				MinTouch:       uiinspect.Size{Width: 32, Height: 32},
			}
			contract.Contained = append(contract.Contained, rackContainmentContracts()...)
			if scenario.padPixels[0] > 0 {
				contract.PhysicalSquares = append(contract.PhysicalSquares, uiinspect.PhysicalSquareContract{
					IDs: activePadIDs(24), MinPixels: scenario.padPixels[0], MaxPixels: scenario.padPixels[1], Tolerance: 1,
				})
			}
			if scenario.stepPixels[0] > 0 {
				contract.PhysicalSquares = append(contract.PhysicalSquares, uiinspect.PhysicalSquareContract{
					IDs: activeStepIDs(6, 16), MinPixels: scenario.stepPixels[0], MaxPixels: scenario.stepPixels[1], Tolerance: 1,
				})
			}
			problems := uiinspect.Check(bundle.Snapshot, contract)
			for _, problem := range problems {
				t.Error(problem)
			}
		})
	}
}

func captureLayoutScenario(t *testing.T, scenario layoutScenario) uiinspect.Bundle {
	t.Helper()
	u, w := newInspectionUI(t)
	// The real platform is a compile-time constant, so exercise the phone/tablet
	// variants by overriding it per scenario (see layoutEnv).
	mobile, tablet := scenario.mobile, scenario.tablet
	u.mobileForTest = &mobile
	u.tabletForTest = &tablet
	u.fullScreen = scenario.console
	u.maximized = scenario.maximized
	if scenario.page != "" {
		u.activePage = scenario.page // navigate to the scenario's page before the first relayout
		u.updatePageNav()            // light the active page's key (setPage does this in the app)
	}
	// The content area is the panel less the insets the platform keeps content
	// out of (phones: phoneInsets), converted to logical units by the scale.
	area := scenario.insets.apply(scenario.pixel)
	logical := fyne.NewSize(float32(area.Width)/scenario.scale, float32(area.Height)/scenario.scale)
	u.relayout()
	if scenario.configure != nil {
		scenario.configure(u)
	}
	c, ok := w.Canvas().(software.WindowlessCanvas)
	require.True(t, ok, "headless canvas supports deterministic scale")
	initialScale := scenario.scale
	if scenario.initialScale > 0 {
		initialScale = scenario.initialScale
	}
	c.SetScale(initialScale)
	w.Resize(logical)
	u.relayout()
	if scenario.initialScale > 0 {
		// Match Fyne's Wayland scale callback: rendering scale changes and the
		// canvas is refreshed, but no resize/layout pass is guaranteed.
		c.SetScale(scenario.scale)
		stale := uiinspect.SnapshotCanvas(w.Canvas(), u.inspectionMetadata(scenario.name+"-stale", scenario.formFactor), u.inspectionTargets())
		staleProblems := uiinspect.Check(stale, uiinspect.Contract{PhysicalSquares: []uiinspect.PhysicalSquareContract{
			{IDs: activePadIDs(24), MinPixels: scenario.padPixels[0], MaxPixels: scenario.padPixels[1], Tolerance: 1},
		}})
		assert.True(t, hasProblemCode(staleProblems, "physical-size"), "regression setup must reproduce stale physical sizing before relayout")
		u.relayoutIfScaleChanged()
		assert.InDelta(t, scenario.scale, u.layoutScale, 0.01, "scale transition triggered a real relayout")
	}

	bundle, err := uiinspect.CaptureBundle(w.Canvas(), u.inspectionMetadata(scenario.name, scenario.formFactor, scenario.notes...), u.inspectionTargets())
	require.NoError(t, err)
	assert.Equal(t, area, bundle.Snapshot.Canvas.Pixel)

	if updateLayoutArtifacts() {
		dir := filepath.Join("testdata", "layout-inspection")
		require.NoError(t, uiinspect.WriteBundle(dir, scenario.name, bundle))
		t.Logf("layout artifacts: %s", filepath.Join(dir, scenario.name+".{json,png}"))
	}
	return bundle
}

func newInspectionUI(t *testing.T) (*ui, fyne.Window) {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	test.ApplyTheme(t, uitheme.Amber{})
	u := newUI()
	u.useEmu = true
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	u.build(w)
	return u, w
}

func desktopConsoleScene(u *ui) {
	u.setVisible(u.fxRack.Object(), u.padFXBtn, true)
	u.setVisible(u.keyboardFXRack.Object(), u.keysFXBtn, false)
	u.setVisible(u.keyboardRack.Object(), u.keysBtn, true)
	u.setVisible(u.paksRack.Object(), u.paksBtn, true)
	u.setVisible(u.seqRack.Object(), u.seqBtn, true)
	u.seqRack.applyTracks(6)
	u.seqSide = true
	u.seqRack.docked = true
	u.seqRack.dockBtn.SetOn(true)
	u.setVisible(u.padRackObj, u.padBtn, true)
	u.setVisible(u.meterArea, u.meterBtn, true)
	u.paksRack.lister = inspectionPakItems
	u.paksRack.refresh("/kits/modular-hits")
	u.setConnected(true)
	u.setStatus("emulator online - layout inspection scene")
}

func productionScene(u *ui) {
	u.paksRack.lister = inspectionPakItems
	u.paksRack.refresh("/kits/modular-hits")
	u.setConnected(true)
	u.setStatus("emulator online - production layout scene")
}

// p6WindowScene switches to the P-6 hardware backend so the P-6-only rack is
// shown — the real "850x950 window mode" target in resolutions.txt.
func p6WindowScene(u *ui) {
	u.useEmu = false
	u.applyBackendGating() // reveals the P-6 rack, disables/hides the emulator keys-FX
	u.setConnected(true)
	u.setStatus("P-6 online - window layout scene")
}

// p6ConsoleScene is the desktop console with the P-6 hardware backend: the
// console scene's racks plus the P-6 rack (its one-row arrangement) along the
// bottom, in place of the emulator's keyboard FX.
func p6ConsoleScene(u *ui) {
	desktopConsoleScene(u)
	u.useEmu = false
	u.applyBackendGating()
	u.setStatus("P-6 online - console layout scene")
}

func tabletScene(u *ui) {
	// The tablet variant force-shows paks + keyboard (show: true); the sequencer
	// is undocked in the centre column and the pads share it, so show them here.
	productionScene(u)
	u.setVisible(u.seqRack.Object(), u.seqBtn, true)
	u.seqSide = false
	u.seqRack.docked = false
	u.seqRack.dockBtn.SetOn(false)
	u.seqRack.applyTracks(6)
	u.setVisible(u.padRackObj, u.padBtn, true)
	u.setVisible(u.meterArea, u.meterBtn, true)
	u.setStatus("emulator online - tablet layout scene")
}

// hideDesktopOnlyControls removes the bottom-bar controls a phone build doesn't
// compile or place — the JAM toggles (desktop-only) and the CONSOLE key (omitted
// on mobile, see build) — from the desktop test process, so a phone scene
// measures the real phone bottom bar. The CONSOLE key alone is 87dp, enough to
// overfill the Pixel 10 Pro's bar.
func hideDesktopOnlyControls(u *ui) {
	for _, control := range u.jamControls {
		control.Hide()
	}
	u.consoleBtn.Hide()
}

func phoneScene(u *ui) {
	hideDesktopOnlyControls(u)
	u.setVisible(u.fxRack.Object(), u.padFXBtn, false)
	u.setVisible(u.keyboardFXRack.Object(), u.keysFXBtn, false)
	u.setVisible(u.seqRack.Object(), u.seqBtn, false)
	u.setVisible(u.keyboardRack.Object(), u.keysBtn, false)
	u.setVisible(u.paksRack.Object(), u.paksBtn, false)
	u.setVisible(u.padRackObj, u.padBtn, true)
	u.setVisible(u.meterArea, u.meterBtn, true)
	u.setConnected(true)
	u.setStatus("emulator online")
}

// phoneRacksScene is the phone with the optional PAKS + KEYS racks toggled on
// above the pads — the state the Pixel 10 Pro XL was found crammed in — with a
// few paks installed, so the contract proves the pads keep their physical size
// with both racks stacked above them.
func phoneRacksScene(u *ui) {
	phoneScene(u)
	u.setVisible(u.paksRack.Object(), u.paksBtn, true)
	u.setVisible(u.keyboardRack.Object(), u.keysBtn, true)
	u.paksRack.lister = inspectionPakItems
	u.paksRack.refresh("/kits/modular-hits")
	u.setStatus("emulator online - paks + keys shown")
}

// phoneP6Scene is the phone with the P-6 hardware as the active backend, so the
// P-6 rack (transport, PATTERN, Delay/Reverb) is placed above the pads.
func phoneP6Scene(u *ui) {
	phoneScene(u)
	u.useEmu = false
	u.applyBackendGating() // reveals the P-6 rack
	u.setConnected(true)
	u.setStatus("P-6 online - phone layout scene")
}

// loopPhoneScene is loopScene on a phone, less the controls a phone build
// doesn't have (see hideDesktopOnlyControls).
func loopPhoneScene(u *ui) {
	loopScene(u)
	hideDesktopOnlyControls(u)
}

// withoutConsole drops the CONSOLE key from a touch-target list for the phone
// scenes, where it's hidden (see hideDesktopOnlyControls).
func withoutConsole(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "navigation.console" {
			out = append(out, id)
		}
	}
	return out
}

// loopScene sets up the LOOP page (the recorder is force-shown by the loop
// variant; the pads stay visible as its record source). The active page itself
// is set from the scenario (see captureLayoutScenario).
func loopScene(u *ui) {
	// JAM is desktop-only and not compiled into mobile builds; hide the test
	// process's contribution on the mobile loop scenes so the bar measures the
	// real mobile control set (matches phoneScene).
	if u.mobileForTest != nil && *u.mobileForTest {
		for _, control := range u.jamControls {
			control.Hide()
		}
	}
	u.setConnected(true)
	u.setStatus("emulator online - loop page")
}

// loopTouchTargets are the LOOP page's finger targets: the page nav (added
// globally), the rack toggles, the pad tools + cells, and the recorder's header
// transport. The sequencer's controls are omitted — it's off on this page.
func loopTouchTargets() []string {
	return append([]string{
		"navigation.play", "navigation.p6", "navigation.fx", "navigation.paks", "navigation.vu", "navigation.console",
		"pads.float", "pads.listen", "pads.layout", "pads.store", "pads.device",
		"recorder.play", "recorder.quant", "recorder.export",
	}, activePadIDs(24)...)
}

func inspectionPakItems() []pakItem {
	return []pakItem{
		{ID: "modular-hits", Name: "Modular Hits", Dir: "/kits/modular-hits"},
		{ID: "tape-drums", Name: "Tape Drums", Dir: "/kits/tape-drums"},
		{ID: "field-notes", Name: "Field Notes", Dir: "/kits/field-notes"},
	}
}

func desktopTouchTargets() []string {
	return append([]string{
		"navigation.play", "navigation.p6", "navigation.fx", "navigation.paks", "navigation.vu", "navigation.console",
		"pads.float", "pads.listen", "pads.layout", "pads.store", "pads.device",
		"sequencer.play", "sequencer.tracks", "sequencer.slot", "sequencer.copy", "sequencer.clear", "sequencer.save", "sequencer.dock", "sequencer.mute", "sequencer.bars",
	}, activePadIDs(24)...)
}

// phoneTouchTargets are the phone's finger targets: the rack toggles (no CONSOLE
// key on mobile), the pad tools and the 24 visible pad cells.
func phoneTouchTargets() []string {
	return append([]string{
		"navigation.play", "navigation.p6", "navigation.fx", "navigation.paks", "navigation.vu",
		"pads.float", "pads.listen", "pads.layout", "pads.store", "pads.device",
	}, activePadIDs(24)...)
}

func activePadIDs(n int) []string {
	ids := make([]string, n)
	for i := range n {
		ids[i] = fmt.Sprintf("pads.cell.%02d", i+1)
	}
	return ids
}

func activeSequencerStepIDs(tracks, steps int) []string {
	ids := make([]string, 0, tracks*steps)
	for track := 1; track <= tracks; track++ {
		ids = append(ids, fmt.Sprintf("sequencer.track.%d.assign", track))
		for step := 1; step <= steps; step++ {
			ids = append(ids, fmt.Sprintf("sequencer.track.%d.step.%d", track, step))
		}
	}
	return ids
}

func activeStepIDs(tracks, steps int) []string {
	ids := make([]string, 0, tracks*steps)
	for track := 1; track <= tracks; track++ {
		for step := 1; step <= steps; step++ {
			ids = append(ids, fmt.Sprintf("sequencer.track.%d.step.%d", track, step))
		}
	}
	return ids
}

func rackContainmentContracts() []uiinspect.ContainmentContract {
	padControls := []string{"pads.grid", "pads.float", "pads.listen", "pads.layout", "pads.store", "pads.device"}
	seqControls := []string{"sequencer.header", "sequencer.track-controls", "sequencer.grid"}
	seqGridChildren := activeSequencerStepIDs(8, 64)
	return []uiinspect.ContainmentContract{
		{Parent: "rack.pads", Children: padControls, Tolerance: 0.5},
		{Parent: "pads.grid", Children: activePadIDs(48), Tolerance: 0.5},
		{Parent: "rack.sequencer", Children: seqControls, Tolerance: 0.5},
		{Parent: "sequencer.grid", Children: seqGridChildren, VisibleOnly: true, Tolerance: 0.5},
	}
}

func hasProblemCode(problems []uiinspect.Problem, code string) bool {
	for _, problem := range problems {
		if problem.Code == code {
			return true
		}
	}
	return false
}

func updateLayoutArtifacts() bool { return testEnvBool(layoutArtifactEnv) }

func testEnvBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
