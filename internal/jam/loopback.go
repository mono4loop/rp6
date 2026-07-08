package jam

import "sync"

// loopback is an in-memory Transport used for tests and local experiments:
// frames broadcast on one end arrive on the other end's Incoming channel. It
// never touches the network, so the Engine can be exercised by plain `go test`.
type loopback struct {
	out    chan []byte // frames we send (the paired end reads these)
	in     chan []byte // frames we receive (the paired end sends these)
	closed chan struct{}
	once   sync.Once
}

// NewLoopback returns two Transports wired to each other: a frame broadcast on
// one is delivered to the other's Incoming channel.
func NewLoopback() (Transport, Transport) {
	a2b := make(chan []byte, 64)
	b2a := make(chan []byte, 64)
	a := &loopback{out: a2b, in: b2a, closed: make(chan struct{})}
	b := &loopback{out: b2a, in: a2b, closed: make(chan struct{})}
	return a, b
}

func (l *loopback) Broadcast(frame []byte) error {
	cp := append([]byte(nil), frame...) // copy: caller may reuse the buffer
	select {
	case l.out <- cp:
		return nil
	case <-l.closed:
		return ErrClosed
	}
}

func (l *loopback) Incoming() <-chan []byte { return l.in }

func (l *loopback) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}
