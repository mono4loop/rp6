package main

import (
	"sync"

	"github.com/mono4loop/rp6/internal/audio"
)

// meterSource feeds the master meter a 0..1 level each animation frame.
type meterSource interface {
	level() float64
	step() // advance per-frame state (e.g. decay); no-op for live audio
}

// activitySource is the fallback meter driver: it rises when pads are triggered
// (bump) and decays each frame. Used when live audio capture is unavailable.
type activitySource struct {
	mu  sync.Mutex
	lvl float64
}

func (a *activitySource) bump(v float64) {
	a.mu.Lock()
	if v > a.lvl {
		a.lvl = v
	}
	a.mu.Unlock()
}

func (a *activitySource) step() {
	a.mu.Lock()
	a.lvl *= 0.86
	if a.lvl < 0.005 {
		a.lvl = 0
	}
	a.mu.Unlock()
}

func (a *activitySource) level() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lvl
}

// audioFloorDB is the bottom of the meter's dB range; louder output fills more
// of the meter. Tune for taste (lower = more sensitive/fuller).
const audioFloorDB = -42

// audioSource drives the meter from real captured audio (accurate VU), mapped
// onto a dB scale so the meter uses its full range.
type audioSource struct{ m *audio.Meter }

func (a *audioSource) step() {}
func (a *audioSource) level() float64 {
	return audio.NormDB(a.m.Level(), audioFloorDB)
}
