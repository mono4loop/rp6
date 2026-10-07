package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLifecycle captures the hooks an app registers (fyne.Lifecycle has no
// public getters, so the test invokes the captured stop hook itself).
type fakeLifecycle struct {
	foreground, background, started, stopped func()
}

func (l *fakeLifecycle) SetOnEnteredForeground(f func()) { l.foreground = f }
func (l *fakeLifecycle) SetOnExitedForeground(f func())  { l.background = f }
func (l *fakeLifecycle) SetOnStarted(f func())           { l.started = f }
func (l *fakeLifecycle) SetOnStopped(f func())           { l.stopped = f }

// TestCloseIsIdempotent: close() runs its shutdown once. The interactive exit
// paths call it and then quit, which fires the lifecycle stop hook that calls
// it again; the second call must be a no-op rather than, e.g., closing the
// relayout watcher's stop channel twice (a panic).
func TestCloseIsIdempotent(t *testing.T) {
	u := newTestUI(t)
	u.relayoutWatch() // the main()-started goroutine close() stops
	require.NotNil(t, u.relayoutStop)

	u.close()
	assert.Nil(t, u.relayoutStop, "the first close stops the relayout watcher")
	assert.Nil(t, u.dev, "the first close releases the device")

	assert.NotPanics(t, u.close, "a second close is a no-op")
	assert.NotPanics(t, u.close)
}

// TestLifecycleStopRunsClose: a quit RP6 doesn't see (App.Quit from an
// autopilot `quit`, an Android activity being destroyed) still runs close()
// through the lifecycle stop hook — the autosave / MIDI Stop / device release
// path, not just a process exit.
func TestLifecycleStopRunsClose(t *testing.T) {
	u := newTestUI(t)
	u.relayoutWatch()
	l := &fakeLifecycle{}
	u.hookLifecycle(l)
	require.NotNil(t, l.stopped, "close is registered as the stop hook")
	assert.Nil(t, l.started)
	assert.Nil(t, l.foreground)
	assert.Nil(t, l.background)

	l.stopped()
	assert.Nil(t, u.relayoutStop, "the stop hook ran close")
	assert.Nil(t, u.dev)
	assert.NotPanics(t, l.stopped, "and running it again after an explicit close is harmless")
}
