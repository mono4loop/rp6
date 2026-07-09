package midiin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeDevice is a no-op Device for registry tests.
type fakeDevice struct{ path string }

func (f *fakeDevice) Name() string         { return "fake" }
func (f *fakeDevice) Path() string         { return f.path }
func (f *fakeDevice) Run(h Handlers) error { return nil }
func (f *fakeDevice) Close() error         { return nil }

// resetDrivers clears the registry and restores it when the test ends.
func resetDrivers(t *testing.T) {
	t.Helper()
	mu.Lock()
	saved := drivers
	drivers = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		drivers = saved
		mu.Unlock()
	})
}

func TestDetectNoDrivers(t *testing.T) {
	resetDrivers(t)
	_, err := Detect()
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDetectSkipsAbsent(t *testing.T) {
	resetDrivers(t)
	opened := ""
	Register(Driver{
		Name:   "absent",
		Detect: func() (string, bool) { return "", false },
		Open:   func(p string) (Device, error) { opened = "absent"; return &fakeDevice{p}, nil },
	})
	Register(Driver{
		Name:   "present",
		Detect: func() (string, bool) { return "/dev/snd/midiC9D0", true },
		Open:   func(p string) (Device, error) { opened = "present"; return &fakeDevice{p}, nil },
	})

	dev, err := Detect()
	assert.NoError(t, err)
	assert.Equal(t, "present", opened, "the absent driver must not be opened")
	assert.Equal(t, "/dev/snd/midiC9D0", dev.Path())
}

func TestDetectIgnoresIncompleteDriver(t *testing.T) {
	resetDrivers(t)
	Register(Driver{Name: "no-open", Detect: func() (string, bool) { return "x", true }})
	_, err := Detect()
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestPresentReturnsAllPluggedControllers(t *testing.T) {
	resetDrivers(t)
	Register(Driver{
		Name:   "macropad",
		Detect: func() (string, bool) { return "/dev/snd/midiC1D0", true },
		Open:   func(p string) (Device, error) { return &fakeDevice{p}, nil },
	})
	Register(Driver{
		Name:   "absent",
		Detect: func() (string, bool) { return "", false },
		Open:   func(p string) (Device, error) { return &fakeDevice{p}, nil },
	})
	Register(Driver{
		Name:   "keyboard",
		Detect: func() (string, bool) { return "/dev/snd/midiC2D0", true },
		Open:   func(p string) (Device, error) { return &fakeDevice{p}, nil },
	})

	found := Present()
	assert.Len(t, found, 2, "both plugged controllers reported, the absent one skipped")
	assert.Equal(t, "macropad", found[0].Name)
	assert.Equal(t, "/dev/snd/midiC1D0", found[0].Path)
	assert.Equal(t, "keyboard", found[1].Name)

	dev, err := found[1].Open()
	assert.NoError(t, err)
	assert.Equal(t, "/dev/snd/midiC2D0", dev.Path())
}

func TestLineMatchesAny(t *testing.T) {
	line := " 2 [MacroPad       ]: USB-Audio - MacroPad"
	assert.True(t, lineMatchesAny(line, []string{"macropad"}))
	assert.True(t, lineMatchesAny(line, []string{"nope", "MACROPAD"}))
	assert.False(t, lineMatchesAny(line, []string{"p-6"}))
	assert.False(t, lineMatchesAny(line, []string{""}))
}
