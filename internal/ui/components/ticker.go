package components

import (
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
)

// coalescedTicker runs tick on the Fyne main thread every interval until stop is
// closed. It coalesces: while a previous tick is still queued on the render loop
// it skips firing, so a slow frame can't grow the UI-update queue unboundedly.
// tick runs inside fyne.Do, so it may touch widgets directly.
//
// It is the shared engine behind the Knob's pending-flash blink and the LED's
// breathing pulse (both background animations that differ only in interval and
// per-tick work). Run it in its own goroutine: go coalescedTicker(...).
func coalescedTicker(interval time.Duration, stop <-chan struct{}, tick func()) {
	t := time.NewTicker(interval)
	defer t.Stop()
	var pending atomic.Bool // never queue a tick while the previous one is pending
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if pending.Load() {
				continue // the render loop hasn't drained the last tick yet
			}
			pending.Store(true)
			fyne.Do(func() {
				tick()
				pending.Store(false)
			})
		}
	}
}
