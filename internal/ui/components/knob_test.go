package components

import (
	"image"
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestKnob(onChange func(int)) *Knob {
	return NewKnob(KnobConfig{
		Label: "TEMPO", Value: 120, Min: 40, Max: 300, Step: 5,
		OnChange: onChange,
	})
}

func TestKnobClampAndStep(t *testing.T) {
	test.NewApp()
	changes := 0
	k := newTestKnob(func(int) { changes++ })

	k.Increment()
	assert.Equal(t, 125, k.Value())
	k.Decrement()
	assert.Equal(t, 120, k.Value())
	assert.Equal(t, 2, changes)

	k.SetValue(1000)
	assert.Equal(t, 300, k.Value(), "clamps to max")
	k.SetValue(0)
	assert.Equal(t, 40, k.Value(), "clamps to min")

	// Silent set doesn't fire OnChange.
	before := changes
	k.SetValueSilent(200)
	assert.Equal(t, 200, k.Value())
	assert.Equal(t, before, changes)
}

func TestKnobArrowKeys(t *testing.T) {
	test.NewApp()
	k := newTestKnob(nil)

	k.TypedKey(&fyne.KeyEvent{Name: fyne.KeyUp})
	k.TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
	assert.Equal(t, 130, k.Value(), "Up/Right increase")

	k.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	k.TypedKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	assert.Equal(t, 120, k.Value(), "Down/Left decrease")
}

func TestKnobDragAndScroll(t *testing.T) {
	test.NewApp()
	k := newTestKnob(nil)

	// Drag up 12px (>= 2 * 6px/step) increases by two steps.
	k.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, -12)})
	assert.Equal(t, 130, k.Value())
	k.DragEnd()

	// Drag down 6px decreases one step.
	k.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, 6)})
	assert.Equal(t, 125, k.Value())
	k.DragEnd()

	k.Scrolled(&fyne.ScrollEvent{Scrolled: fyne.NewDelta(0, 1)})
	assert.Equal(t, 130, k.Value())
}

func TestKnobDoubleTapResetsToZero(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme())
	got := -1
	k := NewKnob(KnobConfig{Value: 65, Min: 0, Max: 100, OnChange: func(v int) { got = v }})
	w := test.NewWindow(k)
	defer w.Close()
	w.Resize(fyne.NewSize(160, 70))

	k.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(10, 20)})
	assert.Zero(t, k.Value())
	assert.Zero(t, got, "reset fires OnChange")
	assert.False(t, k.editing)
	assert.NotEqual(t, k, w.Canvas().Focused(), "left-half reset does not open edit mode")

	// Knobs whose range excludes zero reset to the closest valid edge.
	tempo := newTestKnob(nil)
	tempo.Resize(fyne.NewSize(160, 70))
	tempo.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(10, 20)})
	assert.Equal(t, 40, tempo.Value())
}

func TestKnobRightDoubleTapEditsValue(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme())
	got := -1
	k := NewKnob(KnobConfig{Value: 65, Min: 0, Max: 100, OnChange: func(v int) { got = v }})
	w := test.NewWindow(k)
	defer w.Close()
	w.Resize(fyne.NewSize(160, 70))

	k.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 20)})
	require.True(t, k.editing)
	assert.Equal(t, k, w.Canvas().Focused())
	assert.Equal(t, mobile.NumberKeyboard, k.Keyboard())

	k.TypedRune('8')
	k.TypedRune('5')
	assert.Equal(t, "85", k.editText)
	assert.Equal(t, 65, k.Value(), "typing does not commit early")
	k.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	assert.Equal(t, 85, k.Value())
	assert.Equal(t, 85, got)
	assert.False(t, k.editing)
	assert.Equal(t, k, w.Canvas().Focused(), "Done ends editing; the next tap dismisses keyboard focus")
	k.Tapped(nil)
	assert.NotEqual(t, k, w.Canvas().Focused())
}

func TestKnobNumericEditClampBackspaceAndCancel(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme())
	k := NewKnob(KnobConfig{Value: -20, Min: -100, Max: 100})
	k.Resize(fyne.NewSize(160, 70))

	k.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 20)})
	k.TypedRune('-')
	k.TypedRune('9')
	k.TypedRune('9')
	k.TypedRune('9')
	k.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
	k.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnter})
	assert.Equal(t, -99, k.Value())

	k.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 20)})
	k.TypedRune('5')
	k.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	assert.Equal(t, -99, k.Value(), "Escape discards the edit")

	k.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 20)})
	k.TypedRune('9')
	k.TypedRune('9')
	k.TypedRune('9')
	k.TypedRune('\n')
	assert.Equal(t, 100, k.Value(), "Done commits and clamps to the knob range")
}

func TestKnobTapAndDragDoNotFocus(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme())
	k := newTestKnob(nil)
	w := test.NewWindow(k)
	defer w.Close()

	k.Tapped(nil)
	assert.NotEqual(t, k, w.Canvas().Focused())
	k.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, -6)})
	assert.Equal(t, 125, k.Value())
	assert.NotEqual(t, k, w.Canvas().Focused())
}

func TestKnobDragCancelsPartialNumericEdit(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme())
	k := NewKnob(KnobConfig{Value: 65, Min: 0, Max: 100})
	w := test.NewWindow(k)
	defer w.Close()
	w.Resize(fyne.NewSize(160, 70))

	k.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 20)})
	k.TypedRune('9')
	k.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, -6)})
	assert.Equal(t, 66, k.Value(), "drag adjusts the original value instead of committing partial text")
	assert.False(t, k.editing)
}

func TestKnobArrowsDoNotOverwriteNumericEdit(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme())
	k := NewKnob(KnobConfig{Value: 65, Min: 0, Max: 100})
	k.Resize(fyne.NewSize(160, 70))
	k.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(150, 20)})
	k.TypedRune('8')

	k.TypedKey(&fyne.KeyEvent{Name: fyne.KeyUp})
	assert.Equal(t, "8", k.editText)
	assert.Equal(t, 65, k.Value())
}

func TestKnobLightsWhenFocused(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme()) // real fonts (bold monospace value)
	k := newTestKnob(nil)
	w := test.NewWindow(k)
	defer w.Close()

	assert.Equal(t, lcdBorder, k.plate.StrokeColor, "dim border when unfocused")
	assert.Equal(t, transparent, k.glow.FillColor, "no glow when unfocused")
	k.FocusGained()
	assert.Equal(t, withAlpha(k.cfg.Accent, 0x88), k.plate.StrokeColor, "border lights when focused")
	assert.Equal(t, withAlpha(k.cfg.Accent, 0x40), k.glow.FillColor, "glow lights when focused")
	k.FocusLost()
	assert.Equal(t, lcdBorder, k.plate.StrokeColor)
	assert.Equal(t, transparent, k.glow.FillColor)
}

func TestKnobDefaultAccentIsAmber(t *testing.T) {
	k := NewKnob(KnobConfig{Min: 0, Max: 10})
	assert.Equal(t, lcdText, k.cfg.Accent)
	assert.NotEqual(t, color.NRGBA{}, k.cfg.Accent)
}

func TestKnobPending(t *testing.T) {
	test.NewApp()
	k := newTestKnob(nil)
	assert.False(t, k.flashing)

	k.SetPending(true)
	assert.True(t, k.flashing, "queued change flashes")
	k.SetPending(true) // idempotent
	assert.True(t, k.flashing)

	k.SetPending(false)
	assert.False(t, k.flashing, "resolved change stops flashing")
}

// countAccent tallies pixels that are (close to) the accent color, i.e. "lit".
func countAccent(img image.Image, accent color.NRGBA) int {
	near := func(a, b uint8) bool { d := int(a) - int(b); return d < 40 && d > -40 }
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A > 200 && near(c.R, accent.R) && near(c.G, accent.G) && near(c.B, accent.B) {
				n++
			}
		}
	}
	return n
}

func TestKnobIndicators(t *testing.T) {
	accent := color.NRGBA{R: 0xFF, G: 0x80, B: 0x20, A: 0xFF}

	// Every indicator renders a ringPx-square image.
	for _, ind := range []KnobIndicator{RingIndicator{}, BoltIndicator{}, LanesIndicator{}, GridIndicator{Cols: 4, Rows: 4}} {
		img := ind.Image(ringPx, 3, 0, 8, 1, accent)
		require.NotNil(t, img)
		assert.Equal(t, ringPx, img.Bounds().Dx())
		assert.Equal(t, ringPx, img.Bounds().Dy())
	}

	// Bolt and lanes light more as the value rises (count fill).
	assert.Less(t,
		countAccent(BoltIndicator{}.Image(ringPx, 1, 1, 8, 1, accent), accent),
		countAccent(BoltIndicator{}.Image(ringPx, 8, 1, 8, 1, accent), accent),
		"bolt fills more at a higher value")
	assert.Less(t,
		countAccent(LanesIndicator{}.Image(ringPx, 2, 1, 8, 1, accent), accent),
		countAccent(LanesIndicator{}.Image(ringPx, 6, 1, 8, 1, accent), accent),
		"more lanes light at a higher track count")

	// Grid highlights a single tile: the lit position differs between slots, so
	// the two images are not identical.
	g := GridIndicator{Cols: 4, Rows: 4}
	a := g.Image(ringPx, 1, 1, 16, 1, accent)
	b := g.Image(ringPx, 11, 1, 16, 1, accent)
	assert.NotEqual(t, a.(*image.RGBA).Pix, b.(*image.RGBA).Pix, "different slot highlights a different tile")
}

// TestKnobDestroyStopsFlash verifies the renderer's Destroy() tears down the
// pending-flash goroutine, so a widget torn down without an explicit
// SetPending(false) doesn't leak a ticker forever (efhb).
func TestKnobDestroyStopsFlash(t *testing.T) {
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme()) // real fonts for the knob LCD
	k := newTestKnob(nil)
	r := k.CreateRenderer()

	k.SetPending(true)
	k.flashMu.Lock()
	flashing := k.flashing
	k.flashMu.Unlock()
	require.True(t, flashing, "SetPending(true) should start flashing")

	r.Destroy()
	k.flashMu.Lock()
	flashing = k.flashing
	k.flashMu.Unlock()
	assert.False(t, flashing, "Destroy should stop the flash goroutine")
}
