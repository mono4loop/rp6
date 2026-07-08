package jam

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineRelaysPadHits(t *testing.T) {
	ta, tb := NewLoopback()
	a := New(ta)
	b := New(tb)
	defer a.Close()
	defer b.Close()

	got := make(chan [2]int, 4)
	b.OnPad = func(pad int, vel uint8) { got <- [2]int{pad, int(vel)} }
	a.Start()
	b.Start()

	a.SendPad(17, 100)

	select {
	case g := <-got:
		require.Equal(t, [2]int{17, 100}, g)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for remote pad hit")
	}
}

// TestNoSelfEcho: a hit sent by A must not come back to A (a jam Engine only
// hears its peers, never itself).
func TestNoSelfEcho(t *testing.T) {
	ta, tb := NewLoopback()
	a := New(ta)
	b := New(tb)
	defer a.Close()
	defer b.Close()

	var selfHits int32
	self := make(chan struct{}, 1)
	a.OnPad = func(int, uint8) {
		selfHits++
		select {
		case self <- struct{}{}:
		default:
		}
	}
	b.OnPad = func(int, uint8) {}
	a.Start()
	b.Start()

	a.SendPad(3, 64)

	select {
	case <-self:
		t.Fatal("A heard its own pad hit (echo)")
	case <-time.After(150 * time.Millisecond):
		assert.Zero(t, selfHits)
	}
}

func TestSendPadIgnoresOutOfRange(t *testing.T) {
	ta, tb := NewLoopback()
	a := New(ta)
	b := New(tb)
	defer a.Close()
	defer b.Close()

	got := make(chan struct{}, 4)
	b.OnPad = func(int, uint8) { got <- struct{}{} }
	b.Start()

	a.SendPad(-1, 100)
	a.SendPad(999, 100)

	select {
	case <-got:
		t.Fatal("out-of-range pad should not have been broadcast")
	case <-time.After(120 * time.Millisecond):
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	cases := [][]byte{
		nil,
		{},
		{'X', 1, 0, 0},  // bad magic
		{'J'},           // too short
		{'J', 1, 5},     // pad msg missing velocity
		{'J', 99, 1, 2}, // unknown kind
	}
	for _, c := range cases {
		_, ok := decode(c)
		assert.False(t, ok, "decode(%v) should fail", c)
	}
	m, ok := decode(encodePad(9, 111))
	require.True(t, ok)
	assert.Equal(t, kindPad, m.kind)
	assert.Equal(t, uint8(9), m.pad)
	assert.Equal(t, uint8(111), m.velocity)
}

func TestCodeFormatAndNormalize(t *testing.T) {
	c := NewCode()
	assert.Len(t, c, 19) // "xxxx-xxxx-xxxx-xxxx"
	assert.Equal(t, 3, strings.Count(c, "-"))
	for part := range strings.SplitSeq(c, "-") {
		assert.Len(t, part, 4)
	}
	assert.NotEqual(t, NewCode(), NewCode()) // vanishingly unlikely to collide

	assert.Equal(t, "k7pq-2m9x", NormalizeCode("  K7PQ-2M9X  "))
}

func TestClosedTransportBroadcastErrs(t *testing.T) {
	ta, _ := NewLoopback()
	require.NoError(t, ta.Close())
	// buffered channel drains a few first; fill then expect ErrClosed eventually.
	var lastErr error
	for range 128 {
		lastErr = ta.Broadcast([]byte{1})
		if lastErr != nil {
			break
		}
	}
	assert.ErrorIs(t, lastErr, ErrClosed)
}
