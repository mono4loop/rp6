package components

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
)

func TestStepButtonToggle(t *testing.T) {
	test.NewApp()
	toggles := 0
	s := NewStepButton(func() { toggles++ })
	assert.False(t, s.On())

	s.Tapped(nil)
	assert.True(t, s.On())
	assert.Equal(t, 1, toggles)

	s.Tapped(nil)
	assert.False(t, s.On())
	assert.Equal(t, 2, toggles)

	s.SetOn(true)
	assert.True(t, s.On())
	s.SetPlaying(true) // must not panic before render
}
