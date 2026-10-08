//go:build !autopilot

package main

import "fyne.io/fyne/v2"

// autopilotKey binds nothing outside autopilot builds (see autopilot_keys.go).
func (u *ui) autopilotKey(fyne.KeyName) {}
