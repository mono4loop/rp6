package effects

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// counter is a concurrency-safe fake Trigger.
type counter struct {
	mu   sync.Mutex
	hits map[int]int
}

func newCounter() *counter { return &counter{hits: map[int]int{}} }

func (c *counter) fire(pad int) {
	c.mu.Lock()
	c.hits[pad]++
	c.mu.Unlock()
}

func (c *counter) get(pad int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[pad]
}

func TestInterval(t *testing.T) {
	// Look up divisions by name so the test survives list reordering.
	idx := func(name string) int {
		for i, d := range Divisions {
			if d.Name == name {
				return i
			}
		}
		t.Fatalf("division %q not found", name)
		return 0
	}
	// 1/8 at 120 BPM = 250ms; 1/16 = 125ms; whole note = 2s.
	assert.Equal(t, 250*time.Millisecond, Interval(120, idx("1/8")))
	assert.Equal(t, 125*time.Millisecond, Interval(120, idx("1/16")))
	assert.Equal(t, 2*time.Second, Interval(120, idx("1")))
	// bad div falls back to default; bad bpm falls back to 120.
	assert.Equal(t, Interval(120, DefaultDiv), Interval(0, 999))
}

func TestTapFiresOnceWithoutRoll(t *testing.T) {
	c := newCounter()
	e := New(c.fire)

	e.Tap(7)
	assert.Equal(t, 1, c.get(7))
	assert.False(t, e.IsRolling(7))
}

func TestRollToggle(t *testing.T) {
	c := newCounter()
	e := New(c.fire)
	e.SetTempo(6000) // very fast so rolls tick quickly
	e.SetSlot(3, 0, KindRoll)
	e.SetRollDiv(3, DefaultDiv)

	e.Tap(3) // start rolling
	assert.True(t, e.IsRolling(3))
	time.Sleep(40 * time.Millisecond)
	e.Tap(3) // stop
	assert.False(t, e.IsRolling(3))

	time.Sleep(20 * time.Millisecond) // let any already-scheduled retrigger land
	got := c.get(3)
	assert.GreaterOrEqual(t, got, 2, "roll should have fired several times")

	// No further fires once settled.
	time.Sleep(40 * time.Millisecond)
	assert.Equal(t, got, c.get(3))
}

func TestRemovingRollStopsIt(t *testing.T) {
	c := newCounter()
	e := New(c.fire)
	e.SetTempo(6000)
	e.SetSlot(1, 0, KindRoll)

	e.Tap(1)
	require.True(t, e.IsRolling(1))

	e.SetSlot(1, 0, KindNone) // removing the effect stops the roll
	assert.False(t, e.IsRolling(1))
}

func TestState(t *testing.T) {
	e := New(func(int) {})
	e.SetSlot(2, 1, KindRoll)
	e.SetRollDiv(2, 6)

	s := e.State(2)
	assert.Equal(t, KindRoll, s.Slots[1])
	assert.Equal(t, 6, s.RollDiv)
	assert.True(t, e.HasEffect(2, KindRoll))
	assert.False(t, e.HasEffect(99, KindRoll), "untouched pad has no effects")
}

func TestStopAll(t *testing.T) {
	c := newCounter()
	e := New(c.fire)
	e.SetTempo(6000)
	e.SetSlot(0, 0, KindRoll)
	e.SetSlot(5, 0, KindRoll)
	e.Tap(0)
	e.Tap(5)
	require.True(t, e.IsRolling(0))
	require.True(t, e.IsRolling(5))

	e.StopAll()
	assert.False(t, e.IsRolling(0))
	assert.False(t, e.IsRolling(5))
}
