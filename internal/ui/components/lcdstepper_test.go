package components

import (
	"strconv"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/assert"
)

func newTestLCD(t *testing.T, onChange func(int)) *LCDStepper {
	t.Helper()
	a := test.NewApp()
	a.Settings().SetTheme(theme.DefaultTheme()) // real fonts for monospace text
	return NewLCDStepper(LCDStepperConfig{
		Title: "N", Value: 5, Min: 0, Max: 10, Step: 1,
		Format:   func(v int) string { return strconv.Itoa(v) },
		Parse:    func(s string) (int, bool) { v, err := strconv.Atoi(s); return v, err == nil },
		OnChange: onChange,
	})
}

func TestLCDStepperIncrementDecrement(t *testing.T) {
	var got []int
	s := newTestLCD(t, func(v int) { got = append(got, v) })

	s.Increment()
	s.Increment()
	s.Decrement()

	assert.Equal(t, 6, s.Value())
	assert.Equal(t, []int{6, 7, 6}, got)
}

func TestLCDStepperClampsAndSuppressesNoChange(t *testing.T) {
	var calls int
	s := newTestLCD(t, func(int) { calls++ })

	s.SetValue(100) // clamps to 10 -> change
	assert.Equal(t, 10, s.Value())

	s.Increment() // already max -> no change, no callback
	assert.Equal(t, 10, s.Value())
	assert.Equal(t, 1, calls)
}

func TestLCDDisplayTypingReplacesAndCommits(t *testing.T) {
	var last int
	s := newTestLCD(t, func(v int) { last = v })
	w := test.NewWindow(s.Object())
	defer w.Close()
	w.Resize(fyne.NewSize(400, 80))

	d := s.display
	w.Canvas().Focus(d) // FocusGained: pristine, shows current value

	// First keystroke replaces the shown value (select-all-like behavior).
	d.TypedRune('8')
	assert.False(t, d.pristine)
	assert.Equal(t, "8", d.buffer)

	d.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	assert.Equal(t, 8, s.Value())
	assert.Equal(t, 8, last)
}

func TestLCDDisplayBackspaceAndReject(t *testing.T) {
	s := newTestLCD(t, func(int) {})
	w := test.NewWindow(s.Object())
	defer w.Close()

	d := s.display
	w.Canvas().Focus(d)
	d.TypedRune('1')
	d.TypedRune('2') // "12"
	d.TypedRune('x') // rejected (not a digit)
	assert.Equal(t, "12", d.buffer)
	d.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
	assert.Equal(t, "1", d.buffer)

	d.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	assert.Equal(t, 1, s.Value())
}

func TestLCDDisplayEscapeCancels(t *testing.T) {
	s := newTestLCD(t, func(int) {})
	w := test.NewWindow(s.Object())
	defer w.Close()

	d := s.display
	w.Canvas().Focus(d)
	d.TypedRune('9')
	d.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})

	assert.Equal(t, 5, s.Value(), "escape must not change the value")
}

func TestLCDDisplayCommitEmptyKeepsValue(t *testing.T) {
	s := newTestLCD(t, func(int) {})
	w := test.NewWindow(s.Object())
	defer w.Close()

	d := s.display
	w.Canvas().Focus(d)                              // pristine
	d.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn}) // commit without typing
	assert.Equal(t, 5, s.Value())
}

func TestLCDTapFocuses(t *testing.T) {
	s := newTestLCD(t, func(int) {})
	// Place the display alongside another focusable to prove tap moves focus.
	other := widget.NewEntry()
	w := test.NewWindow(container.NewHBox(other, s.Object()))
	defer w.Close()
	w.Resize(fyne.NewSize(400, 80))

	w.Canvas().Focus(other)
	test.Tap(s.display)
	assert.Same(t, s.display, w.Canvas().Focused())
}
