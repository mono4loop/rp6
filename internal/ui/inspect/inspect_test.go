package inspect

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotCanvasTracksVisibilityAndGeometry(t *testing.T) {
	aApp := test.NewApp()
	t.Cleanup(aApp.Quit)
	a := canvas.NewRectangle(color.White)
	b := canvas.NewRectangle(color.Black)
	a.SetMinSize(fyne.NewSize(40, 30))
	b.SetMinSize(fyne.NewSize(20, 20))
	root := container.NewGridWithColumns(2, a, b)
	w := test.NewWindow(root)
	t.Cleanup(w.Close)
	w.SetPadded(false)
	w.Resize(fyne.NewSize(200, 100))
	c := w.Canvas()
	_ = c.Capture()

	snapshot := SnapshotCanvas(c, Metadata{Scenario: "geometry"}, []Target{
		{ID: "a", Kind: "rack", Object: a},
		{ID: "b", Kind: "rack", Object: b},
		{ID: "missing", Kind: "rack", Object: canvas.NewRectangle(color.NRGBA{R: 0xff, A: 0xff})},
	})

	aElement, ok := snapshot.Find("a")
	require.True(t, ok)
	assert.True(t, aElement.Present)
	assert.True(t, aElement.EffectiveVisible)
	assert.Equal(t, Rect{Width: 98, Height: 100}, aElement.Rect)
	assert.False(t, aElement.Clipped)
	assert.False(t, aElement.UnderMin)

	missing, ok := snapshot.Find("missing")
	require.True(t, ok)
	assert.False(t, missing.Present)
}

func TestCheckReportsLayoutFailures(t *testing.T) {
	snapshot := Snapshot{Elements: []Element{
		{ID: "a", Present: true, SelfVisible: true, EffectiveVisible: true, Clipped: true, Rect: Rect{Width: 20, Height: 20}, VisibleRect: Rect{Width: 10, Height: 20}},
		{ID: "b", Present: true, EffectiveVisible: true, Rect: Rect{X: 5, Width: 20, Height: 20}, VisibleRect: Rect{X: 5, Width: 20, Height: 20}},
		{ID: "off", Present: true, EffectiveVisible: true},
	}}
	problems := Check(snapshot, Contract{
		Required:       []string{"a", "missing"},
		Hidden:         []string{"off"},
		Fit:            []string{"a"},
		NonOverlapping: []string{"a", "b"},
		TouchTargets:   []string{"a"},
		MinTouch:       Size{Width: 44, Height: 44},
	})
	codes := make([]string, len(problems))
	for i, p := range problems {
		codes[i] = p.Code
	}
	assert.ElementsMatch(t, []string{"missing-target", "unexpected-visible", "clipped", "overlap", "small-touch-target"}, codes)
}

func TestSnapshotDetectsClipAndSplitBounds(t *testing.T) {
	aApp := test.NewApp()
	t.Cleanup(aApp.Quit)
	oversize := canvas.NewRectangle(color.White)
	oversize.SetMinSize(fyne.NewSize(120, 120))
	clip := container.NewClip(oversize)
	other := canvas.NewRectangle(color.Black)
	split := container.NewHSplit(clip, other)
	w := test.NewWindow(split)
	t.Cleanup(w.Close)
	w.SetPadded(false)
	w.Resize(fyne.NewSize(100, 60))
	_ = w.Canvas().Capture()

	snapshot := SnapshotCanvas(w.Canvas(), Metadata{}, []Target{{ID: "oversize", Kind: "test", Object: oversize}})
	e, ok := snapshot.Find("oversize")
	require.True(t, ok)
	assert.True(t, e.Present)
	assert.True(t, e.Clipped)
	assert.Less(t, e.VisibleRect.Width, e.Rect.Width)
	assert.Less(t, e.VisibleRect.Height, e.Rect.Height)
}

func TestPixelRectsUseRoundedEdges(t *testing.T) {
	aApp := test.NewApp()
	t.Cleanup(aApp.Quit)
	w := test.NewWindow(canvas.NewRectangle(color.White))
	t.Cleanup(w.Close)
	c := w.Canvas().(software.WindowlessCanvas)
	c.SetScale(3)
	got := pixelRect(Rect{X: 0.2, Width: 0.2, Height: 1}, c)
	assert.Equal(t, PixelRect{X: 0, Width: 1, Height: 3}, got)
}

func TestCheckPhysicalSquares(t *testing.T) {
	snapshot := Snapshot{Elements: []Element{{
		ID: "cell", EffectiveVisible: true, PixelRect: PixelRect{Width: 39, Height: 43},
	}}}
	problems := Check(snapshot, Contract{PhysicalSquares: []PhysicalSquareContract{{
		IDs: []string{"cell"}, MinPixels: 40, MaxPixels: 50, Tolerance: 1,
	}}})
	require.Len(t, problems, 1)
	assert.Equal(t, "not-square", problems[0].Code)
}

func TestCheckContainmentReportsEscapedChild(t *testing.T) {
	snapshot := Snapshot{Elements: []Element{
		{ID: "rack", EffectiveVisible: true, Rect: Rect{Width: 100, Height: 100}},
		{ID: "inside", EffectiveVisible: true, Rect: Rect{X: 10, Y: 10, Width: 20, Height: 20}},
		{ID: "escaped", EffectiveVisible: true, Rect: Rect{X: 90, Y: 10, Width: 20, Height: 20}},
	}}
	problems := Check(snapshot, Contract{Contained: []ContainmentContract{{
		Parent: "rack", Children: []string{"inside", "escaped"}, Tolerance: 0.5,
	}}})
	require.Len(t, problems, 1)
	assert.Equal(t, "outside-parent", problems[0].Code)
	assert.Equal(t, "escaped", problems[0].ID)
}
