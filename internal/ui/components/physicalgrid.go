package components

import (
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// PhysicalGrid lays out a fixed number of square columns whose side is bounded
// in physical screen pixels. The backing Object is a regular *fyne.Container so
// Fyne and RP6's inspection walker can traverse its children normally.
type PhysicalGrid struct {
	Object *fyne.Container

	cols          int
	minPixels     float32
	maxPixels     float32
	padding       float32
	objects       []fyne.CanvasObject
	physicalScale func(fyne.CanvasObject) float32
}

// NewPhysicalGrid returns a square grid with cell sides constrained to
// minPixels..maxPixels physical pixels. The grid keeps exactly cols columns;
// hidden children are omitted from its row count and layout.
func NewPhysicalGrid(cols int, minPixels, maxPixels float32, objects ...fyne.CanvasObject) *PhysicalGrid {
	if cols < 1 {
		cols = 1
	}
	if minPixels <= 0 {
		minPixels = 1
	}
	if maxPixels < minPixels {
		maxPixels = minPixels
	}
	g := &PhysicalGrid{
		cols:          cols,
		minPixels:     minPixels,
		maxPixels:     maxPixels,
		padding:       theme.Padding(),
		objects:       objects,
		physicalScale: physicalScaleFor,
	}
	g.Object = container.New(&physicalGridLayout{grid: g}, objects...)
	return g
}

// SetPadding sets the logical gap between cells.
func (g *PhysicalGrid) SetPadding(padding float32) {
	if padding < 0 {
		padding = 0
	}
	g.padding = padding
	g.Object.Refresh()
}

// PreferredSize returns the tightly-packed grid size for the available area.
// Width determines the square side unless height is also positive and tighter.
func (g *PhysicalGrid) PreferredSize(available fyne.Size) fyne.Size {
	count := visibleCount(g.objects)
	rows := rowsFor(count, g.cols)
	if rows == 0 {
		return fyne.Size{}
	}
	side := g.sideFor(available, rows)
	padding := g.padding
	return fyne.NewSize(
		float32(g.cols)*side+float32(g.cols-1)*padding,
		float32(rows)*side+float32(rows-1)*padding,
	)
}

// MinSize returns the tightly-packed grid size at the physical minimum side.
func (g *PhysicalGrid) MinSize() fyne.Size {
	count := visibleCount(g.objects)
	rows := rowsFor(count, g.cols)
	if rows == 0 {
		return fyne.Size{}
	}
	side := g.logicalPixels(g.minPixels)
	padding := g.padding
	return fyne.NewSize(
		float32(g.cols)*side+float32(g.cols-1)*padding,
		float32(rows)*side+float32(rows-1)*padding,
	)
}

func (g *PhysicalGrid) sideFor(available fyne.Size, rows int) float32 {
	padding := g.padding
	fit := float32(math.MaxFloat32)
	if available.Width > 0 {
		fit = (available.Width - float32(g.cols-1)*padding) / float32(g.cols)
	}
	if available.Height > 0 {
		heightFit := (available.Height - float32(rows-1)*padding) / float32(rows)
		fit = min(fit, heightFit)
	}
	minimum := g.logicalPixels(g.minPixels)
	maximum := g.logicalPixels(g.maxPixels)
	if fit == float32(math.MaxFloat32) {
		return maximum
	}
	// Never exceed the allocation. The minimum is a preferred/touch floor used
	// by parent layout negotiation and inspection contracts, not permission to
	// paint outside a constrained pane.
	if fit < minimum {
		return max(fit, float32(1))
	}
	return min(fit, maximum)
}

func (g *PhysicalGrid) logicalPixels(pixels float32) float32 {
	scale := g.physicalScale(g.Object)
	if scale <= 0 {
		scale = 1
	}
	return pixels / scale
}

type physicalGridLayout struct{ grid *PhysicalGrid }

func (l *physicalGridLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return l.grid.MinSize()
}

func (l *physicalGridLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	count := visibleCount(objects)
	rows := rowsFor(count, l.grid.cols)
	if rows == 0 {
		return
	}
	side := l.grid.sideFor(size, rows)
	padding := l.grid.padding
	index := 0
	for _, object := range objects {
		if object == nil || !object.Visible() {
			continue
		}
		row, col := index/l.grid.cols, index%l.grid.cols
		object.Move(fyne.NewPos(float32(col)*(side+padding), float32(row)*(side+padding)))
		object.Resize(fyne.NewSquareSize(side))
		index++
	}
}

func physicalScaleFor(object fyne.CanvasObject) float32 {
	app := fyne.CurrentApp()
	if app != nil && app.Driver() != nil {
		if c := app.Driver().CanvasForObject(object); c != nil {
			x0, _ := c.PixelCoordinateForPosition(fyne.Position{})
			x1, _ := c.PixelCoordinateForPosition(fyne.NewPos(1000, 0))
			if scale := float32(x1-x0) / 1000; scale > 0 {
				return scale
			}
			if scale := c.Scale(); scale > 0 {
				return scale
			}
		}
	}
	if device := fyne.CurrentDevice(); device != nil {
		if scale := device.SystemScaleForWindow(nil); scale > 0 {
			return scale
		}
	}
	return 1
}

func visibleCount(objects []fyne.CanvasObject) int {
	count := 0
	for _, object := range objects {
		if object != nil && object.Visible() {
			count++
		}
	}
	return count
}

func rowsFor(count, cols int) int {
	if count == 0 {
		return 0
	}
	return (count + cols - 1) / cols
}
