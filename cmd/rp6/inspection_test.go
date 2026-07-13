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
	name       string
	formFactor string
	pixel      uiinspect.PixelSize
	scale      float32
	console    bool
	configure  func(*ui)
	required   []string
	hidden     []string
	fit        []string
	overlaps   []string
	touch      []string
	notes      []string
}

var layoutScenarios = []layoutScenario{
	{
		name:       "thinkpad-x13-1920x1200",
		formFactor: "laptop",
		pixel:      uiinspect.PixelSize{Width: 1920, Height: 1200},
		scale:      1,
		console:    true,
		configure:  desktopConsoleScene,
		required:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.keys-fx"},
		fit:        []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(6, 16)...),
		notes:      []string{"Full-screen desktop console with pads, sequencer, pad FX, keyboard and sample-pak browser; the emulator-only keyboard FX rack is left off."},
	},
	{
		name:       "pixel-10-pro-xl-1344x2992",
		formFactor: "phone-portrait",
		pixel:      uiinspect.PixelSize{Width: 1344, Height: 2992},
		scale:      3,
		configure:  phoneScene,
		required:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      phoneTouchTargets(),
		notes:      []string{"486 ppi maps to Fyne's Android 3x scale bucket; logical canvas is 448x997.3. Sequencer and optional racks are left off."},
	},
	{
		name:       "pixel-10-pro-1280x2856",
		formFactor: "phone-portrait",
		pixel:      uiinspect.PixelSize{Width: 1280, Height: 2856},
		scale:      3,
		configure:  phoneScene,
		required:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.pad-fx", "rack.keys-fx", "rack.sequencer", "rack.keyboard", "rack.paks"},
		fit:        []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid"},
		overlaps:   []string{"rack.transport", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      phoneTouchTargets(),
		notes:      []string{"495 ppi maps to Fyne's Android 3x scale bucket; logical canvas is 426.7x952. Sequencer and optional racks are left off."},
	},
	{
		name:       "asus-rog-3440x1440",
		formFactor: "ultrawide",
		pixel:      uiinspect.PixelSize{Width: 3440, Height: 1440},
		scale:      1,
		console:    true,
		configure:  desktopConsoleScene,
		required:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		hidden:     []string{"rack.p6", "rack.keys-fx"},
		fit:        []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status", "pads.grid", "sequencer.grid", "paks.list", "keyboard.keys"},
		overlaps:   []string{"rack.transport", "rack.pad-fx", "rack.sequencer", "rack.keyboard", "rack.paks", "rack.pads", "rack.vu", "rack.navigation", "rack.status"},
		touch:      append(desktopTouchTargets(), activeSequencerStepIDs(6, 16)...),
		notes:      []string{"Full-screen ultrawide console with pads, sequencer, pad FX, keyboard and sample-pak browser; the emulator-only keyboard FX rack is left off."},
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
			problems := uiinspect.Check(bundle.Snapshot, uiinspect.Contract{
				Required:       scenario.required,
				Hidden:         scenario.hidden,
				Fit:            scenario.fit,
				NonOverlapping: scenario.overlaps,
				TouchTargets:   scenario.touch,
				MinTouch:       uiinspect.Size{Width: 32, Height: 32},
			})
			for _, problem := range problems {
				t.Error(problem)
			}
		})
	}
}

func captureLayoutScenario(t *testing.T, scenario layoutScenario) uiinspect.Bundle {
	t.Helper()
	u, w := newInspectionUI(t)
	u.fullScreen = scenario.console
	u.lastFullScreen = scenario.console
	logical := fyne.NewSize(float32(scenario.pixel.Width)/scenario.scale, float32(scenario.pixel.Height)/scenario.scale)
	u.compact = classifyCompact(false, logical.Width, logical.Height)
	u.relayout()
	if scenario.configure != nil {
		scenario.configure(u)
	}
	c, ok := w.Canvas().(software.WindowlessCanvas)
	require.True(t, ok, "headless canvas supports deterministic scale")
	c.SetScale(scenario.scale)
	w.Resize(logical)
	u.relayout()

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

func updateLayoutArtifacts() bool { return testEnvBool(layoutArtifactEnv) }

func testEnvBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
