// Package effects implements a small, device- and UI-agnostic effects engine
// for a pad controller. Each pad has a fixed number of effect slots; the only
// effect implemented so far is Roll, a tempo-synced retrigger.
//
// The engine knows nothing about MIDI or Fyne: it fires pads through a Trigger
// callback (which may be invoked from a background goroutine and must therefore
// be safe to call concurrently) and reads tempo via SetTempo.
package effects

import (
	"sync"
	"time"
)

// Slots is the number of effect slots per pad.
const Slots = 4

// Kind identifies an effect type.
type Kind int

const (
	KindNone Kind = iota
	KindRoll
)

// Name returns a short label for the kind.
func (k Kind) Name() string {
	switch k {
	case KindRoll:
		return "ROLL"
	default:
		return "—"
	}
}

// Division is a tempo-synced note division used by the Roll effect.
type Division struct {
	Name  string
	Beats float64 // length in quarter-note beats
}

// Divisions lists the selectable roll rates, coarse to fine.
var Divisions = []Division{
	{"1", 4},
	{"1/2", 2},
	{"1/4", 1},
	{"1/4T", 2.0 / 3},
	{"1/8", 0.5},
	{"1/8T", 1.0 / 3},
	{"1/16", 0.25},
	{"1/16T", 1.0 / 6},
	{"1/32", 0.125},
}

// DefaultDiv is the index of the default roll division (1/16).
const DefaultDiv = 6

// Interval returns the time between retriggers for a tempo and division index.
func Interval(bpm float64, div int) time.Duration {
	if bpm <= 0 {
		bpm = 120
	}
	if div < 0 || div >= len(Divisions) {
		div = DefaultDiv
	}
	beat := float64(time.Minute) / bpm // ns per quarter-note
	return time.Duration(beat * Divisions[div].Beats)
}

// State is a snapshot of a pad's effect configuration.
type State struct {
	Slots   [Slots]Kind
	RollDiv int
}

type padState struct {
	slots   [Slots]Kind
	rollDiv int
}

// Engine tracks per-pad effect slots and runs active rolls.
type Engine struct {
	// Trigger fires the given pad. It may be called from a background
	// goroutine and must be safe for concurrent use.
	Trigger func(pad int)

	mu      sync.Mutex
	bpm     float64
	states  map[int]*padState
	rolling map[int]chan struct{}
}

// New returns an engine that fires pads through trigger.
func New(trigger func(pad int)) *Engine {
	return &Engine{
		Trigger: trigger,
		bpm:     120,
		states:  map[int]*padState{},
		rolling: map[int]chan struct{}{},
	}
}

// SetTempo updates the tempo used to time rolls (takes effect on the next tick).
func (e *Engine) SetTempo(bpm float64) {
	if bpm <= 0 {
		return
	}
	e.mu.Lock()
	e.bpm = bpm
	e.mu.Unlock()
}

func (e *Engine) stateLocked(pad int) *padState {
	s := e.states[pad]
	if s == nil {
		s = &padState{rollDiv: DefaultDiv}
		e.states[pad] = s
	}
	return s
}

// SetSlot sets the effect kind in one slot of a pad.
func (e *Engine) SetSlot(pad, slot int, k Kind) {
	if slot < 0 || slot >= Slots {
		return
	}
	e.mu.Lock()
	e.stateLocked(pad).slots[slot] = k
	stillRoll := e.hasKindLocked(pad, KindRoll)
	e.mu.Unlock()
	if !stillRoll {
		e.stopRoll(pad) // removing the last Roll stops an active roll
	}
}

// SetRollDiv sets a pad's roll division (index into Divisions).
func (e *Engine) SetRollDiv(pad, div int) {
	if div < 0 || div >= len(Divisions) {
		return
	}
	e.mu.Lock()
	e.stateLocked(pad).rollDiv = div
	e.mu.Unlock()
}

// State returns a snapshot of a pad's configuration.
func (e *Engine) State(pad int) State {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.stateLocked(pad)
	return State{Slots: s.slots, RollDiv: s.rollDiv}
}

func (e *Engine) hasKindLocked(pad int, k Kind) bool {
	s := e.states[pad]
	if s == nil {
		return false
	}
	for _, sk := range s.slots {
		if sk == k {
			return true
		}
	}
	return false
}

// HasEffect reports whether any of a pad's slots holds kind k.
func (e *Engine) HasEffect(pad int, k Kind) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hasKindLocked(pad, k)
}

// IsRolling reports whether a pad is currently rolling.
func (e *Engine) IsRolling(pad int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rolling[pad] != nil
}

// Tap handles a pad press: toggles the roll if the pad has a Roll effect,
// otherwise fires it once.
func (e *Engine) Tap(pad int) {
	e.mu.Lock()
	rolling := e.rolling[pad] != nil
	hasRoll := e.hasKindLocked(pad, KindRoll)
	e.mu.Unlock()

	switch {
	case hasRoll && rolling:
		e.stopRoll(pad)
	case hasRoll && !rolling:
		e.startRoll(pad)
	default:
		e.fire(pad)
	}
}

// StopAll stops every active roll.
func (e *Engine) StopAll() {
	e.mu.Lock()
	chans := e.rolling
	e.rolling = map[int]chan struct{}{}
	e.mu.Unlock()
	for _, ch := range chans {
		close(ch)
	}
}

func (e *Engine) fire(pad int) {
	if e.Trigger != nil {
		e.Trigger(pad)
	}
}

func (e *Engine) startRoll(pad int) {
	e.mu.Lock()
	if e.rolling[pad] != nil {
		e.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	e.rolling[pad] = stop
	e.mu.Unlock()
	go e.roll(pad, stop)
}

func (e *Engine) stopRoll(pad int) {
	e.mu.Lock()
	if ch := e.rolling[pad]; ch != nil {
		close(ch)
		delete(e.rolling, pad)
	}
	e.mu.Unlock()
}

func (e *Engine) roll(pad int, stop chan struct{}) {
	for {
		e.fire(pad)

		e.mu.Lock()
		bpm := e.bpm
		div := e.stateLocked(pad).rollDiv
		still := e.hasKindLocked(pad, KindRoll)
		e.mu.Unlock()
		if !still {
			e.stopRoll(pad)
			return
		}

		select {
		case <-stop:
			return
		case <-time.After(Interval(bpm, div)):
		}
	}
}
