package main

import (
	"strings"

	"fyne.io/fyne/v2"

	"code.rbel.co/rubiojr/fade/autopilot"
)

// attachAutopilot lets a script drive the main window, for demos, screen
// recordings and on-device smoke checks (see docs/autopilot.md). It does
// nothing unless rp6 is built with -tags autopilot and FADE_AUTOPILOT names a
// script (on Android, autopilot.txt in the app's shared-storage folder).
// Call it once the window has its content, before the app runs.
func (u *ui) attachAutopilot() {
	autopilot.Attach(u.win, autopilot.Options{Named: u.autopilotNamed, Idle: u.autopilotIdle})
}

// autopilotNamed resolves a script name to the object of the inspection
// target with that ID. Script names can't contain dots, so they spell the ID
// with underscores: sequencer_track_1_step_1 is sequencer.track.1.step.1.
// It runs on the Fyne goroutine.
func (u *ui) autopilotNamed(name string) fyne.CanvasObject {
	id := strings.ReplaceAll(name, "_", ".")
	for _, t := range u.inspectionTargets() {
		if t.ID == id {
			return t.Object
		}
	}
	return nil
}

// autopilotIdle reports whether rp6 has settled for "wait idle": no sample
// pak loading in the background (setEmuSamples swaps it in, and stops the
// transport, when it finishes) and no relayout queued. Device connects need
// no check: they run on the Fyne goroutine, as this does, so they're over by
// the time it runs.
func (u *ui) autopilotIdle() bool {
	return !u.loadingSamples.Load() && len(u.relayoutReq) == 0
}
