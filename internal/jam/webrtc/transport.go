//go:build !nojam && !js && !android && !ios

package webrtc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	pion "github.com/pion/webrtc/v4"
	"golang.org/x/net/websocket"

	"github.com/mono4loop/rp6/internal/jam"
)

// defaultICE is a public STUN server used for NAT traversal when the caller
// supplies none. TURN (for symmetric NATs) can be added via Config.ICEServers.
var defaultICE = []string{"stun:stun.l.google.com:19302"}

// Reconnect / liveness tuning.
const (
	// clientIdleTimeout drops a signaling connection that goes silent (the
	// server pings every ~30s, so no traffic for this long means a dead link),
	// which triggers a reconnect.
	clientIdleTimeout = 90 * time.Second
	reconnectBaseWait = time.Second
	reconnectMaxWait  = 30 * time.Second
)

// Transport implements jam.Transport.
var _ jam.Transport = (*Transport)(nil)

// Config configures a WebRTC jam Transport.
type Config struct {
	// Signaling is the ws:// or wss:// URL of an rp6-signal signaling server.
	Signaling string
	// Room is the session code; peers sharing it are connected to each other.
	Room string
	// Name is an optional human label used only in logs.
	Name string
	// ICEServers overrides the default STUN/TURN server list.
	ICEServers []string
	// OnPeers, if set, is called with the current number of connected peers
	// (those with an open data channel) whenever it changes. It may fire from a
	// background goroutine.
	OnPeers func(count int)
}

// Transport is a mesh of WebRTC peer connections implementing jam.Transport.
// Pad frames are exchanged over an unreliable, unordered data channel (lowest
// latency; a dropped live hit is discarded rather than retransmitted). Peers
// find each other through a signaling server that relays only the WebRTC
// handshake — pad data itself flows directly peer-to-peer, DTLS-encrypted.
type Transport struct {
	cfg   Config
	icesv []pion.ICEServer
	id    string
	url   string // full signaling URL including the /s/<token> path

	ws   *websocket.Conn
	wsMu sync.Mutex // guards ws (pointer swaps on reconnect) + serializes sends

	mu    sync.Mutex
	peers map[string]*peer

	connected int // peers with an open data channel

	in     chan []byte
	closed chan struct{}
	once   sync.Once
}

// Dial connects to the signaling server, joins the room, and returns a running
// Transport. Peer connections are established asynchronously as members appear,
// and the signaling link auto-reconnects (with backoff) if it drops.
func Dial(cfg Config) (*Transport, error) {
	if cfg.Signaling == "" {
		return nil, fmt.Errorf("jam/webrtc: empty signaling address")
	}
	if cfg.Room == "" {
		return nil, fmt.Errorf("jam/webrtc: empty room")
	}
	ice := cfg.ICEServers
	if len(ice) == 0 {
		ice = defaultICE
	}

	t := &Transport{
		cfg:   cfg,
		icesv: []pion.ICEServer{{URLs: ice}},
		id:    randomID(),
		// The session token rides in the URL path (/s/<token>): it's a bearer
		// capability the signaling server validates and uses as the room.
		url:    NormalizeURL(cfg.Signaling) + "/s/" + cfg.Room,
		peers:  map[string]*peer{},
		in:     make(chan []byte, 256),
		closed: make(chan struct{}),
	}

	// The first connection is synchronous so Dial reports an unreachable server
	// / bad URL immediately; later drops are recovered by supervise.
	ws, err := t.connect()
	if err != nil {
		return nil, err
	}
	go t.supervise(ws)
	return t, nil
}

// connect opens a signaling WebSocket and joins the room.
func (t *Transport) connect() (*websocket.Conn, error) {
	ws, err := websocket.Dial(t.url, "", "http://localhost/")
	if err != nil {
		return nil, fmt.Errorf("jam/webrtc: dial signaling %q: %w", t.url, err)
	}
	t.setWS(ws)
	if err := t.sig(sigMsg{Type: "join", ID: t.id}); err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("jam/webrtc: join: %w", err)
	}
	return ws, nil
}

// supervise runs the signaling read loop and, whenever the link drops before
// Close, tears down the (now-stale) peer connections and reconnects with capped
// exponential backoff. On reconnect the server sends a fresh member list, so
// the mesh re-forms automatically.
func (t *Transport) supervise(ws *websocket.Conn) {
	for {
		t.readLoop(ws) // blocks until the connection drops or Close

		select {
		case <-t.closed:
			return
		default:
		}

		t.resetPeers() // stale after a signaling drop; rebuilt on rejoin
		delay := reconnectBaseWait
		for {
			log.Printf("jam/webrtc: signaling lost — reconnecting in %s", delay)
			select {
			case <-t.closed:
				return
			case <-time.After(delay):
			}
			next, err := t.connect()
			if err == nil {
				log.Printf("jam/webrtc: reconnected to signaling")
				ws = next
				break
			}
			log.Printf("jam/webrtc: reconnect failed: %v", err)
			if delay *= 2; delay > reconnectMaxWait {
				delay = reconnectMaxWait
			}
		}
	}
}

// Broadcast sends frame to every peer whose data channel is open.
func (t *Transport) Broadcast(frame []byte) error {
	t.mu.Lock()
	targets := make([]*peer, 0, len(t.peers))
	for _, p := range t.peers {
		targets = append(targets, p)
	}
	t.mu.Unlock()
	for _, p := range targets {
		p.send(frame)
	}
	return nil
}

// Incoming returns the channel of frames received from peers.
func (t *Transport) Incoming() <-chan []byte { return t.in }

// Close tears down all peer connections and the signaling link (idempotent).
func (t *Transport) Close() error {
	t.once.Do(func() {
		close(t.closed)
		t.mu.Lock()
		peers := make([]*peer, 0, len(t.peers))
		for _, p := range t.peers {
			peers = append(peers, p)
		}
		t.peers = map[string]*peer{}
		t.mu.Unlock()
		for _, p := range peers {
			p.close() // outside the lock: p.close -> bumpPeers re-locks t.mu
		}
		t.wsMu.Lock()
		if t.ws != nil {
			_ = t.ws.Close() // unblocks the read loop -> supervise sees closed and exits
		}
		t.wsMu.Unlock()
	})
	return nil
}

// ---- signaling ----

type sigMsg struct {
	Type  string          `json:"type"`
	Room  string          `json:"room,omitempty"`
	ID    string          `json:"id,omitempty"`
	Peers []string        `json:"peers,omitempty"`
	To    string          `json:"to,omitempty"`
	From  string          `json:"from,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

type signalPayload struct {
	SDP       *pion.SessionDescription `json:"sdp,omitempty"`
	Candidate *pion.ICECandidateInit   `json:"candidate,omitempty"`
}

func (t *Transport) setWS(ws *websocket.Conn) {
	t.wsMu.Lock()
	t.ws = ws
	t.wsMu.Unlock()
}

func (t *Transport) sig(m sigMsg) error {
	t.wsMu.Lock()
	defer t.wsMu.Unlock()
	if t.ws == nil {
		return fmt.Errorf("jam/webrtc: not connected")
	}
	return websocket.JSON.Send(t.ws, m)
}

// readLoop reads signaling messages on ws until it errors or Close. It sets an
// idle read deadline so a silently-dead link is detected and reconnected.
func (t *Transport) readLoop(ws *websocket.Conn) {
	for {
		_ = ws.SetReadDeadline(time.Now().Add(clientIdleTimeout))
		var m sigMsg
		if err := websocket.JSON.Receive(ws, &m); err != nil {
			select {
			case <-t.closed:
			default:
				log.Printf("jam/webrtc: signaling read: %v", err)
			}
			return
		}
		t.handleSig(m)
	}
}

func (t *Transport) handleSig(m sigMsg) {
	switch m.Type {
	case "ping":
		_ = t.sig(sigMsg{Type: "pong"}) // keepalive: refresh the server's read deadline
	case "peers":
		// Existing members: we are the newcomer, so we initiate the offer to
		// each (deterministic initiator avoids offer glare).
		for _, id := range m.Peers {
			if id == t.id {
				continue
			}
			p := t.ensurePeer(id, true)
			if p != nil {
				p.startOffer()
			}
		}
	case "peer-join":
		if m.ID != "" && m.ID != t.id {
			t.ensurePeer(m.ID, false) // they'll offer to us; we answer
		}
	case "peer-leave":
		t.removePeer(m.ID)
	case "signal":
		if m.From == "" || m.From == t.id {
			return
		}
		p := t.ensurePeer(m.From, false)
		if p == nil {
			return
		}
		var pl signalPayload
		if err := json.Unmarshal(m.Data, &pl); err != nil {
			return
		}
		p.handleSignal(pl)
	}
}

func (t *Transport) ensurePeer(id string, initiator bool) *peer {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p, ok := t.peers[id]; ok {
		return p
	}
	p, err := newPeer(t, id, initiator)
	if err != nil {
		log.Printf("jam/webrtc: new peer %s: %v", id, err)
		return nil
	}
	t.peers[id] = p
	return p
}

func (t *Transport) deliver(frame []byte) {
	select {
	case t.in <- frame:
	case <-t.closed:
	default:
		// Buffer full — drop. A stale live hit is worse than a missing one.
	}
}

func (t *Transport) removePeer(id string) {
	t.mu.Lock()
	p := t.peers[id]
	delete(t.peers, id)
	t.mu.Unlock()
	if p != nil {
		p.close()
	}
}

// resetPeers closes and forgets every peer connection (used on a signaling
// reconnect: the old connections are stale, and the fresh member list from the
// server rebuilds the mesh). Firing each peer's close decrements the connected
// count, so OnPeers reports 0 until peers re-form.
func (t *Transport) resetPeers() {
	t.mu.Lock()
	peers := make([]*peer, 0, len(t.peers))
	for _, p := range t.peers {
		peers = append(peers, p)
	}
	t.peers = map[string]*peer{}
	t.mu.Unlock()
	for _, p := range peers {
		p.close()
	}
}

// bumpPeers adjusts the connected-peer count and reports it to Config.OnPeers.
func (t *Transport) bumpPeers(delta int) {
	t.mu.Lock()
	t.connected += delta
	if t.connected < 0 {
		t.connected = 0
	}
	n := t.connected
	cb := t.cfg.OnPeers
	t.mu.Unlock()
	if cb != nil {
		cb(n)
	}
}

func randomID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// NormalizeURL turns a user-entered signaling address into a ws:// or wss:// URL
// (trailing slash trimmed). A bare host or host:port with no scheme becomes
// wss:// — secure by default; http/https map to ws/wss; ws/wss are kept as-is.
func NormalizeURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(s, "ws://"), strings.HasPrefix(s, "wss://"):
		// already a WebSocket URL
	case strings.HasPrefix(s, "http://"):
		s = "ws://" + strings.TrimPrefix(s, "http://")
	case strings.HasPrefix(s, "https://"):
		s = "wss://" + strings.TrimPrefix(s, "https://")
	default:
		s = "wss://" + s // bare FQDN / host -> secure by default
	}
	return strings.TrimRight(s, "/")
}
