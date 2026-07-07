package p6

import (
	"sync"
	"time"
)

// clocksPerQuarter is the MIDI standard: 24 timing-clock pulses per quarter note.
const clocksPerQuarter = 24

// ClockInterval returns the delay between MIDI timing-clock pulses for a given
// tempo in beats per minute. A non-positive bpm defaults to 120.
func ClockInterval(bpm float64) time.Duration {
	if bpm <= 0 {
		bpm = 120
	}
	return time.Duration(float64(time.Minute) / (bpm * clocksPerQuarter))
}

// ClockTarget is the transport surface Clocker drives: the MIDI Start/Stop
// transport plus timing-clock pulses. Both *Device and the emulator satisfy it
// (it is a subset of Controller), so a Clocker can drive either.
type ClockTarget interface {
	Start() error
	Stop() error
	Clock() error
}

// Clocker drives a P-6 that is slaved to USB clock (SYnC=USB). Start emits a
// MIDI Start and then streams timing-clock pulses at the configured tempo so
// the sequencer actually advances; Stop halts the pulses and emits MIDI Stop.
// The tempo can be changed while running.
type Clocker struct {
	dev ClockTarget

	mu      sync.Mutex
	bpm     float64
	running bool
	stop    chan struct{}

	// onError, if set, is called (from the streaming goroutine) the first time
	// a clock pulse fails to send — e.g. the P-6 was unplugged mid-playback.
	// The clock keeps running so the app can decide whether to stop/reconnect.
	onError func(error)
}

// NewClocker returns a Clocker for dev at the given tempo (bpm).
func NewClocker(dev ClockTarget, bpm float64) *Clocker {
	if bpm <= 0 {
		bpm = 120
	}
	return &Clocker{dev: dev, bpm: bpm}
}

// SetOnError installs a callback invoked (once per Start, from the streaming
// goroutine) when a timing-clock pulse fails to send — typically the device
// disappearing mid-playback. Set it before calling Start.
func (c *Clocker) SetOnError(fn func(error)) {
	c.mu.Lock()
	c.onError = fn
	c.mu.Unlock()
}

// Running reports whether the clock is currently streaming.
func (c *Clocker) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// SetTempo updates the tempo; if running, it takes effect on the next pulse.
func (c *Clocker) SetTempo(bpm float64) {
	if bpm <= 0 {
		return
	}
	c.mu.Lock()
	c.bpm = bpm
	c.mu.Unlock()
}

// Start emits MIDI Start and begins streaming clock pulses. It is a no-op if
// already running.
func (c *Clocker) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.stop = make(chan struct{})
	stop := c.stop
	c.mu.Unlock()

	if err := c.dev.Start(); err != nil {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		return err
	}
	go c.run(stop)
	return nil
}

func (c *Clocker) run(stop <-chan struct{}) {
	reported := false
	for {
		c.mu.Lock()
		bpm := c.bpm
		onErr := c.onError
		c.mu.Unlock()

		select {
		case <-stop:
			return
		case <-time.After(ClockInterval(bpm)):
			if err := c.dev.Clock(); err != nil && !reported {
				reported = true
				if onErr != nil {
					onErr(err)
				}
			}
		}
	}
}

// Stop halts clock pulses (if running) and emits MIDI Stop.
func (c *Clocker) Stop() error {
	c.mu.Lock()
	if c.running {
		close(c.stop)
		c.running = false
	}
	c.mu.Unlock()
	return c.dev.Stop()
}
