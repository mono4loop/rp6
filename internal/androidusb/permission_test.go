package androidusb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// scan runs one complete scan in which every listed device is ungranted, and
// returns the devices it asked for and the hints it logged.
func scan(a *permAsker, devs ...string) (asked, hints []string) {
	a.beginScan()
	for _, id := range devs {
		ask, hint := a.ungranted(id, id, id == "P-6")
		if ask {
			asked = append(asked, id)
		}
		if hint != "" {
			hints = append(hints, hint)
		}
	}
	a.endScan(true)
	return asked, hints
}

func TestPermAskerAsksOncePerAttach(t *testing.T) {
	var a permAsker
	asked, _ := scan(&a, "P-6")
	assert.Equal(t, []string{"P-6"}, asked)

	// Refused (Cancel, or OK overridden by Microphone access being off): later
	// scans must not ask again — that was the 2 s re-prompt loop of issue #2.
	var hints []string
	for range 10 {
		asked, h := scan(&a, "P-6")
		assert.Empty(t, asked)
		hints = append(hints, h...)
	}
	assert.Equal(t, []string{"rp6usb: P-6 needs Microphone access on (or replug)"}, hints,
		"the hint is logged exactly once")
}

func TestPermAskerHintTiming(t *testing.T) {
	var a permAsker
	scan(&a, "P-6")
	for range hintAfterScans - 1 {
		_, hints := scan(&a, "P-6")
		assert.Empty(t, hints, "the dialog may still be up")
	}
	_, hints := scan(&a, "P-6")
	assert.Len(t, hints, 1)
}

func TestPermAskerHintWithoutAudio(t *testing.T) {
	var a permAsker
	scan(&a, "MacroPad")
	var hints []string
	for range hintAfterScans {
		_, h := scan(&a, "MacroPad")
		hints = append(hints, h...)
	}
	assert.Equal(t, []string{"rp6usb: MacroPad not allowed: replug to ask again"}, hints)
}

func TestPermAskerReplugAsksAgain(t *testing.T) {
	var a permAsker
	scan(&a, "P-6")
	scan(&a, "P-6")

	asked, _ := scan(&a) // unplugged: a complete scan no longer sees it
	assert.Empty(t, asked)

	asked, _ = scan(&a, "P-6")
	assert.Equal(t, []string{"P-6"}, asked)
}

func TestPermAskerIncompleteScanKeepsRequests(t *testing.T) {
	var a permAsker
	scan(&a, "P-6")

	// A scan that stops to read another device never visits the P-6, but the
	// P-6 is still attached: its request must survive.
	a.beginScan()
	a.endScan(false)

	asked, _ := scan(&a, "P-6")
	assert.Empty(t, asked)
}

func TestPermAskerGrantThenRevokeAsksOnceMore(t *testing.T) {
	var a permAsker
	scan(&a, "P-6")
	a.granted("P-6") // OK tapped: rp6 reads it

	// Permission lost mid-session (e.g. Microphone access turned off): ask
	// once more, then stop.
	asked, _ := scan(&a, "P-6")
	assert.Equal(t, []string{"P-6"}, asked)
	asked, _ = scan(&a, "P-6")
	assert.Empty(t, asked)
}

func TestPermAskerOneDialogAtATime(t *testing.T) {
	var a permAsker
	asked, _ := scan(&a, "P-6", "MacroPad")
	assert.Equal(t, []string{"P-6"}, asked, "only one request per scan")

	// The MacroPad waits until the P-6's dialog can no longer be on screen.
	for range hintAfterScans - 1 {
		asked, _ = scan(&a, "P-6", "MacroPad")
		assert.Empty(t, asked)
	}
	asked, _ = scan(&a, "P-6", "MacroPad")
	assert.Equal(t, []string{"MacroPad"}, asked)
}
