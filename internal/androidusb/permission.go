package androidusb

import "sync"

// hintAfterScans is how many later scans (2 s apart) may find a device still
// ungranted after its permission request before we assume the dialog was
// answered and say why access may be refused. Until then a dialog may still be
// on screen, so no other device is asked either: Android delivers a second
// request to the dialog already showing, which drops it.
const hintAfterScans = 3

// permAsker decides when to ask Android for USB permission. A request has no
// memory on Android's side, and hasPermission can stay false even after the
// user taps OK: Android refuses any device with audio capture (a P-6) while the
// Microphone access privacy toggle is off. Asking on every scan therefore
// re-prompted every 2 s, forever (issue #2). permAsker asks once per attach
// instead: a request is forgotten once the device is granted or a full scan no
// longer sees it, so replugging asks again. Calls come from the USB scan thread.
type permAsker struct {
	mu   sync.Mutex
	reqs map[string]*permRequest // device name -> outstanding request
}

type permRequest struct {
	scans int  // later scans that found the device still ungranted
	seen  bool // visited by the current scan
}

// beginScan starts a scan of the attached devices.
func (a *permAsker) beginScan() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.reqs {
		r.seen = false
	}
}

// ungranted reports a MIDI device the scan found without permission. It
// returns ask=true when the caller should request permission now, and a
// non-empty hint (once per request) when the device is still refused
// hintAfterScans scans later. audio marks a device that can capture audio,
// which Android refuses while Microphone access is off.
func (a *permAsker) ungranted(id, name string, audio bool) (ask bool, hint string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r, ok := a.reqs[id]; ok {
		r.seen = true
		r.scans++
		if r.scans == hintAfterScans {
			return false, permissionHint(name, audio)
		}
		return false, ""
	}
	for _, r := range a.reqs {
		if r.scans < hintAfterScans {
			return false, "" // another device's dialog may still be up
		}
	}
	if a.reqs == nil {
		a.reqs = map[string]*permRequest{}
	}
	a.reqs[id] = &permRequest{seen: true}
	return true, ""
}

// granted forgets a device's request once it has permission, so losing that
// permission later asks again (once).
func (a *permAsker) granted(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.reqs, id)
}

// endScan finishes a scan. A complete scan visited every attached device, so
// requests it didn't see belong to unplugged devices and are dropped. A scan
// cut short (it stopped to read a device) leaves them alone.
func (a *permAsker) endScan(complete bool) {
	if !complete {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, r := range a.reqs {
		if !r.seen {
			delete(a.reqs, id)
		}
	}
}

// permissionHint is the status line for a device still refused after its
// request. We can't tell Cancel from an OK that Android overrode, so an audio
// device gets both ways out. Keep it short: the phone's status bar shows ~55
// characters.
func permissionHint(name string, audio bool) string {
	if audio {
		return "rp6usb: " + name + " needs Microphone access on (or replug)"
	}
	return "rp6usb: " + name + " not allowed: replug to ask again"
}
