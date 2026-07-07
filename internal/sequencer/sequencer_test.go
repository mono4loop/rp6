package sequencer

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type recorder struct {
	mu   sync.Mutex
	hits map[int]int
}

func newRecorder() *recorder { return &recorder{hits: map[int]int{}} }

func (r *recorder) fire(pad int, _ uint8) {
	r.mu.Lock()
	r.hits[pad]++
	r.mu.Unlock()
}

func (r *recorder) get(pad int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits[pad]
}

func TestStepInterval(t *testing.T) {
	assert.Equal(t, 125*time.Millisecond, stepInterval(120)) // 16ths at 120 BPM
	assert.Equal(t, stepInterval(120), stepInterval(0))      // bad bpm falls back
}

func TestGridEditing(t *testing.T) {
	e := New(8, 4, func(int, uint8) {})
	assert.Equal(t, 8, e.MaxTracks())
	assert.Equal(t, 8, e.Tracks())
	assert.Equal(t, 4, e.MaxBars())
	assert.Equal(t, StepsPerBar, e.TrackLen(0)) // 1 bar by default
	assert.Equal(t, -1, e.Pad(0))

	e.SetPad(2, 42)
	assert.Equal(t, 42, e.Pad(2))

	assert.True(t, e.Toggle(2, 5))
	assert.True(t, e.Step(2, 5))
	assert.False(t, e.Toggle(2, 5))

	e.SetStep(1, 0, true)
	e.Clear()
	assert.False(t, e.Step(1, 0))

	assert.False(t, e.Toggle(99, 0))  // track out of range
	assert.False(t, e.Toggle(0, 999)) // step out of range
}

func TestBars(t *testing.T) {
	e := New(8, 4, func(int, uint8) {})
	assert.Equal(t, 1, e.Bars(0))
	e.SetBars(0, 2)
	assert.Equal(t, 2, e.Bars(0))
	assert.Equal(t, 2*StepsPerBar, e.TrackLen(0))
	e.SetBars(0, 99) // clamp to maxBars
	assert.Equal(t, 4, e.Bars(0))
	e.SetBars(0, 0) // clamp to 1
	assert.Equal(t, 1, e.Bars(0))

	// A 2-bar track can hold a step in its second bar; it persists.
	e.SetBars(1, 2)
	e.SetStep(1, StepsPerBar+3, true)
	assert.True(t, e.Step(1, StepsPerBar+3))
}

func TestPlaybackFiresProgrammedPads(t *testing.T) {
	rec := newRecorder()
	e := New(8, 4, rec.fire)
	e.SetTempo(6000)
	e.SetPad(0, 36)
	e.SetPad(1, 38)
	e.SetStep(0, 0, true)
	e.SetStep(0, 4, true)
	e.SetStep(1, 8, true)

	var ticks []int
	var mu sync.Mutex
	e.OnStep = func(tk int) { mu.Lock(); ticks = append(ticks, tk); mu.Unlock() }

	e.Start()
	assert.True(t, e.Running())
	time.Sleep(80 * time.Millisecond)
	e.Stop()
	assert.False(t, e.Running())

	assert.Greater(t, rec.get(36), 0)
	assert.Greater(t, rec.get(38), 0)
	assert.Greater(t, rec.get(36), rec.get(38))
	mu.Lock()
	assert.NotEmpty(t, ticks)
	mu.Unlock()
}

func TestMutedTrackDoesNotFire(t *testing.T) {
	rec := newRecorder()
	e := New(4, 4, rec.fire)
	e.SetTempo(6000)
	e.SetPad(0, 36)
	e.SetStep(0, 0, true)
	assert.True(t, e.ToggleMuted(0))
	assert.True(t, e.Muted(0))

	e.Start()
	time.Sleep(30 * time.Millisecond)
	e.Stop()
	assert.Equal(t, 0, rec.get(36))
}

func TestSetTracksClampsAndPreservesData(t *testing.T) {
	e := New(8, 4, func(int, uint8) {})
	e.SetTracks(4)
	assert.Equal(t, 4, e.Tracks())
	e.SetTracks(100)
	assert.Equal(t, 8, e.Tracks())
	e.SetTracks(0)
	assert.Equal(t, 1, e.Tracks())

	e.SetTracks(8)
	e.SetPad(7, 42)
	e.SetStep(7, 3, true)
	e.SetTracks(2)
	assert.Equal(t, 42, e.Pad(7))
	assert.True(t, e.Step(7, 3))
}

func TestStartIdempotent(t *testing.T) {
	e := New(4, 4, func(int, uint8) {})
	e.Start()
	e.Start()
	assert.True(t, e.Running())
	e.Stop()
	assert.False(t, e.Running())
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	e := New(8, 4, func(int, uint8) {})
	e.SetTracks(3)
	e.SetTempo(140)
	e.SetPad(0, 12)
	e.SetBars(0, 2)
	e.SetMuted(1, true)
	e.SetStep(0, 5, true)
	e.SetStep(0, StepsPerBar+2, true) // second bar

	st := e.Snapshot()
	assert.Equal(t, 3, st.Tracks)
	assert.Equal(t, 140.0, st.Tempo)

	// Apply the snapshot to a fresh engine.
	e2 := New(8, 4, func(int, uint8) {})
	e2.Restore(st)
	assert.Equal(t, 3, e2.Tracks())
	assert.Equal(t, 12, e2.Pad(0))
	assert.Equal(t, 2, e2.Bars(0))
	assert.True(t, e2.Muted(1))
	assert.True(t, e2.Step(0, 5))
	assert.True(t, e2.Step(0, StepsPerBar+2))
	assert.False(t, e2.Step(0, 6))
}
