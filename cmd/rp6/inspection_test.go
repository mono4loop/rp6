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
	mobile       bool
	tablet       bool
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

// layoutScenarios mirror resolutions.txt: the fixed set of supported form
// factors (see docs/architecture/layouts.md). Each target maps to exactly one
// designed variant — window (fixed desktop), console (desktop full screen),
// phone (mobile portrait) or tablet (mobile landscape) — with no continuous
// adaptation. The final entry is a Wayland scale-change regression guard, not a
// supported resolution.
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
		notes:      []string{"Fixed, non-resizable desktop windowed size (resolutions.txt: 850x950). The sequencer (4 tracks) is shown by default above the 12-pad grid."},
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
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(4, 16)...),
		notes:      []string{"Same fixed 850x950 window with the P-6 hardware backend: the P-6 rack wraps onto two rows and the 4-track sequencer fits (unclipped) above the 12-pad grid. This is the tightest desktop window, so the mouse-driven pads are compact."},
		padPixels:  [2]int{66, 130},
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
		configure:  phoneScene,
		required:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      phoneTouchTargets(),
		notes:      []string{"486 ppi maps to Fyne's Android 3x scale bucket; logical canvas is 448x997.3. Sequencer and optional racks are left off."},
		padPixels:  [2]int{80, 130},
	},
	{
		name:       "pixel-10-pro-1280x2856",
		formFactor: "phone-portrait",
		pixel:      uiinspect.PixelSize{Width: 1280, Height: 2856},
		scale:      3,
		mobile:     true,
		configure:  phoneScene,
		required:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      phoneTouchTargets(),
		notes:      []string{"495 ppi maps to Fyne's Android 3x scale bucket; logical canvas is 426.7x952. Sequencer and optional racks are left off."},
		padPixels:  [2]int{80, 130},
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
		configure:  loopScene,
		required:   []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.recorder", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      loopTouchTargets(),
		notes:      []string{"LOOP page on a phone (Pixel 10 Pro XL): the recorder above the pads, VU + page nav + toggles along the bottom."},
		padPixels:  [2]int{80, 130},
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
	if scenario.page != "" {
		u.activePage = scenario.page // navigate to the scenario's page before the first relayout
		u.updatePageNav()            // light the active page's key (setPage does this in the app)
	}
	logical := fyne.NewSize(float32(scenario.pixel.Width)/scenario.scale, float32(scenario.pixel.Height)/scenario.scale)
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
	assert.Equal(t, scenario.pixel, bundle.Snapshot.Canvas.Pixel)

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

func phoneScene(u *ui) {
	// JAM is not compiled into Android/iOS builds; hide the desktop-only test
	// process's contribution so this scene measures the real mobile control set.
	for _, control := range u.jamControls {
		control.Hide()
	}
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

func phoneTouchTargets() []string {
	return append([]string{
		"navigation.play", "navigation.p6", "navigation.fx", "navigation.paks", "navigation.vu", "navigation.console",
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
