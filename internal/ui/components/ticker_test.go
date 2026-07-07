package components

import (
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCoalescedTicker verifies the shared animation engine fires its tick
// periodically on the Fyne thread and stops for good once stop is closed.
func TestCoalescedTicker(t *testing.T) {
	test.NewApp()

	var n atomic.Int32
	stop := make(chan struct{})
	go coalescedTicker(2*time.Millisecond, stop, func() { n.Add(1) })

	require.Eventually(t, func() bool { return n.Load() >= 3 }, time.Second, time.Millisecond,
		"ticker should fire repeatedly")

	close(stop)
	// Let the goroutine observe the close, then confirm it stopped firing.
	time.Sleep(20 * time.Millisecond)
	after := n.Load()
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, after, n.Load(), "ticker must stop firing after stop is closed")
}
