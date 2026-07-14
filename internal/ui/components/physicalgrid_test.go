package components

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPhysicalGridKeepsSquarePixelBounds(t *testing.T) {
	for _, scale := range []float32{1, 3} {
		t.Run(string(rune('0'+int(scale))), func(t *testing.T) {
			a := test.NewApp()
			t.Cleanup(a.Quit)
			objects := make([]fyne.CanvasObject, 24)
			for i := range objects {
				objects[i] = canvas.NewRectangle(color.White)
			}
			grid := NewPhysicalGrid(6, 130, 150, objects...)
			w := test.NewWindow(grid.Object)
			t.Cleanup(w.Close)
			c, ok := w.Canvas().(software.WindowlessCanvas)
			require.True(t, ok)
			c.SetScale(scale)
			w.Resize(fyne.NewSize(1000, 1000))
			grid.Object.Resize(grid.PreferredSize(fyne.NewSize(1000, 1000)))
			_ = w.Canvas().Capture()

			width := physicalWidth(w.Canvas(), objects[0])
			height := physicalHeight(w.Canvas(), objects[0])
			assert.InDelta(t, width, height, 1)
			assert.GreaterOrEqual(t, width, 130)
			assert.LessOrEqual(t, width, 150)
		})
	}
}

func TestPhysicalGridFillsWidthWithinBounds(t *testing.T) {
	objects := make([]fyne.CanvasObject, 4)
	for i := range objects {
		objects[i] = canvas.NewRectangle(color.White)
	}
	grid := NewPhysicalGrid(2, 40, 50, objects...)
	grid.physicalScale = func(fyne.CanvasObject) float32 { return 1 }

	preferred := grid.PreferredSize(fyne.NewSize(94, 1000))
	assert.Equal(t, fyne.NewSize(94, 94), preferred)
	assert.Equal(t, fyne.NewSize(104, 104), grid.PreferredSize(fyne.NewSize(1000, 1000)))
	assert.Equal(t, fyne.NewSize(84, 84), grid.MinSize())
	grid.SetPadding(3)
	assert.Equal(t, fyne.NewSize(103, 103), grid.PreferredSize(fyne.NewSize(1000, 1000)))
}

func TestPhysicalGridNeverEscapesConstrainedAllocation(t *testing.T) {
	objects := make([]fyne.CanvasObject, 6)
	for i := range objects {
		objects[i] = canvas.NewRectangle(color.White)
	}
	grid := NewPhysicalGrid(6, 80, 130, objects...)
	grid.physicalScale = func(fyne.CanvasObject) float32 { return 1 }
	grid.Object.Resize(fyne.NewSize(320, 100))

	last := objects[len(objects)-1]
	assert.LessOrEqual(t, last.Position().X+last.Size().Width, grid.Object.Size().Width)
	assert.InDelta(t, last.Size().Width, last.Size().Height, 0.01)
}

func TestContentFitLeavesExcessOutsideChild(t *testing.T) {
	child := canvas.NewRectangle(color.White)
	child.SetMinSize(fyne.NewSize(20, 20))
	fit := NewContentFit(child, func(fyne.Size) fyne.Size { return fyne.NewSize(80, 60) })
	renderer := test.TempWidgetRenderer(t, fit)
	renderer.Layout(fyne.NewSize(300, 200))
	assert.Equal(t, fyne.NewSize(80, 60), child.Size())
	assert.Equal(t, fyne.NewPos(110, 0), child.Position())
}

func TestContentFitClampsChildToAllocation(t *testing.T) {
	child := canvas.NewRectangle(color.White)
	fit := NewContentFit(child, func(fyne.Size) fyne.Size { return fyne.NewSize(200, 100) })
	renderer := test.TempWidgetRenderer(t, fit)
	renderer.Layout(fyne.NewSize(80, 60))
	assert.Equal(t, fyne.NewSize(80, 60), child.Size())
	assert.Equal(t, fyne.Position{}, child.Position())
}

func TestContentFitCanAdvertiseContentMinimum(t *testing.T) {
	child := canvas.NewRectangle(color.White)
	child.SetMinSize(fyne.NewSize(80, 60))
	fit := NewContentFit(child, nil)
	assert.Equal(t, fyne.NewSize(1, 1), fit.MinSize())
	fit.SetContentMin(true)
	assert.Equal(t, fyne.NewSize(80, 60), fit.MinSize())
	fit.SetMinSizeFunc(func() fyne.Size { return fyne.NewSize(120, 90) })
	assert.Equal(t, fyne.NewSize(120, 90), fit.MinSize())
}

func physicalWidth(c fyne.Canvas, object fyne.CanvasObject) int {
	pos := object.Position()
	x0, _ := c.PixelCoordinateForPosition(pos)
	x1, _ := c.PixelCoordinateForPosition(pos.Add(fyne.NewPos(object.Size().Width, 0)))
	return x1 - x0
}

func physicalHeight(c fyne.Canvas, object fyne.CanvasObject) int {
	pos := object.Position()
	_, y0 := c.PixelCoordinateForPosition(pos)
	_, y1 := c.PixelCoordinateForPosition(pos.Add(fyne.NewPos(0, object.Size().Height)))
	return y1 - y0
}
