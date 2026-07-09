package components

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBlackKey(t *testing.T) {
	// One octave from C: C C# D D# E F F# G G# A A# B
	want := []bool{false, true, false, true, false, false, true, false, true, false, true, false}
	for i, w := range want {
		assert.Equal(t, w, isBlackKey(i), "semitone %d", i)
		assert.Equal(t, w, isBlackKey(i+12), "semitone %d (next octave)", i)
	}
}

func TestPianoKeyboardFiresNote(t *testing.T) {
	var got int = -1
	p := NewPianoKeyboard(PianoConfig{
		Accent: color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF},
		OnNote: func(i int) { got = i },
	})
	// Tapping key 5 reports index 5.
	p.keys[5].Tapped(nil)
	assert.Equal(t, 5, got)
}

func TestChromaticForWhites(t *testing.T) {
	// Each count ends on a white key (last index is a white note).
	for _, w := range []int{1, 2, 7, 9, 15, 36} {
		n := chromaticForWhites(w)
		require.Positive(t, n)
		assert.False(t, isBlackKey(n-1), "count for %d whites must end on a white key", w)
		whites := 0
		for i := range n {
			if !isBlackKey(i) {
				whites++
			}
		}
		assert.Equal(t, w, whites, "chromaticForWhites(%d) should contain %d whites", w, w)
	}
	assert.Equal(t, 15, chromaticForWhites(9)) // C..D5, the default minimum
}

func TestPianoKeyboardAdaptsKeyCount(t *testing.T) {
	p := NewPianoKeyboard(PianoConfig{MinWhite: 9, MaxWhite: 36, WhiteW: 54})
	r := p.CreateRenderer()

	visible := func() int {
		n := 0
		for _, k := range p.keys {
			if k.Visible() {
				n++
			}
		}
		return n
	}

	r.Layout(fyne.NewSize(500, 60))
	narrow := visible()
	r.Layout(fyne.NewSize(1800, 60))
	wide := visible()
	assert.Greater(t, wide, narrow, "a wider keyboard shows more keys")
}

func TestPianoKeyboardLabelsWhiteOnly(t *testing.T) {
	p := NewPianoKeyboard(PianoConfig{
		Label: func(i int) string { return "X" },
	})
	for _, k := range p.keys {
		if k.black {
			assert.Empty(t, k.label, "black keys carry no label")
		} else {
			assert.Equal(t, "X", k.label)
		}
	}
}

func TestPianoKeyboardSetMinHeight(t *testing.T) {
	p := NewPianoKeyboard(PianoConfig{})
	base := p.MinSize().Height
	p.SetMinHeight(base * 2)
	assert.Greater(t, p.MinSize().Height, base, "SetMinHeight grows the keyboard")
	p.SetMinHeight(0)
	assert.Equal(t, base, p.MinSize().Height, "0 resets to the default height")
}

// TestPianoKeyboardLayoutFitsBounds guards that every visible key stays within
// the widget bounds across window sizes (no clipping/overhang).
func TestPianoKeyboardLayoutFitsBounds(t *testing.T) {
	for _, w := range []float32{200, 640, 1600} {
		p := NewPianoKeyboard(PianoConfig{})
		r := p.CreateRenderer()
		size := fyne.NewSize(w, 60)
		r.Layout(size)
		for i, k := range p.keys {
			if !k.Visible() {
				continue
			}
			pos := k.Position()
			sz := k.Size()
			assert.GreaterOrEqual(t, pos.X, float32(-0.5), "key %d off the left (w=%v)", i, w)
			assert.LessOrEqual(t, pos.X+sz.Width, size.Width+0.5, "key %d past the right (w=%v)", i, w)
		}
	}
}
