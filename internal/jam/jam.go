// Package jam implements the transport-agnostic core of rp6's shared jam
// sessions: peers broadcast their live pad hits to each other so everyone hears
// (and sees a brief blink for) what the others play.
//
// Like internal/effects and internal/sequencer, this package is deliberately
// generic — it imports no Fyne, no p6, and no networking library. The app wires
// callbacks and supplies a Transport; the concrete network layer (WebRTC) lives
// in the build-tagged internal/jam/webrtc sub-package, and an in-memory
// Loopback transport makes the whole thing unit-testable with no network.
package jam

import (
	"errors"
	"sync"
)

// ErrClosed is returned by a Transport once it has been closed.
var ErrClosed = errors.New("jam: transport closed")

// Transport is the pluggable network layer for a jam session. Implementations
// deliver frames received from remote peers on the channel returned by Incoming
// and send local frames to every connected peer via Broadcast. All methods must
// be safe for concurrent use; Incoming's channel is closed when the transport
// shuts down.
type Transport interface {
	// Broadcast sends frame to every connected peer (best-effort).
	Broadcast(frame []byte) error
	// Incoming returns the channel of frames received from peers.
	Incoming() <-chan []byte
	// Close shuts the transport down and releases its resources.
	Close() error
}

// Engine runs a jam session over a Transport: it encodes local events into
// frames for Broadcast and decodes remote frames into callback invocations.
//
// Callbacks fire on the transport's read goroutine, so the app is responsible
// for marshalling any UI work (e.g. through fyne.Do). Applying a remote event
// must NOT feed back into a Send* call, or hits would echo endlessly.
type Engine struct {
	// OnPad is invoked for each remote pad hit (pad is a 0-based id, velocity
	// 0..127). nil ignores incoming pad hits.
	OnPad func(pad int, velocity uint8)

	t       Transport
	out     chan []byte // outgoing frames, drained by the send goroutine
	mu      sync.Mutex
	started bool
	done    chan struct{}
}

// outBuffer bounds the queue of unsent frames. It's generous — live pad hits
// are tiny and the send goroutine drains promptly — so it only fills if the
// network truly stalls, at which point new hits are dropped rather than
// blocking the player.
const outBuffer = 256

// New returns an Engine over t. Call Start to begin sending/receiving.
func New(t Transport) *Engine {
	return &Engine{t: t, out: make(chan []byte, outBuffer), done: make(chan struct{})}
}

// Start begins the send and receive loops in background goroutines (idempotent).
func (e *Engine) Start() {
	e.mu.Lock()
	if e.started || e.t == nil {
		e.mu.Unlock()
		return
	}
	e.started = true
	e.mu.Unlock()
	go e.recv()
	go e.sendLoop()
}

// sendLoop drains queued frames to the transport off the caller's goroutine, so
// broadcasting never blocks the UI/audio path that produced a hit.
func (e *Engine) sendLoop() {
	for {
		select {
		case <-e.done:
			return
		case frame := <-e.out:
			_ = e.t.Broadcast(frame)
		}
	}
}

func (e *Engine) recv() {
	in := e.t.Incoming()
	for {
		select {
		case <-e.done:
			return
		case frame, ok := <-in:
			if !ok {
				return
			}
			e.dispatch(frame)
		}
	}
}

func (e *Engine) dispatch(frame []byte) {
	msg, ok := decode(frame)
	if !ok {
		return
	}
	switch msg.kind {
	case kindPad:
		if e.OnPad != nil {
			e.OnPad(int(msg.pad), msg.velocity)
		}
	}
}

// SendPad queues a local pad hit for broadcast to all peers. It never blocks
// the caller (the enqueue is a non-blocking channel send) and never surfaces a
// transport error: a dropped live hit — because the network stalled and the
// send queue is full — is preferable to stalling the audio/UI path that
// produced it. Out-of-range pads are ignored.
func (e *Engine) SendPad(pad int, velocity uint8) {
	if e.t == nil || pad < 0 || pad > 255 {
		return
	}
	select {
	case e.out <- encodePad(uint8(pad), velocity):
	default:
		// Send queue full (network stalled) — drop this hit rather than block.
	}
}

// Close stops the receive loop and closes the underlying transport (idempotent).
func (e *Engine) Close() error {
	e.mu.Lock()
	select {
	case <-e.done:
	default:
		close(e.done)
	}
	e.mu.Unlock()
	if e.t != nil {
		return e.t.Close()
	}
	return nil
}
