// Package sequencer implements a small, device- and UI-agnostic step sequencer
// that drives a pad controller in software. It has a fixed number of tracks
// (each assigned to a pad); each track can be 1..maxBars bars long (16 steps per
// bar) and loops at its own length, so tracks of different lengths run as a
// polymeter. When running it advances a global 16th-note tick and fires the
// programmed pad of each track at that track's current step.
//
// It knows nothing about MIDI or Fyne. Trigger and OnStep may be invoked from
// the internal clock goroutine; Trigger must be concurrency-safe and UIs should
// marshal OnStep onto their main thread.
package sequencer

import (
	"sync"
	"time"
)

const (
	// StepsPerBeat and BeatsPerBar give StepsPerBar (a "bar" = 16 sixteenths).
	StepsPerBeat = 4
	BeatsPerBar  = 4
	StepsPerBar  = StepsPerBeat * BeatsPerBar
)

// Engine is a tracks × (bars×16 steps) step sequencer.
type Engine struct {
	// Trigger fires a pad (by id) at the given velocity. Safe for concurrent use.
	Trigger func(pad int, velocity uint8)
	// OnStep is called each 16th-note tick with the running tick count. A track's
	// current step is tick % (bars*StepsPerBar). Called from the clock goroutine.
	OnStep func(tick int)

	mu        sync.Mutex
	maxTracks int
	maxBars   int
	tracks    int    // active track count
	bars      []int  // [track] bar count (1..maxBars)
	pads      []int  // [track] -> pad id, or -1
	muted     []bool // [track]
	grid      [][]bool
	velocity  uint8
	bpm       float64
	tick      int
	running   bool
	stop      chan struct{}
}

// New allocates a sequencer for maxTracks tracks of up to maxBars bars each.
func New(maxTracks, maxBars int, trigger func(pad int, velocity uint8)) *Engine {
	perTrack := maxBars * StepsPerBar
	e := &Engine{
		Trigger:   trigger,
		maxTracks: maxTracks,
		maxBars:   maxBars,
		tracks:    maxTracks,
		velocity:  100,
		bpm:       120,
		bars:      make([]int, maxTracks),
		pads:      make([]int, maxTracks),
		muted:     make([]bool, maxTracks),
		grid:      make([][]bool, maxTracks),
	}
	for t := range e.grid {
		e.grid[t] = make([]bool, perTrack)
		e.pads[t] = -1
		e.bars[t] = 1
	}
	return e
}

// MaxTracks / MaxBars report the allocated capacities.
func (e *Engine) MaxTracks() int { return e.maxTracks }
func (e *Engine) MaxBars() int   { return e.maxBars }

// SetTempo sets the tempo used to time steps (takes effect on the next step).
func (e *Engine) SetTempo(bpm float64) {
	if bpm <= 0 {
		return
	}
	e.mu.Lock()
	e.bpm = bpm
	e.mu.Unlock()
}

// Tracks reports the active track count.
func (e *Engine) Tracks() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tracks
}

// SetTracks sets the active track count, clamped to [1, maxTracks].
func (e *Engine) SetTracks(n int) {
	if n < 1 {
		n = 1
	}
	if n > e.maxTracks {
		n = e.maxTracks
	}
	e.mu.Lock()
	e.tracks = n
	e.mu.Unlock()
}

// Bars returns a track's bar count.
func (e *Engine) Bars(track int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if track < 0 || track >= e.maxTracks {
		return 1
	}
	return e.bars[track]
}

// SetBars sets a track's bar count, clamped to [1, maxBars].
func (e *Engine) SetBars(track, n int) {
	if n < 1 {
		n = 1
	}
	if n > e.maxBars {
		n = e.maxBars
	}
	e.mu.Lock()
	if track >= 0 && track < e.maxTracks {
		e.bars[track] = n
	}
	e.mu.Unlock()
}

// TrackLen returns a track's length in steps (bars × 16).
func (e *Engine) TrackLen(track int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.trackLenLocked(track)
}

func (e *Engine) trackLenLocked(track int) int {
	if track < 0 || track >= e.maxTracks {
		return StepsPerBar
	}
	return e.bars[track] * StepsPerBar
}

// SetPad assigns a pad id to a track (-1 to unassign).
func (e *Engine) SetPad(track, pad int) {
	e.mu.Lock()
	if track >= 0 && track < e.maxTracks {
		e.pads[track] = pad
	}
	e.mu.Unlock()
}

// Pad returns the pad id assigned to a track, or -1.
func (e *Engine) Pad(track int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if track < 0 || track >= e.maxTracks {
		return -1
	}
	return e.pads[track]
}

// SetMuted sets a track's mute state; muted tracks don't fire.
func (e *Engine) SetMuted(track int, m bool) {
	e.mu.Lock()
	if track >= 0 && track < e.maxTracks {
		e.muted[track] = m
	}
	e.mu.Unlock()
}

// Muted reports whether a track is muted.
func (e *Engine) Muted(track int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return track >= 0 && track < e.maxTracks && e.muted[track]
}

// ToggleMuted flips a track's mute state and returns the new value.
func (e *Engine) ToggleMuted(track int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if track < 0 || track >= e.maxTracks {
		return false
	}
	e.muted[track] = !e.muted[track]
	return e.muted[track]
}

// Toggle flips a step and returns its new state.
func (e *Engine) Toggle(track, step int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.inRange(track, step) {
		return false
	}
	e.grid[track][step] = !e.grid[track][step]
	return e.grid[track][step]
}

// SetStep sets a step on or off.
func (e *Engine) SetStep(track, step int, on bool) {
	e.mu.Lock()
	if e.inRange(track, step) {
		e.grid[track][step] = on
	}
	e.mu.Unlock()
}

// Step reports whether a step is programmed.
func (e *Engine) Step(track, step int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.inRange(track, step) && e.grid[track][step]
}

// Clear turns off every step.
func (e *Engine) Clear() {
	e.mu.Lock()
	for t := range e.grid {
		for s := range e.grid[t] {
			e.grid[t][s] = false
		}
	}
	e.mu.Unlock()
}

func (e *Engine) inRange(track, step int) bool {
	return track >= 0 && track < e.maxTracks && step >= 0 && step < e.maxBars*StepsPerBar
}

// Running reports whether the sequencer is playing.
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// Start begins playback from tick 0. No-op if already running.
func (e *Engine) Start() {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.tick = 0
	e.stop = make(chan struct{})
	stop := e.stop
	e.mu.Unlock()
	go e.run(stop)
}

// Stop halts playback.
func (e *Engine) Stop() {
	e.mu.Lock()
	if e.running {
		close(e.stop)
		e.running = false
	}
	e.mu.Unlock()
}

func stepInterval(bpm float64) time.Duration {
	if bpm <= 0 {
		bpm = 120
	}
	return time.Duration(float64(time.Minute) / (bpm * StepsPerBeat))
}

func (e *Engine) run(stop chan struct{}) {
	next := time.Now()
	for {
		e.mu.Lock()
		tick := e.tick
		bpm := e.bpm
		vel := e.velocity
		fires := make([]int, 0, e.tracks)
		for t := 0; t < e.tracks; t++ {
			step := tick % e.trackLenLocked(t)
			if e.pads[t] >= 0 && !e.muted[t] && e.grid[t][step] {
				fires = append(fires, e.pads[t])
			}
		}
		e.mu.Unlock()

		for _, p := range fires {
			if e.Trigger != nil {
				e.Trigger(p, vel)
			}
		}
		if e.OnStep != nil {
			e.OnStep(tick)
		}

		next = next.Add(stepInterval(bpm))
		select {
		case <-stop:
			return
		case <-time.After(time.Until(next)):
		}

		e.mu.Lock()
		e.tick++
		e.mu.Unlock()
	}
}

// TrackState is one track's persistable state.
type TrackState struct {
	Pad   int    `json:"pad"`
	Bars  int    `json:"bars"`
	Muted bool   `json:"muted"`
	Steps []bool `json:"steps"`
}

// State is a persistable snapshot of the whole sequencer.
type State struct {
	Version int          `json:"version"`
	Tempo   float64      `json:"tempo"`
	Tracks  int          `json:"tracks"` // active track count
	Data    []TrackState `json:"data"`   // one per allocated track
}

// Snapshot captures the sequencer's current state (all allocated tracks).
func (e *Engine) Snapshot() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := State{Version: 1, Tempo: e.bpm, Tracks: e.tracks, Data: make([]TrackState, e.maxTracks)}
	for t := 0; t < e.maxTracks; t++ {
		steps := make([]bool, len(e.grid[t]))
		copy(steps, e.grid[t])
		st.Data[t] = TrackState{Pad: e.pads[t], Bars: e.bars[t], Muted: e.muted[t], Steps: steps}
	}
	return st
}

// Restore applies a snapshot, clamping values to the engine's capacities.
func (e *Engine) Restore(st State) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if st.Tempo > 0 {
		e.bpm = st.Tempo
	}
	if st.Tracks >= 1 && st.Tracks <= e.maxTracks {
		e.tracks = st.Tracks
	}
	for t := 0; t < e.maxTracks; t++ {
		if t >= len(st.Data) {
			continue
		}
		ts := st.Data[t]
		e.pads[t] = ts.Pad
		if ts.Bars >= 1 && ts.Bars <= e.maxBars {
			e.bars[t] = ts.Bars
		}
		e.muted[t] = ts.Muted
		for s := range e.grid[t] {
			e.grid[t][s] = s < len(ts.Steps) && ts.Steps[s]
		}
	}
}
