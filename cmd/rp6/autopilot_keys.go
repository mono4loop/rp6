//go:build autopilot

package main

import "fyne.io/fyne/v2"

// autopilotKey handles the keys only autopilot builds bind, for scripted live
// checks. F9 toggles maximize through GLFW: the same compositor request a
// title-bar double-click makes, which a script can't reach (libdecor draws the
// title bar). Autopilot can't send modifiers, so it's a bare key. See
// docs/autopilot/maximize.txt and scripts/smoke-maximize.sh.
func (u *ui) autopilotKey(name fyne.KeyName) {
	if name != fyne.KeyF9 || u.native == nil {
		return
	}
	if u.native.Maximized() {
		u.native.Restore()
	} else {
		u.native.Maximize()
	}
}
