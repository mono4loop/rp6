//go:build !nojam && !js && !android && !ios

package webrtc

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	pion "github.com/pion/webrtc/v4"
)

// peer is one remote participant: a pion PeerConnection plus the single
// unreliable data channel over which pad frames flow.
type peer struct {
	t         *Transport
	id        string
	initiator bool

	pc *pion.PeerConnection

	mu        sync.Mutex
	dc        *pion.DataChannel
	remoteSet bool
	pending   []pion.ICECandidateInit
	counted   bool // whether this peer is currently counted as connected

	rttStop   chan struct{}
	closeOnce sync.Once
}

func newPeer(t *Transport, id string, initiator bool) (*peer, error) {
	pc, err := pion.NewPeerConnection(pion.Configuration{ICEServers: t.icesv})
	if err != nil {
		return nil, err
	}
	p := &peer{t: t, id: id, initiator: initiator, pc: pc, rttStop: make(chan struct{})}

	pc.OnICECandidate(func(c *pion.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		p.sendSignal(signalPayload{Candidate: &init})
	})
	pc.OnConnectionStateChange(func(s pion.PeerConnectionState) {
		log.Printf("jam/webrtc: peer %s: %s", id, s)
		if s == pion.PeerConnectionStateFailed || s == pion.PeerConnectionStateClosed {
			t.removePeer(id)
		}
	})

	go p.logRTT()

	if initiator {
		ordered := false
		var maxRetransmits uint16 = 0
		dc, err := pc.CreateDataChannel("jam", &pion.DataChannelInit{
			Ordered:        &ordered,
			MaxRetransmits: &maxRetransmits,
		})
		if err != nil {
			_ = pc.Close()
			return nil, err
		}
		p.wireDC(dc)
	} else {
		pc.OnDataChannel(func(dc *pion.DataChannel) { p.wireDC(dc) })
	}
	return p, nil
}

// startOffer (initiator only) creates and sends the SDP offer.
func (p *peer) startOffer() {
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		log.Printf("jam/webrtc: peer %s: create offer: %v", p.id, err)
		return
	}
	if err := p.pc.SetLocalDescription(offer); err != nil {
		log.Printf("jam/webrtc: peer %s: set local (offer): %v", p.id, err)
		return
	}
	p.sendSignal(signalPayload{SDP: &offer})
}

func (p *peer) handleSignal(pl signalPayload) {
	switch {
	case pl.SDP != nil:
		if err := p.pc.SetRemoteDescription(*pl.SDP); err != nil {
			log.Printf("jam/webrtc: peer %s: set remote: %v", p.id, err)
			return
		}
		p.flushPending()
		if pl.SDP.Type == pion.SDPTypeOffer {
			answer, err := p.pc.CreateAnswer(nil)
			if err != nil {
				log.Printf("jam/webrtc: peer %s: create answer: %v", p.id, err)
				return
			}
			if err := p.pc.SetLocalDescription(answer); err != nil {
				log.Printf("jam/webrtc: peer %s: set local (answer): %v", p.id, err)
				return
			}
			p.sendSignal(signalPayload{SDP: &answer})
		}
	case pl.Candidate != nil:
		p.mu.Lock()
		if !p.remoteSet {
			p.pending = append(p.pending, *pl.Candidate) // apply once remote desc is set
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		if err := p.pc.AddICECandidate(*pl.Candidate); err != nil {
			log.Printf("jam/webrtc: peer %s: add candidate: %v", p.id, err)
		}
	}
}

// flushPending applies ICE candidates that arrived before the remote
// description was set.
func (p *peer) flushPending() {
	p.mu.Lock()
	p.remoteSet = true
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()
	for _, c := range pending {
		if err := p.pc.AddICECandidate(c); err != nil {
			log.Printf("jam/webrtc: peer %s: add pending candidate: %v", p.id, err)
		}
	}
}

func (p *peer) wireDC(dc *pion.DataChannel) {
	p.mu.Lock()
	p.dc = dc
	p.mu.Unlock()
	dc.OnOpen(func() {
		log.Printf("jam/webrtc: peer %s: data channel open", p.id)
		p.markConnected()
	})
	dc.OnClose(func() { p.markDisconnected() })
	dc.OnMessage(func(msg pion.DataChannelMessage) {
		frame := append([]byte(nil), msg.Data...)
		p.t.deliver(frame)
	})
}

// markConnected / markDisconnected keep the transport's live-peer count in sync
// (counting a peer only once its data channel is actually usable).
func (p *peer) markConnected() {
	p.mu.Lock()
	if p.counted {
		p.mu.Unlock()
		return
	}
	p.counted = true
	p.mu.Unlock()
	p.t.bumpPeers(1)
}

func (p *peer) markDisconnected() {
	p.mu.Lock()
	if !p.counted {
		p.mu.Unlock()
		return
	}
	p.counted = false
	p.mu.Unlock()
	p.t.bumpPeers(-1)
}

func (p *peer) send(frame []byte) {
	p.mu.Lock()
	dc := p.dc
	p.mu.Unlock()
	if dc == nil || dc.ReadyState() != pion.DataChannelStateOpen {
		return
	}
	if err := dc.Send(frame); err != nil {
		log.Printf("jam/webrtc: peer %s: send: %v", p.id, err)
	}
}

func (p *peer) sendSignal(pl signalPayload) {
	data, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = p.t.sig(sigMsg{Type: "signal", To: p.id, Data: data})
}

func (p *peer) close() {
	p.closeOnce.Do(func() {
		close(p.rttStop)
	})
	p.markDisconnected()
	p.mu.Lock()
	dc := p.dc
	p.dc = nil
	p.mu.Unlock()
	if dc != nil {
		_ = dc.Close()
	}
	if p.pc != nil {
		_ = p.pc.Close()
	}
}

// logRTT periodically reports the round-trip time to this peer (the latency you
// feel on remote pad hits) as a min/avg/max over each window, so jitter is
// obvious at a glance. It samples every 2s and reports roughly every 10s, and
// stays quiet until the connection is up.
func (p *peer) logRTT() {
	const sampleEvery = 2 * time.Second
	const perReport = 5 // ~10s
	t := time.NewTicker(sampleEvery)
	defer t.Stop()
	var n int
	var sum, min, max float64
	for {
		select {
		case <-p.rttStop:
			return
		case <-t.C:
			rtt, ok := p.rtt()
			if !ok {
				continue
			}
			ms := rtt * 1000
			if n == 0 || ms < min {
				min = ms
			}
			if ms > max {
				max = ms
			}
			sum += ms
			n++
			if n >= perReport {
				log.Printf("jam/webrtc: peer %s: rtt min %.0f avg %.0f max %.0fms", p.id, min, sum/float64(n), max)
				n, sum, min, max = 0, 0, 0, 0
			}
		}
	}
}

// rtt returns the current round-trip time (seconds) of the nominated ICE
// candidate pair, if the connection is established.
func (p *peer) rtt() (float64, bool) {
	for _, s := range p.pc.GetStats() {
		pair, ok := s.(pion.ICECandidatePairStats)
		if ok && pair.Nominated && pair.State == pion.StatsICECandidatePairStateSucceeded {
			return pair.CurrentRoundTripTime, true
		}
	}
	return 0, false
}
