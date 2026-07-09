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
	r := p.CreateRenderer()
	r.Layout(fyne.NewSize(900, 80))

	// A tap in the lower area of a white key hits that white key (no black key
	// overlaps the bottom). Aim at the vertical center-bottom of key 2 (D, white).
	k := p.keys[2]
	require.False(t, k.black)
	pos := k.Position()
	sz := k.Size()
	p.Tapped(&fyne.PointEvent{Position: fyne.NewPos(pos.X+sz.Width/2, pos.Y+sz.Height-2)})
	assert.Equal(t, 2, got)

	// A tap high over a black key hits the black key, not the white beneath it.
	got = -1
	var bk *pianoKey
	for _, kk := range p.keys {
		if kk.black && kk.Visible() {
			bk = kk
			break
		}
	}
	require.NotNil(t, bk)
	bp, bs := bk.Position(), bk.Size()
	p.Tapped(&fyne.PointEvent{Position: fyne.NewPos(bp.X+bs.Width/2, bp.Y+2)})
	assert.Equal(t, bk.index, got, "a tap in the black-key zone hits the black key")
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
