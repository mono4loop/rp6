//go:build !nojam && !js && !android && !ios

package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mono4loop/rp6/internal/jam"
	jamwebrtc "github.com/mono4loop/rp6/internal/jam/webrtc"
	"github.com/mono4loop/rp6/internal/ui/components"
)

// Shared-jam state. rp6 is a single-window app, so package-level handles keep
// every jam field out of the ui struct — the whole feature lives in this
// build-tagged file plus a few no-op-by-default call sites in main.go. It is
// compiled by default on desktop; -tags nojam (or a web/mobile build) drops it.
var (
	jamEngine *jam.Engine
	jamBtn    *components.RackToggle
	jamPeers  int
	jamSignal string
	jamCode   string
)

const (
	prefJamSignal = "jam.signal"
	prefJamCode   = "jam.code"
)

// jamAccent lights the jam button; green reads as an online/connection cue,
// distinct from the amber section toggles beside it.
var jamAccent = ledGreen

// jamToggles returns the bottom-bar controls contributed by the jam feature —
// here, a single people/account icon toggle. main.go appends these to the
// section-toggle row (the stub build returns none).
func (u *ui) jamToggles() []fyne.CanvasObject {
	jamBtn = components.NewRackToggleIcon(theme.AccountIcon(), jamAccent, u.showJamDialog)
	return []fyne.CanvasObject{jamBtn}
}

// showJamDialog opens the guided connection dialog: a join form when idle, or a
// status/leave panel when a session is active.
func (u *ui) showJamDialog() {
	if jamEngine != nil {
		u.showJamStatus()
		return
	}

	signal := jamSignal
	if signal == "" {
		signal = prefsString(prefJamSignal, "")
	}
	code := jamCode
	if code == "" {
		code = prefsString(prefJamCode, "")
	}
	if code == "" {
		code = jam.NewCode() // offer a ready-to-share random session key by default
	}

	signalEntry := widget.NewEntry()
	signalEntry.SetText(signal)
	signalEntry.SetPlaceHolder("rp6-signal.example.com  (bare host → wss://, or ws://host:1337/)")

	codeEntry := widget.NewEntry()
	codeEntry.SetText(code)
	codeEntry.SetPlaceHolder("shared session code")
	gen := widget.NewButtonWithIcon("Generate", theme.ViewRefreshIcon(), func() {
		codeEntry.SetText(jam.NewCode())
	})

	intro := widget.NewLabel(
		"Jam together over the internet: everyone who joins with the same session " +
			"code hears — and sees a blink for — each other's live pad hits.\n\n" +
			"1. Point everyone at the same signaling server.\n" +
			"2. Share one session code (Generate makes a strong one).\n" +
			"3. Join — the button lights up when peers connect.")
	intro.Wrapping = fyne.TextWrapWord

	form := widget.NewForm(
		widget.NewFormItem("Signaling server", signalEntry),
		widget.NewFormItem("Session code", container.NewBorder(nil, nil, nil, gen, codeEntry)),
	)
	content := container.NewVBox(intro, widget.NewSeparator(), form)

	d := dialog.NewCustomConfirm("JAM — Join a session", "Join", "Cancel", content, func(join bool) {
		if !join {
			return
		}
		s := strings.TrimSpace(signalEntry.Text)
		c := jam.NormalizeCode(codeEntry.Text)
		if s == "" || c == "" {
			u.setStatus("jam: enter a server URL and a session code")
			return
		}
		u.startJamWith(s, c, "")
	}, u.win)
	d.Resize(fyne.NewSize(480, 380))
	d.Show()
}

// showJamStatus shows the active session's details and a Leave action.
func (u *ui) showJamStatus() {
	codeLabel := widget.NewLabelWithStyle(jamCode, fyne.TextAlignLeading, fyne.TextStyle{Bold: true, Monospace: true})

	body := container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("Session code", codeLabel),
			widget.NewFormItem("Signaling server", widget.NewLabel(jamSignal)),
			widget.NewFormItem("Peers connected", widget.NewLabel(fmt.Sprintf("%d", jamPeers))),
		),
		widget.NewLabel("Share the server URL and code above to invite more players."),
	)
	d := dialog.NewCustomConfirm("JAM — Connected", "Leave session", "Close", body, func(leave bool) {
		if leave {
			u.stopJam()
			u.setStatus("jam: left session")
		}
	}, u.win)
	d.Resize(fyne.NewSize(460, 280))
	d.Show()
}

// startJam joins a session from RP6_JAM_CODE at launch, if set (headless / CI /
// power-user path). The UI dialog uses startJamWith directly.
func (u *ui) startJam() {
	code := jam.NormalizeCode(os.Getenv("RP6_JAM_CODE"))
	if code == "" {
		return
	}
	signal := strings.TrimSpace(os.Getenv("RP6_JAM_SIGNAL"))
	if signal == "" {
		signal = prefsString(prefJamSignal, "") // fall back to the last-used server
	}
	if signal == "" {
		log.Printf("rp6/jam: RP6_JAM_CODE set but no signaling server — set RP6_JAM_SIGNAL")
		return
	}
	u.startJamWith(signal, code, strings.TrimSpace(os.Getenv("RP6_JAM_NAME")))
}

// startJamWith joins the given session, wiring remote hits to applyRemotePad and
// the live peer count to the button. Any existing session is left first.
func (u *ui) startJamWith(signal, code, name string) {
	u.stopJam()

	signal = jamwebrtc.NormalizeURL(signal) // bare host -> wss://, etc.
	t, err := jamwebrtc.Dial(jamwebrtc.Config{
		Signaling: signal,
		Room:      code,
		Name:      name,
		OnPeers:   u.setJamPeers,
	})
	if err != nil {
		log.Printf("rp6/jam: %v", err)
		u.setStatus("jam: " + err.Error())
		return
	}
	e := jam.New(t)
	e.OnPad = u.applyRemotePad
	e.Start()

	jamEngine = e
	jamSignal, jamCode = signal, code
	setPref(prefJamSignal, signal)
	setPref(prefJamCode, code)

	if jamBtn != nil {
		jamBtn.SetOn(true) // lit border + icon, like the other section toggles
	}
	log.Printf("rp6/jam: joined session %q via %s", code, signal)
	u.setStatus("jam: session " + code + " (waiting for peers)")
}

// stopJam leaves the jam session and resets the button (idempotent).
func (u *ui) stopJam() {
	if jamEngine != nil {
		_ = jamEngine.Close()
		jamEngine = nil
	}
	jamPeers = 0
	if jamBtn != nil {
		jamBtn.SetOn(false)
	}
}

// setJamPeers reflects the connected-peer count in the status line and keeps the
// button lit (border + icon) while a session is active — the same look as the
// other bottom-bar toggles. Called from the transport's background goroutine, so
// UI work is marshalled.
func (u *ui) setJamPeers(n int) {
	fyne.Do(func() {
		jamPeers = n
		if jamBtn != nil {
			jamBtn.SetOn(true) // stays lit while in the session
		}
		if n > 0 {
			u.setStatus(fmt.Sprintf("jam: %d peer(s) connected", n))
		} else {
			u.setStatus("jam: waiting for peers")
		}
	})
}

// jamBroadcastPad sends a locally-produced live pad hit to jam peers. Called
// from the live pad sources only (screen tap, external controller, hardware pad
// press) — never from the sequencer/effects fire path, and never while applying
// a remote hit — so hits neither echo nor double.
func (u *ui) jamBroadcastPad(id int, velocity uint8) {
	if jamEngine != nil {
		jamEngine.SendPad(id, velocity)
	}
}

// applyRemotePad plays a peer's pad hit locally and blinks the pad — exactly as
// a hit from an external MIDI controller would look — but WITHOUT touching the
// selection, current page, effects rack or status line, so a peer's playing
// never disturbs the local UI. Runs on the jam transport's read goroutine, so
// the visible blink is marshalled through fyne.Do.
func (u *ui) applyRemotePad(id int, velocity uint8) {
	u.firePadVel(id, velocity) // sound locally (concurrency-safe via devMu)
	bank, number := padBankNumber(id)
	page, row, col := u.gridPos(bank, number)
	fyne.Do(func() {
		u.grid.FlashPad(page, row, col) // blink only — no selection/page change
	})
}

// prefsString returns a trimmed app preference, or def if unset.
func prefsString(key, def string) string {
	if a := fyne.CurrentApp(); a != nil {
		if v := strings.TrimSpace(a.Preferences().String(key)); v != "" {
			return v
		}
	}
	return def
}

// setPref persists an app preference.
func setPref(key, val string) {
	if a := fyne.CurrentApp(); a != nil {
		a.Preferences().SetString(key, val)
	}
}
