package p6

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClockInterval(t *testing.T) {
	// 120 BPM: 120*24 = 2880 pulses/min -> 60s/2880 = 20.833ms.
	assert.InDelta(t, 20.833, float64(ClockInterval(120).Microseconds())/1000.0, 0.1)
	// 60 BPM -> half the rate.
	assert.Equal(t, 2*ClockInterval(120), ClockInterval(60))
	// Non-positive falls back to 120.
	assert.Equal(t, ClockInterval(120), ClockInterval(0))
}

// safeBuf is a concurrency-safe io.Writer for exercising the clock goroutine.
type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.b.Bytes()...)
}

func TestClockerStartStreamsClockAndStops(t *testing.T) {
	sb := &safeBuf{}
	c := NewClocker(New(sb, DefaultConfig()), 6000) // fast tempo -> ~0.4ms pulses

	require.NoError(t, c.Start())
	assert.True(t, c.Running())
	time.Sleep(15 * time.Millisecond)
	require.NoError(t, c.Stop())
	assert.False(t, c.Running())

	b := sb.Bytes()
	require.NotEmpty(t, b)
	assert.Equal(t, byte(msgStart), b[0], "should begin with MIDI Start")
	assert.Contains(t, b, byte(msgClock), "should stream clock pulses")
	assert.Contains(t, b, byte(msgStop), "should end with MIDI Stop")
}

func TestClockerStartIsIdempotent(t *testing.T) {
	sb := &safeBuf{}
	c := NewClocker(New(sb, DefaultConfig()), 120)

	require.NoError(t, c.Start())
	require.NoError(t, c.Start()) // second call must be a no-op
	assert.True(t, c.Running())
	require.NoError(t, c.Stop())

	// Only one MIDI Start should have been emitted.
	count := 0
	for _, x := range sb.Bytes() {
		if x == msgStart {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// failClock is a ClockTarget whose Clock() always fails, simulating a device
// that went away mid-playback.
type failClock struct {
	mu    sync.Mutex
	calls int
}

func (f *failClock) Start() error { return nil }
func (f *failClock) Stop() error  { return nil }
func (f *failClock) Clock() error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return errors.New("write: no such device")
}

func TestClockerOnErrorFiresOnceOnClockFailure(t *testing.T) {
	var errs int32
	c := NewClocker(&failClock{}, 6000) // fast pulses
	c.SetOnError(func(error) { atomic.AddInt32(&errs, 1) })

	require.NoError(t, c.Start())
	time.Sleep(15 * time.Millisecond) // many failing pulses elapse
	require.NoError(t, c.Stop())

	assert.Equal(t, int32(1), atomic.LoadInt32(&errs),
		"OnError should report the disconnect exactly once per Start")
}
