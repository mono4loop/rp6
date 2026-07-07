package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/mono4loop/rp6/internal/effects"
	"github.com/mono4loop/rp6/internal/sequencer"
	"github.com/mono4loop/rp6/internal/store"
	"github.com/mono4loop/rp6/internal/ui/components"
	"github.com/mono4loop/rp6/p6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncBuf is a concurrency-safe io.Writer for tests that exercise the clock
// goroutine.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.b.Bytes()...)
}

func newTestUI(t *testing.T) *ui {
	t.Helper()
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme()) // real fonts (monospace) for LCD
	u := newUI()
	w := test.NewWindow(nil)
	t.Cleanup(func() { w.Close() })
	u.build(w)
	return u
}

func TestShowPageAssignsBanks(t *testing.T) {
	u := newTestUI(t)
	g := u.grid
	last := len(g.Pads()) - 1

	g.ShowPage(0) // banks A-D
	assert.Equal(t, "A1", g.Pads()[0].Label())
	assert.Equal(t, "D6", g.Pads()[last].Label())

	g.ShowPage(1) // banks E-H
	assert.Equal(t, "E1", g.Pads()[0].Label())
	assert.Equal(t, "H6", g.Pads()[last].Label())
}

func TestPadTapSelectsAndTriggers(t *testing.T) {
	u := newTestUI(t)
	var buf bytes.Buffer
	u.dev = p6.New(&buf, p6.DefaultConfig())

	u.grid.ShowPage(1)          // E-H, so pads[0] is E1 -> note 72
	u.grid.Pads()[0].OnTapped() // simulate a tap

	require.Equal(t, []byte{0x9A, 72, p6.DefaultVelocity}, buf.Bytes())
	assert.Contains(t, u.status.Text, "E1")
	assert.True(t, u.grid.Pads()[0].Selected(), "tapped pad becomes selected")
}

func TestTriggerWithoutDeviceIsSafe(t *testing.T) {
	u := newTestUI(t)
	u.dev = nil

	u.onPadTrigger(0, 1) // must not panic

	assert.Contains(t, u.status.Text, "not connected")
}

func TestPatternNavSendsProgramChange(t *testing.T) {
	u := newTestUI(t)
	var buf bytes.Buffer
	u.dev = p6.New(&buf, p6.DefaultConfig())

	u.patternStep.Increment() // 0 -> 1
	u.patternStep.Increment() // 1 -> 2
	u.patternStep.Decrement() // 2 -> 1

	// Program change on the Program channel (16 -> status 0xCF).
	assert.Equal(t, []byte{0xCF, 1, 0xCF, 2, 0xCF, 1}, buf.Bytes())
	assert.Equal(t, 1, u.patternStep.Value())
	assert.Equal(t, "1-02", patternName(u.patternStep.Value()))
}

func TestPatternClampsAtEdges(t *testing.T) {
	u := newTestUI(t)
	var buf bytes.Buffer
	u.dev = p6.New(&buf, p6.DefaultConfig())

	u.patternStep.Decrement() // already 0, stays 0 (no change, no PC)
	assert.Equal(t, 0, u.patternStep.Value())
	assert.Empty(t, buf.Bytes())

	u.patternStep.SetValue(100) // clamps to 63
	assert.Equal(t, 63, u.patternStep.Value())
	assert.Equal(t, "4-16", patternName(u.patternStep.Value()))
}

func TestTransportButtons(t *testing.T) {
	u := newTestUI(t)
	sb := &syncBuf{}
	u.dev = p6.New(sb, p6.DefaultConfig())
	u.clock = p6.NewClocker(u.dev, 120)

	u.play()
	assert.True(t, u.clock.Running())
	time.Sleep(10 * time.Millisecond)
	u.stop()
	assert.False(t, u.clock.Running())

	b := sb.Bytes()
	require.NotEmpty(t, b)
	assert.Equal(t, byte(0xFA), b[0], "play sends MIDI Start first")
	assert.Contains(t, b, byte(0xFC), "stop sends MIDI Stop")
}

func TestTempoStepperClampsAndUpdatesBPM(t *testing.T) {
	u := newTestUI(t)

	u.tempo.SetValue(1000)
	assert.Equal(t, 300, u.tempo.Value())
	assert.Equal(t, float64(300), u.bpm)

	u.tempo.SetValue(1)
	assert.Equal(t, 40, u.tempo.Value())
	assert.Equal(t, float64(40), u.bpm)
}

func TestParsePattern(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"1-01", 0, true},
		{"2-05", 20, true},
		{"4-16", 63, true},
		{"1", 0, true},
		{"64", 63, true},
		{"101", 0, true},  // concatenated bank+slot
		{"205", 20, true}, // 2-05
		{"416", 63, true}, // 4-16
		{"0", 0, false},
		{"65", 0, false},
		{"5-01", 0, false},
		{"1-17", 0, false},
		{"500", 0, false}, // bank 5 invalid
		{"120", 0, false}, // slot 20 invalid
		{"junk", 0, false},
	}
	for _, c := range cases {
		got, ok := parsePattern(c.in)
		assert.Equal(t, c.ok, ok, "input %q", c.in)
		if c.ok {
			assert.Equal(t, c.want, got, "input %q", c.in)
		}
	}
}

func TestParseTempo(t *testing.T) {
	v, ok := parseTempo("140")
	assert.True(t, ok)
	assert.Equal(t, 140, v)

	v, ok = parseTempo("128 BPM")
	assert.True(t, ok)
	assert.Equal(t, 128, v)

	_, ok = parseTempo("fast")
	assert.False(t, ok)
}

func TestFXSliderSendsCC(t *testing.T) {
	u := newTestUI(t)
	var buf bytes.Buffer
	u.dev = p6.New(&buf, p6.DefaultConfig())

	u.sendFX("Delay Time", p6.CCDelayTime, 64)

	// Sent on the Auto channel (15 -> status 0xBE).
	assert.Equal(t, []byte{0xBE, p6.CCDelayTime, 64}, buf.Bytes())
	assert.Contains(t, u.status.Text, "Delay Time = 64")
}

func TestPatternName(t *testing.T) {
	assert.Equal(t, "1-01", patternName(0))
	assert.Equal(t, "1-16", patternName(15))
	assert.Equal(t, "2-01", patternName(16))
	assert.Equal(t, "4-16", patternName(63))
}

func TestToggleMeter(t *testing.T) {
	u := newTestUI(t)
	assert.True(t, u.meterArea.Visible(), "meter shown by default")

	u.toggleVisible(u.meterArea, u.meterBtn)
	assert.False(t, u.meterArea.Visible())

	u.toggleVisible(u.meterArea, u.meterBtn)
	assert.True(t, u.meterArea.Visible())
}

func TestSequencerRackDefaultsVisible(t *testing.T) {
	u := newTestUI(t)
	assert.True(t, u.seqRack.Object().Visible(), "sequencer shown by default")
	assert.False(t, u.fxRack.Object().Visible(), "effects hidden by default")
	assert.False(t, u.dlyRevObj.Visible(), "delay/reverb hidden by default")

	u.toggleVisible(u.seqRack.Object(), u.seqBtn)
	assert.False(t, u.seqRack.Object().Visible(), "toggles off")

	// Default track count is 4.
	assert.Equal(t, defaultTracks, u.seq.Tracks())
}

func TestArmedTrackMuteAndBars(t *testing.T) {
	u := newTestUI(t)
	mb := u.seqRack.armMuteBtn
	bb := u.seqRack.armBarsBtn

	// Nothing armed: the second-row controls are greyed and inert.
	assert.False(t, mb.On(), "mute control greyed when nothing armed")
	mb.Tapped(nil)
	assert.False(t, u.seq.Muted(0), "mute does nothing with no armed track")

	// Arm track 0, then the mute control acts on it.
	u.seqRack.trackBtns[0].Tapped(nil)
	assert.Equal(t, 0, u.seqRack.armedTrack)
	assert.True(t, mb.On(), "armed active track lights the mute control")

	mb.Tapped(nil)
	assert.True(t, u.seq.Muted(0), "mute control mutes the armed track")
	assert.False(t, mb.On(), "muted -> greyed")
	mb.Tapped(nil)
	assert.False(t, u.seq.Muted(0), "mute control unmutes the armed track")

	// The bars control expands the armed track's length.
	assert.Equal(t, 1, u.seq.Bars(0))
	bb.Tapped(nil)
	assert.Equal(t, 2, u.seq.Bars(0), "bars control expands the armed track")
}

func TestTrackButtonArmsThenPadAssigns(t *testing.T) {
	u := newTestUI(t)
	tb := u.seqRack.trackBtns[1]

	// Tapping the track button arms it (lit hardest) — it does NOT read the
	// current selection yet.
	tb.Tapped(nil)
	assert.True(t, tb.Armed(), "track button arms on tap")
	assert.Equal(t, 1, u.seqRack.armedTrack)

	// Selecting a pad now assigns it to the armed track and disarms.
	want := padID(2, 3) // bank C, pad 3
	u.onPadTrigger(2, 3)
	assert.Equal(t, want, u.seq.Pad(1), "armed track adopts the selected pad")
	assert.False(t, tb.Armed(), "track disarms after assignment")
	assert.Equal(t, -1, u.seqRack.armedTrack)

	// A later pad hit must NOT change the sample (arming already consumed).
	u.onPadTrigger(0, 1)
	assert.Equal(t, want, u.seq.Pad(1), "accidental pad hit can't change the sample")
}

func TestTrackButtonArmToggleCancels(t *testing.T) {
	u := newTestUI(t)
	tb := u.seqRack.trackBtns[0]
	before := u.seq.Pad(0)

	tb.Tapped(nil) // arm
	assert.True(t, tb.Armed())
	tb.Tapped(nil) // tap again cancels
	assert.False(t, tb.Armed())
	assert.Equal(t, -1, u.seqRack.armedTrack)

	// A pad selection now does nothing to the track (nothing armed).
	u.onPadTrigger(1, 2)
	assert.Equal(t, before, u.seq.Pad(0), "no armed track means no reassignment")
}

func TestBumpMeter(t *testing.T) {
	u := newTestUI(t)
	u.onPadTrigger(0, 1) // triggers a pad -> bumps the activity meter
	assert.Greater(t, u.activity.level(), 0.0)
}

func TestSetConnectedTogglesLED(t *testing.T) {
	u := newTestUI(t)
	u.setConnected(true)
	assert.Equal(t, ledGreen, u.statusLED.Color())
	assert.Equal(t, components.DeviceOnline, u.deviceBadge.State())
	u.setConnected(false)
	assert.Equal(t, ledRed, u.statusLED.Color())
	assert.Equal(t, components.DeviceOffline, u.deviceBadge.State())
}

func TestDeviceIdentity(t *testing.T) {
	u := newTestUI(t)
	name, tag, accent := u.deviceIdentity()
	assert.Equal(t, "P-6", name)
	assert.Equal(t, "USB MIDI", tag)
	assert.Equal(t, deviceHwAccent, accent)

	u.useEmu = true
	name, tag, accent = u.deviceIdentity()
	assert.Equal(t, "EMULATOR", name)
	assert.Equal(t, "SOFTWARE", tag)
	assert.Equal(t, deviceEmuAccent, accent)
}

// errWriter is an io.Writer that fails every write, simulating a P-6 that was
// unplugged (writes to the closed MIDI node return an error).
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write: no such device") }

func TestDeviceFailedFallsBackToEmu(t *testing.T) {
	u := newTestUI(t)
	u.useEmu = false
	u.dev = p6.New(errWriter{}, p6.DefaultConfig())
	u.setConnected(true)
	require.Equal(t, components.DeviceOnline, u.deviceBadge.State())
	t.Cleanup(func() {
		if u.dev != nil {
			_ = u.dev.Close()
		}
	})

	// A pad fire whose MIDI write fails means the P-6 vanished: the app must
	// auto-switch to the emulator (built-in kit) and come back online.
	// deviceFailed marshals through fyne.Do, which the test driver runs inline.
	u.firePadVel(padID(0, 1), p6.DefaultVelocity)

	assert.True(t, u.useEmu, "a P-6 disconnect falls back to the emulator")
	assert.Equal(t, components.DeviceOnline, u.deviceBadge.State(), "emulator connects online")
	name, _, _ := u.deviceIdentity()
	assert.Equal(t, "EMULATOR", name)
}

func TestConnectErrorMessage(t *testing.T) {
	u := newTestUI(t)
	assert.Contains(t, u.connectErrorMessage(p6.ErrBusy), "busy")
	assert.Contains(t, u.connectErrorMessage(p6.ErrPermission), "permission")
	assert.Contains(t, u.connectErrorMessage(p6.ErrNotFound), "not found")

	u.useEmu = true
	u.emuDir = "/tmp/kit"
	assert.Contains(t, u.connectErrorMessage(errors.New("bad wav")), "emulator")
}

func TestToggleBackendToEmuLoadsBuiltinKit(t *testing.T) {
	u := newTestUI(t)

	// On P-6 with no samples dir: switching to the emulator loads the built-in
	// "modular-hits" kit (silent sink under test) and comes online.
	u.useEmu = false
	u.emuDir = ""
	u.toggleBackend()
	t.Cleanup(func() {
		if u.dev != nil {
			_ = u.dev.Close()
		}
	})

	assert.True(t, u.useEmu, "must switch to the emulator")
	assert.Equal(t, components.DeviceOnline, u.deviceBadge.State(), "built-in kit connects online")
	name, _, accent := u.deviceIdentity()
	assert.Equal(t, "EMULATOR", name)
	assert.Equal(t, deviceEmuAccent, accent)
}

func TestEffectsRollViaRack(t *testing.T) {
	u := newTestUI(t)
	u.fx.SetTempo(6000) // fast so the roll ticks quickly if it runs

	u.grid.ShowPage(0)
	u.grid.Pads()[0].OnTapped() // select A1 (one-shot; no effect yet)
	require.Equal(t, padID(0, 1), u.selPad)
	assert.False(t, u.fx.IsRolling(u.selPad))

	u.fxRack.cycleSlot(0) // assign Roll to slot 0 of A1
	assert.True(t, u.fx.HasEffect(u.selPad, effects.KindRoll))
	assert.Equal(t, 1, u.grid.Pads()[0].BadgeCount(), "roll badge appears on the pad")

	u.grid.Pads()[0].OnTapped() // now toggles rolling on
	assert.True(t, u.fx.IsRolling(u.selPad))

	u.fx.StopAll()
	assert.False(t, u.fx.IsRolling(u.selPad))
}

func TestSeqDockToggle(t *testing.T) {
	u := newTestUI(t)
	assert.False(t, u.seqSide)

	u.onSeqDock(true) // dock to the side column
	assert.True(t, u.seqSide)
	assert.True(t, u.seqRack.Object().Visible(), "docking reveals the sequencer")

	u.onSeqDock(false)
	assert.False(t, u.seqSide)
}

func TestTogglePads(t *testing.T) {
	u := newTestUI(t)
	assert.True(t, u.padRackObj.Visible(), "pads shown by default")
	u.togglePads()
	assert.False(t, u.padRackObj.Visible())
	u.togglePads()
	assert.True(t, u.padRackObj.Visible())
}

func TestDensityToggle(t *testing.T) {
	u := newTestUI(t)
	assert.False(t, u.dense, "paged by default")
	assert.Len(t, u.grid.Pads(), 24, "4 banks x 6 pads per page")

	u.toggleDensity()
	assert.True(t, u.dense)
	assert.Len(t, u.grid.Pads(), 48, "all 8 banks x 6 pads on one page")
	assert.Same(t, u.grid.Object(), u.padGridArea.Objects[0], "grid swapped into the holder")

	u.toggleDensity()
	assert.False(t, u.dense)
	assert.Len(t, u.grid.Pads(), 24)
}

func TestMIDIListenToggle(t *testing.T) {
	u := newTestUI(t)
	assert.False(t, u.listenMIDI.Load(), "not listening by default")

	u.toggleMIDIListen()
	assert.True(t, u.listenMIDI.Load())

	u.toggleMIDIListen()
	assert.False(t, u.listenMIDI.Load())
}

func TestListenDefaultFollowsBackend(t *testing.T) {
	u := newTestUI(t)

	// On the P-6, hardware-input reflection is on by default (so pad presses
	// show without tapping the eye toggle); the eye toggle reflects that.
	u.useEmu = false
	u.setListenDefault()
	assert.True(t, u.listenMIDI.Load(), "listening enabled for the P-6")
	assert.True(t, u.midiInBtn.On(), "eye toggle lit for the P-6")

	// The emulator has no MIDI input, so listening is off.
	u.useEmu = true
	u.setListenDefault()
	assert.False(t, u.listenMIDI.Load(), "listening disabled for the emulator")
	assert.False(t, u.midiInBtn.On(), "eye toggle off for the emulator")
}

// TestHardwareReflectWhileFloated guards that incoming pad presses are reflected
// in the grid even when the pad rack is floated in its own window.
func TestHardwareReflectWhileFloated(t *testing.T) {
	u := newTestUI(t)
	u.dev = p6.New(&bytes.Buffer{}, p6.DefaultConfig())
	u.listenMIDI.Store(true)
	u.togglePadFloat()
	require.True(t, u.padFloating)

	// NoteOn ch11, note 48 == A1.
	u.onMIDIIn(u.dev, p6.Event{Type: p6.EventNoteOn, Channel: p6.DefaultSamplerChannel, Data1: 48, Data2: 100})
	assert.Equal(t, padID(0, 1), u.selPad)
	assert.True(t, u.grid.Pads()[0].Selected(), "A1 highlighted from hardware while floated")
}

func TestPadFloatToggle(t *testing.T) {
	u := newTestUI(t)
	assert.False(t, u.padFloating)
	assert.Nil(t, u.padWin)

	u.togglePadFloat() // pop out to its own window
	assert.True(t, u.padFloating)
	assert.NotNil(t, u.padWin)

	u.togglePadFloat() // dock back
	assert.False(t, u.padFloating)
	assert.Nil(t, u.padWin)
}

func TestSequencePersistence(t *testing.T) {
	u := newTestUI(t)
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "test")
	require.NoError(t, err)
	u.store = s
	t.Cleanup(func() { s.Close() })

	// Program a step on slot 1 and save it.
	u.seqSlot = 1
	u.seq.SetStep(0, 3, true)
	u.seqRack.SetSeqName("beat")
	u.persistSeq()

	// Mutate the engine, then reload slot 1 — original state comes back.
	u.seq.SetStep(0, 3, false)
	u.seq.SetStep(2, 7, true)
	u.loadSlot(1)
	assert.True(t, u.seq.Step(0, 3))
	assert.False(t, u.seq.Step(2, 7))
	assert.Equal(t, "beat", u.seqRack.SeqName())

	// An empty slot loads a fresh sequence.
	u.loadSlot(2)
	assert.False(t, u.seq.Step(0, 3))
	assert.Equal(t, "", u.seqRack.SeqName())
}

// TestSeqCopyToSlot verifies Ctrl-click duplication: the current sequence is
// copied into the new slot while the source slot keeps its own content.
func TestSeqCopyToSlot(t *testing.T) {
	u := newTestUI(t)
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "test")
	require.NoError(t, err)
	u.store = s
	t.Cleanup(func() { s.Close() })

	u.loadSlot(1)
	u.seqRack.tracksStep.SetValue(6)
	u.seq.SetStep(5, 4, true)

	u.copyToSlot(2) // duplicate slot 1 -> slot 2
	assert.Equal(t, 2, u.seqSlot)
	assert.Equal(t, 6, u.seq.Tracks(), "copy keeps track count")
	assert.True(t, u.seq.Step(5, 4), "copy keeps programmed steps")

	// The source slot still holds its own copy.
	u.loadSlot(1)
	assert.Equal(t, 6, u.seq.Tracks())
	assert.True(t, u.seq.Step(5, 4))
}

// TestSeqCopyInsertsShiftsRight verifies that copying into an occupied slot
// shifts the existing sequences right instead of overwriting them.
func TestSeqCopyInsertsShiftsRight(t *testing.T) {
	u := newTestUI(t)
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "test")
	require.NoError(t, err)
	u.store = s
	t.Cleanup(func() { s.Close() })

	// Slot 1: a step on track 0. Slot 2: a distinct step on track 1.
	u.loadSlot(1)
	u.seq.SetStep(0, 0, true)
	u.persistSeq()
	u.loadSlot(2)
	u.seq.SetStep(1, 5, true)
	u.persistSeq()

	// Back to slot 1 and copy into slot 2 — the old slot-2 sequence must move
	// to slot 3, not be overwritten.
	u.loadSlot(1)
	u.copyToSlot(2)
	assert.Equal(t, 2, u.seqSlot)
	assert.True(t, u.seq.Step(0, 0), "the copy (from slot 1) now lives in slot 2")
	assert.False(t, u.seq.Step(1, 5))

	u.loadSlot(3) // the original slot-2 sequence shifted here
	assert.True(t, u.seq.Step(1, 5), "old slot 2 shifted to slot 3")
	assert.False(t, u.seq.Step(0, 0))
}

// TestSeqSlotChangeQuantized verifies that while the sequencer plays a slot
// change is queued and applied only on a bar boundary; when stopped it loads
// immediately.
func TestSeqSlotChangeQuantized(t *testing.T) {
	u := newTestUI(t)
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "test")
	require.NoError(t, err)
	u.store = s
	t.Cleanup(func() { s.Close() })

	// Prepare two distinct slots.
	u.loadSlot(1)
	u.seq.SetStep(0, 0, true)
	u.persistSeq()
	u.loadSlot(2)
	u.seq.SetStep(1, 3, true)
	u.persistSeq()
	u.loadSlot(1)
	require.Equal(t, 1, u.seqSlot)

	// Stopped: selecting applies immediately.
	u.selectSlot(2)
	assert.Equal(t, 2, u.seqSlot)
	u.loadSlot(1)

	// Playing: the change is queued, not applied yet. Silence the step callback
	// so the clock goroutine doesn't race us applying the pending change; we
	// drive maybeApplyPendingAt manually instead.
	u.seq.OnStep = nil
	u.seq.Start()
	t.Cleanup(u.seq.Stop)
	u.selectSlot(2)
	assert.Equal(t, 2, u.pendingSlot)
	assert.Equal(t, 1, u.seqSlot, "not switched mid-bar")

	// A non-boundary tick keeps it queued; a bar boundary applies it.
	u.maybeApplyPendingAt(sequencer.StepsPerBar - 1)
	assert.Equal(t, 1, u.seqSlot)
	u.maybeApplyPendingAt(sequencer.StepsPerBar)
	assert.Equal(t, 2, u.seqSlot, "applied on the bar")
	assert.Equal(t, 0, u.pendingSlot)
}

// TestSeqStopAppliesPending verifies a queued slot change is applied when the
// sequencer is stopped, keeping the readout and loaded sequence consistent.
func TestSeqStopAppliesPending(t *testing.T) {
	u := newTestUI(t)
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "test")
	require.NoError(t, err)
	u.store = s
	t.Cleanup(func() { s.Close() })

	u.loadSlot(1)
	u.pendingSlot = 4
	u.applyPendingSlot()
	assert.Equal(t, 4, u.seqSlot)
	assert.Equal(t, 0, u.pendingSlot)
}

// TestSeqDeleteShiftsLeft verifies Ctrl-click Clear deletes the current
// sequence and shifts the following ones left to close the gap.
func TestSeqDeleteShiftsLeft(t *testing.T) {
	u := newTestUI(t)
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "test")
	require.NoError(t, err)
	u.store = s
	t.Cleanup(func() { s.Close() })

	// Slot 1 and 2 hold distinct sequences.
	u.loadSlot(1)
	u.seq.SetStep(0, 0, true)
	u.persistSeq()
	u.loadSlot(2)
	u.seq.SetStep(1, 3, true)
	u.persistSeq()

	// On slot 1, delete it — slot 2's sequence shifts into slot 1.
	u.loadSlot(1)
	u.deleteSlot()
	assert.Equal(t, 1, u.seqSlot)
	assert.True(t, u.seq.Step(1, 3), "old slot 2 shifted into slot 1")
	assert.False(t, u.seq.Step(0, 0), "deleted sequence is gone")

	// Slot 2 is now empty.
	u.loadSlot(2)
	assert.False(t, u.seq.Step(1, 3))
	assert.False(t, u.seq.Step(0, 0))
}

// TestSeqSwitchAutosaves verifies that switching slots persists the working
// slot's in-progress edits (track count + steps) without an explicit Save.
func TestSeqSwitchAutosaves(t *testing.T) {
	u := newTestUI(t)
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"), "test")
	require.NoError(t, err)
	u.store = s
	t.Cleanup(func() { s.Close() })

	u.loadSlot(1)
	// Grow to 6 tracks and program a step on the 6th track — but do NOT Save.
	u.seqRack.tracksStep.SetValue(6)
	u.seq.SetStep(5, 2, true)
	require.Equal(t, 6, u.seq.Tracks())

	u.loadSlot(2) // navigate away (should autosave slot 1)
	u.loadSlot(1) // and back

	assert.Equal(t, 6, u.seq.Tracks(), "extra tracks survive slot navigation")
	assert.True(t, u.seq.Step(5, 2), "programmed step survives slot navigation")
}

// TestSeqCopyAtLastSlotIsNoop verifies copyCurrent does nothing when the SEQ
// knob is already on the last slot: there is no slot after it to duplicate
// into, so onCopy must not be invoked (copying onto the current slot is a
// silent no-op or a misleading "no free slot" error).
func TestSeqCopyAtLastSlotIsNoop(t *testing.T) {
	const maxSlots = 16
	seq := sequencer.New(8, 4, func(int, uint8) {})
	var copied []int
	r := newSequencerRack(seq, func() {}, func(bool) {}, maxSlots,
		func(int) {}, func(slot int) { copied = append(copied, slot) },
		func() {}, func() {})

	// Copying from a middle slot targets the next slot.
	r.SetSlot(3)
	r.copyCurrent()
	require.Equal(t, []int{4}, copied)

	// Copying from the last slot must be a no-op (nowhere to copy into).
	r.SetSlot(maxSlots)
	r.copyCurrent()
	assert.Equal(t, []int{4}, copied, "copy at last slot should not fire onCopy")
}

// TestEmuDirRememberedAcrossRestart covers the app-preferences persistence that
// lets a runtime-picked emulator pak survive a restart (vxrv): rememberEmuDir
// stores the directory and savedEmuDir returns it, but a stale pointer (moved/
// deleted pak, or a non-directory) is rejected so launch falls back to the
// built-in kit instead of failing to open a missing directory.
func TestEmuDirRememberedAcrossRestart(t *testing.T) {
	u := newTestUI(t)
	dir := t.TempDir()

	// Nothing remembered yet.
	assert.Equal(t, "", u.savedEmuDir())

	// An existing pak directory round-trips (restored on the next launch).
	u.rememberEmuDir(dir)
	assert.Equal(t, dir, u.savedEmuDir())

	// A stale pointer (pak moved/deleted) is ignored.
	u.rememberEmuDir(filepath.Join(dir, "gone"))
	assert.Equal(t, "", u.savedEmuDir())

	// A regular file (not a directory) is likewise rejected.
	f := filepath.Join(dir, "f.wav")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
	u.rememberEmuDir(f)
	assert.Equal(t, "", u.savedEmuDir())
}

// TestDeviceFailedLogsError verifies the real disconnect reason is logged (once
// per connection) so field reports have something to diagnose, rather than the
// error being silently discarded (a673).
func TestDeviceFailedLogsError(t *testing.T) {
	u := newTestUI(t)
	u.useEmu = true // avoid the hardware->emulator fallback path

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	gen := u.devGen.Load()
	u.deviceFailed(gen, errors.New("broken pipe"))
	assert.Contains(t, buf.String(), "broken pipe")

	// A second failure on the same connection is deduped (no repeat log).
	buf.Reset()
	u.deviceFailed(gen, errors.New("broken pipe"))
	assert.NotContains(t, buf.String(), "broken pipe")
}
