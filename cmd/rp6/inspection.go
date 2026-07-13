package main

import (
	"fmt"

	"fyne.io/fyne/v2"

	uiinspect "github.com/mono4loop/rp6/internal/ui/inspect"
)

// inspectionTargets is RP6's semantic automation surface. These IDs describe
// user-facing concepts, not renderer objects, so layout tests and visual
// artifacts remain useful when a custom widget's drawing implementation changes.
func (u *ui) inspectionTargets() []uiinspect.Target {
	targets := []uiinspect.Target{
		inspectionTarget("layout.root", "layout", "RP6", "application", u.root, true, nil),
		inspectionTarget("rack.transport", "rack", "Transport", "group", u.transportRack, true, nil),
		inspectionTarget("rack.p6", "rack", "P-6", "group", u.p6Obj, true, nil),
		inspectionTarget("rack.pad-fx", "rack", "Pad effects", "group", u.fxRack.Object(), true, nil),
		inspectionTarget("rack.keys-fx", "rack", "Keyboard effects", "group", u.keyboardFXRack.Object(), true, nil),
		inspectionTarget("rack.sequencer", "rack", "Sequencer", "group", u.seqRack.Object(), true, map[string]any{
			"tracks": u.seq.Tracks(), "docked": u.seqSide,
		}),
		inspectionTarget("rack.keyboard", "rack", "Keyboard", "group", u.keyboardRack.Object(), true, nil),
		inspectionTarget("rack.paks", "rack", "Sample paks", "group", u.paksRack.Object(), true, nil),
		inspectionTarget("rack.pads", "rack", "Pads", "group", u.padRackObj, true, map[string]any{
			"floating": u.padFloating, "layout": int(u.padLayout), "page": u.grid.Page(),
		}),
		inspectionTarget("rack.vu", "rack", "VU meter", "group", u.meterArea, true, nil),
		inspectionTarget("rack.navigation", "rack", "Rack navigation", "group", u.controlBar, true, nil),
		inspectionTarget("rack.status", "rack", "Status", "status", u.statusBar, true, nil),

		inspectionTarget("transport.tempo", "knob", "Tempo", "button", u.tempo.Object(), false, map[string]any{"value": u.tempo.Value()}),
		inspectionTarget("p6.play", "button", "P-6 play or stop", "button", u.playBtn, false, nil),
		inspectionTarget("p6.pattern", "knob", "Pattern", "button", u.patternStep.Object(), false, map[string]any{"value": u.patternStep.Value()}),

		inspectionTarget("pad-fx.roll", "toggle", "Roll", "button", u.fxRack.roll, false, rackToggleState(u.fxRack.roll)),
		inspectionTarget("pad-fx.rate", "knob", "Roll rate", "button", u.fxRack.rate.Object(), false, map[string]any{"value": u.fxRack.rate.Value()}),

		inspectionTarget("sequencer.header", "group", "Sequencer controls", "group", u.seqRack.header, false, nil),
		inspectionTarget("sequencer.track-controls", "group", "Armed track controls", "group", u.seqRack.header2, false, nil),
		inspectionTarget("sequencer.grid", "grid", "Sequencer steps", "group", u.seqRack.trackBox, false, nil),
		inspectionTarget("sequencer.play", "button", "Sequencer play or stop", "button", u.seqRack.playBtn, false, nil),
		inspectionTarget("sequencer.tracks", "knob", "Track count", "button", u.seqRack.tracksStep.Object(), false, map[string]any{"value": u.seqRack.tracksStep.Value()}),
		inspectionTarget("sequencer.slot", "knob", "Sequence slot", "button", u.seqRack.slotStep.Object(), false, map[string]any{"value": u.seqRack.slotStep.Value()}),
		inspectionTarget("sequencer.copy", "button", "Copy sequence", "button", u.seqRack.copyBtn, false, rackToggleState(u.seqRack.copyBtn)),
		inspectionTarget("sequencer.clear", "button", "Clear sequence", "button", u.seqRack.clearBtn, false, rackToggleState(u.seqRack.clearBtn)),
		inspectionTarget("sequencer.save", "button", "Save sequence", "button", u.seqRack.saveBtn, false, rackToggleState(u.seqRack.saveBtn)),
		inspectionTarget("sequencer.dock", "button", "Dock sequencer", "button", u.seqRack.dockBtn, false, rackToggleState(u.seqRack.dockBtn)),
		inspectionTarget("sequencer.mute", "button", "Mute armed track", "button", u.seqRack.armMuteBtn, false, rackToggleState(u.seqRack.armMuteBtn)),
		inspectionTarget("sequencer.bars", "button", "Bars for armed track", "button", u.seqRack.armBarsBtn, false, rackToggleState(u.seqRack.armBarsBtn)),

		inspectionTarget("keyboard.octave", "knob", "Keyboard octave", "button", u.keyboardRack.oct.Object(), false, map[string]any{"value": u.keyboardRack.oct.Value()}),
		inspectionTarget("keyboard.keys", "keyboard", "Piano keyboard", "button", u.keyboardRack.piano, false, nil),
		inspectionTarget("paks.header", "group", "Sample pak controls", "group", u.paksRack.header, false, nil),
		inspectionTarget("paks.filter", "input", "Filter sample paks", "text", u.paksRack.search, false, map[string]any{"value": u.paksRack.search.Text}),
		inspectionTarget("paks.list", "list", "Installed sample paks", "group", u.paksRack.scroll, false, nil),

		inspectionTarget("pads.float", "button", "Float or dock pads", "button", u.padFloatBtn, false, rackToggleState(u.padFloatBtn)),
		inspectionTarget("pads.listen", "button", "Listen to MIDI input", "button", u.midiInBtn, false, rackToggleState(u.midiInBtn)),
		inspectionTarget("pads.layout", "button", "Pad layout", "button", u.layoutBtn, false, map[string]any{"state": u.layoutBtn.State()}),
		inspectionTarget("pads.store", "button", "Open sample-pak store", "button", u.storeToggle, false, rackToggleState(u.storeToggle)),
		inspectionTarget("pads.device", "button", "Active device", "button", u.deviceBadge, false, map[string]any{"state": int(u.deviceBadge.State())}),
		inspectionTarget("pads.grid", "grid", "Sample pads", "group", u.padGridArea, false, nil),
		inspectionTarget("vu.meter", "meter", "Master activity", "status", u.meter, false, nil),

		inspectionTarget("navigation.play", "button", "Choose play rack", "button", u.playMenuBtn, false, rackToggleState(u.playMenuBtn)),
		inspectionTarget("navigation.p6", "button", "Toggle P-6 rack", "button", u.p6Btn, false, rackToggleState(u.p6Btn)),
		inspectionTarget("navigation.fx", "button", "Choose effects rack", "button", u.fxBtn, false, rackToggleState(u.fxBtn)),
		inspectionTarget("navigation.paks", "button", "Toggle sample-paks rack", "button", u.paksBtn, false, rackToggleState(u.paksBtn)),
		inspectionTarget("navigation.vu", "button", "Toggle VU meter", "button", u.meterBtn, false, rackToggleState(u.meterBtn)),
		inspectionTarget("navigation.console", "button", "Toggle console layout", "button", u.consoleBtn, false, rackToggleState(u.consoleBtn)),
	}

	for i, pad := range u.grid.Pads() {
		id := fmt.Sprintf("pads.cell.%02d", i+1)
		targets = append(targets, inspectionTarget(id, "pad", pad.Label(), "button", pad, false, map[string]any{
			"label": pad.Label(), "selected": pad.Selected(), "badges": pad.BadgeCount(),
		}))
	}
	for track, block := range u.seqRack.blocks {
		targets = append(targets, inspectionTarget(
			fmt.Sprintf("sequencer.track.%d", track+1), "track", fmt.Sprintf("Track %d", track+1), "group", block, false,
			map[string]any{"active": track < u.seq.Tracks(), "bars": u.seq.Bars(track)},
		))
		if track < len(u.seqRack.trackBtns) {
			targets = append(targets, inspectionTarget(
				fmt.Sprintf("sequencer.track.%d.assign", track+1), "button", fmt.Sprintf("Assign track %d", track+1), "button", u.seqRack.trackBtns[track], false,
				rackToggleState(u.seqRack.trackBtns[track]),
			))
		}
		for step, cell := range u.seqRack.cells[track] {
			targets = append(targets, inspectionTarget(
				fmt.Sprintf("sequencer.track.%d.step.%d", track+1, step+1), "step", fmt.Sprintf("Track %d step %d", track+1, step+1), "button", cell, false,
				map[string]any{"on": cell.On()},
			))
		}
	}
	return targets
}

func inspectionTarget(id, kind, label, role string, object fyne.CanvasObject, annotate bool, state map[string]any) uiinspect.Target {
	return uiinspect.Target{ID: id, Kind: kind, Label: label, Role: role, Object: object, Annotate: annotate, State: state}
}

func rackToggleState(toggle interface {
	On() bool
	Armed() bool
	Disabled() bool
}) map[string]any {
	return map[string]any{"on": toggle.On(), "armed": toggle.Armed(), "disabled": toggle.Disabled()}
}

func (u *ui) inspectionMetadata(scenario, formFactor string, notes ...string) uiinspect.Metadata {
	backend := "p6"
	if u.useEmu {
		backend = "emulator"
	}
	return uiinspect.Metadata{
		Scenario:   scenario,
		Variant:    u.activeVariant,
		FormFactor: formFactor,
		Backend:    backend,
		State: map[string]any{
			"compact": u.compact, "console": u.fullScreen, "padFloating": u.padFloating,
			"sequencerDocked": u.seqSide,
		},
		Notes: notes,
	}
}
