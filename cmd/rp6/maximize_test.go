package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNative stands in for the GLFW window: the test flips on to play the
// compositor's maximize/restore, and Restore un-maximizes as the compositor
// would (its resize is then simulated with compositorResize). screenW/screenH
// is the monitor mode in pixels (none when zero).
type fakeNative struct {
	on               bool
	restores         int
	screenW, screenH int
}

func (f *fakeNative) Maximized() bool { return f.on }
func (f *fakeNative) Maximize()       { f.on = true }
func (f *fakeNative) Restore()        { f.restores++; f.on = false }
func (f *fakeNative) ScreenPixels() (int, int, bool) {
	return f.screenW, f.screenH, f.screenW > 0
}

// maximizedSize is what a maximized rp6 window lays out in on the ThinkPad X13
// (1920x1200 at GNOME's 1.25 scale, less the top bar and the libdecor title bar).
var maximizedSize = fyne.NewSize(1536, 891)

func newMaximizeTestUI(t *testing.T) (*ui, *fakeNative) {
	t.Helper()
	u := newTestUI(t)
	f := &fakeNative{}
	u.native = f
	u.win.Resize(fyne.NewSize(designWidth, designHeight))
	require.Equal(t, "window", u.activeVariant)
	return u, f
}

// compositorResize plays the configure that follows a maximize state change:
// the window resizes (onCanvasResize re-reads the state), the relayout it
// requested runs, as relayoutWatch would run it in production, and then a meter
// tick (pollWindow).
func compositorResize(u *ui, size fyne.Size) {
	u.win.Resize(size)
	select {
	case <-u.relayoutReq:
		u.relayout()
	default:
	}
	u.pollWindow()
}

// TestMaximizeShowsConsole is the title-bar double-click: maximizing shows the
// console layout, and restoring returns to the window layout at 850x950 with the
// racks the console force-showed put back.
func TestMaximizeShowsConsole(t *testing.T) {
	u, f := newMaximizeTestUI(t)
	require.False(t, u.fxRack.Object().Visible())
	require.False(t, u.keyboardRack.Object().Visible())

	f.on = true
	compositorResize(u, maximizedSize)
	assert.True(t, u.maximized)
	assert.Equal(t, "console", u.activeVariant, "maximized shows the console layout")
	assert.True(t, u.consoleBtn.On(), "CONSOLE lit while maximized")
	assert.True(t, u.keyboardRack.Object().Visible(), "console reveals the keyboard")
	assert.True(t, u.fxRack.Object().Visible(), "console reveals FX")
	_, saved := loadConsolePref()
	assert.False(t, saved, "maximizing doesn't persist the full-screen choice")

	// The compositor restores the pre-maximize frame, here a size the user had
	// dragged the window to; rp6 snaps back to the windowed size.
	f.on = false
	compositorResize(u, fyne.NewSize(1000, 1000))
	assert.False(t, u.maximized)
	assert.Equal(t, "window", u.activeVariant, "restoring returns to the window layout")
	assert.False(t, u.consoleBtn.On())
	assert.False(t, u.keyboardRack.Object().Visible(), "KEYS restored to off")
	assert.False(t, u.fxRack.Object().Visible(), "FX restored to off")
	assert.Equal(t, fyne.NewSize(designWidth, designHeight), u.canvasSize(), "snapped back to the windowed size")
	u.pollWindow()
	assert.False(t, u.snapWindowed, "the snap ends once the size sticks")
}

// TestRestoreSnapSurvivesBounce is the bug the live check (make smoke-maximize)
// caught: right after a restore the console's wider content minimum is still
// the window's size limit, so Fyne grows the snapped window straight back. The
// snap must retry on later ticks until the size sticks.
func TestRestoreSnapSurvivesBounce(t *testing.T) {
	u, f := newMaximizeTestUI(t)
	f.on = true
	compositorResize(u, maximizedSize)
	require.Equal(t, "console", u.activeVariant)

	f.on = false
	compositorResize(u, fyne.NewSize(1000, 900))
	require.Equal(t, fyne.NewSize(designWidth, designHeight), u.canvasSize())
	u.win.Resize(fyne.NewSize(1376, 860)) // Fyne enforcing the stale console minimum
	u.pollWindow()
	assert.Equal(t, fyne.NewSize(designWidth, designHeight), u.canvasSize(), "the snap retries after a bounce")
	u.pollWindow()
	assert.False(t, u.snapWindowed)

	// A window that never shrinks (a content minimum over the target) stops the
	// retries instead of fighting it forever.
	u.snapWindowed = true
	for range snapMaxTries + 1 {
		u.win.Resize(fyne.NewSize(1376, 860))
		u.pollWindow()
	}
	assert.False(t, u.snapWindowed, "the retries are bounded")
}

// TestMaximizeRoundTrips repeats maximize/restore: the rack state must not drift.
func TestMaximizeRoundTrips(t *testing.T) {
	u, f := newMaximizeTestUI(t)
	for cycle := range 3 {
		f.on = true
		compositorResize(u, maximizedSize)
		require.Equal(t, "console", u.activeVariant, "cycle %d", cycle+1)
		assert.True(t, u.paksRack.Object().Visible(), "cycle %d: console shows PAKS", cycle+1)

		f.on = false
		compositorResize(u, fyne.NewSize(designWidth, designHeight))
		require.Equal(t, "window", u.activeVariant, "cycle %d", cycle+1)
		assert.False(t, u.paksRack.Object().Visible(), "cycle %d: PAKS restored to off", cycle+1)
		assert.True(t, u.seqRack.Object().Visible(), "cycle %d: sequencer stays on", cycle+1)
	}
}

// TestConsoleButtonUnmaximizes checks the lit CONSOLE button leaves a maximized
// console: it asks the compositor to restore, and the resize that answers
// switches back to the window layout.
func TestConsoleButtonUnmaximizes(t *testing.T) {
	u, f := newMaximizeTestUI(t)
	f.on = true
	compositorResize(u, maximizedSize)
	require.Equal(t, "console", u.activeVariant)

	u.toggleConsole()
	assert.Equal(t, 1, f.restores, "CONSOLE un-maximizes the window")
	assert.False(t, u.fullScreen, "and doesn't go full screen")

	compositorResize(u, fyne.NewSize(designWidth, designHeight))
	assert.Equal(t, "window", u.activeVariant)
	assert.False(t, u.consoleBtn.On())
	assert.False(t, u.keyboardRack.Object().Visible(), "KEYS restored to off")
}

// TestFullScreenOverMaximized checks F11 on a maximized window: it goes full
// screen on top, and leaving full screen keeps the console because the
// compositor returns the window to maximized. A later restore still puts back
// the racks the console force-showed when the window was first maximized.
func TestFullScreenOverMaximized(t *testing.T) {
	u, f := newMaximizeTestUI(t)
	f.on = true
	compositorResize(u, maximizedSize)
	require.Equal(t, "console", u.activeVariant)

	u.toggleFullScreen()
	assert.True(t, u.fullScreen)
	assert.Equal(t, "console", u.activeVariant)

	// While full screen the compositor may not report the maximized state; the
	// state from before full screen must survive.
	f.on = false
	compositorResize(u, fyne.NewSize(1920, 1200))
	assert.True(t, u.maximized, "maximized state frozen while full screen")
	f.on = true

	u.toggleFullScreen()
	assert.False(t, u.fullScreen)
	assert.True(t, u.isFullScreen(), "still maximized, so still the console")
	assert.Equal(t, 0, f.restores, "leaving full screen doesn't un-maximize")
	compositorResize(u, maximizedSize)
	assert.Equal(t, "console", u.activeVariant)

	f.on = false
	compositorResize(u, fyne.NewSize(designWidth, designHeight))
	assert.Equal(t, "window", u.activeVariant)
	assert.False(t, u.keyboardRack.Object().Visible(), "KEYS restored to off")
	assert.False(t, u.fxRack.Object().Visible(), "FX restored to off")
}

// TestMaximizeWithoutResizeShowsConsole: a maximize the compositor can't size
// (the window's minimum is taller than the work area) flags the window
// maximized without resizing it, so no resize reports it. The meter tick's poll
// must still notice the state change.
func TestMaximizeWithoutResizeShowsConsole(t *testing.T) {
	u, f := newMaximizeTestUI(t)
	f.screenW, f.screenH = 1920, 1200
	f.on = true
	u.pollWindow()
	select {
	case <-u.relayoutReq:
		u.relayout()
	default:
		t.Fatal("the poll requests a relayout when the maximize state flips")
	}
	assert.True(t, u.maximized)
	assert.Equal(t, "console", u.activeVariant)
	_, learned := loadWorkArea("1920x1200@1.00")
	assert.False(t, learned, "an unsized maximize isn't learned as a work area")
}

// TestWindowFitsScreen: on a short screen the window opens at the design size,
// then fits the screen less the panel estimate once the scale is known. A
// maximize teaches it the real work area, which the restore snap then uses.
func TestWindowFitsScreen(t *testing.T) {
	u, f := newMaximizeTestUI(t)
	f.screenW, f.screenH = 1920, 1200 // the ThinkPad X13
	c, ok := u.win.Canvas().(software.WindowlessCanvas)
	require.True(t, ok)
	c.SetScale(1.25) // 1536x960 logical

	u.pollWindow()
	assert.Equal(t, fyne.NewSize(designWidth, 960-screenReserve), u.canvasSize(), "fitted to the screen less the panel estimate")
	u.win.Resize(fyne.NewSize(1000, 900)) // the user drags it larger
	u.pollWindow()
	assert.Equal(t, fyne.NewSize(1000, 900), u.canvasSize(), "fitted once per screen, not on every tick")

	f.on = true
	compositorResize(u, maximizedSize)
	require.Equal(t, "console", u.activeVariant)
	area, ok := loadWorkArea("1920x1200@1.25")
	require.True(t, ok, "the maximized canvas is learned as the work area")
	assert.Equal(t, maximizedSize, area)

	f.on = false
	compositorResize(u, fyne.NewSize(1000, 900))
	assert.Equal(t, "window", u.activeVariant)
	assert.Equal(t, fyne.NewSize(designWidth, maximizedSize.Height), u.canvasSize(), "snapped to the learned work area")
}
