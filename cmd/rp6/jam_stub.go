//go:build nojam || js || android || ios

package main

import "fyne.io/fyne/v2"

// Shared jam sessions are compiled in by default on desktop builds. These no-op
// stand-ins replace them when jam is disabled (-tags nojam) or on targets where
// the pion/webrtc dependency doesn't apply (web/wasm and mobile), keeping the
// handful of call sites in main.go valid and those builds pion-free.

func (u *ui) startJam() {}
func (u *ui) stopJam()  {}

func (u *ui) jamBroadcastPad(id int, velocity uint8) {}

// jamToggles contributes no bottom-bar controls when jam is not compiled in.
func (u *ui) jamToggles() []fyne.CanvasObject { return nil }
