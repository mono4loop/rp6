// Package signal implements the tiny WebSocket signaling hub for rp6 shared jam
// sessions. It relays the WebRTC handshake (SDP offers/answers and ICE
// candidates) between peers that join the same session and never sees the
// pad/jam data itself — that flows directly peer-to-peer over WebRTC.
//
// The session token is carried in the URL path (/s/<token>) and acts as a
// bearer capability: only clients that know the full URL can connect at all,
// and every other path is rejected outright (404) before any work is done.
// Combined with per-IP connection rate limiting, global/per-room connection
// caps, a message-size cap and read deadlines, this keeps an internet-facing
// server from being trivially squatted, flooded or OOM'd.
//
// It has no Fyne, no p6 and no WebRTC dependency (just x/net/websocket), so it
// builds everywhere and is embeddable (see cmd/rp6-signal and the WebRTC
// transport's integration test).
package signal

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"
)

// sessionPrefix is the URL path prefix under which a session token lives.
const sessionPrefix = "/s/"

// tokenRE bounds a session token's charset and length: enough to reject junk /
// oversized paths and scanners, without pinning the exact code format. rp6's
// NewCode() (base32 groups joined by dashes) satisfies it.
var tokenRE = regexp.MustCompile(`^[a-z2-9]{4,}(-[a-z2-9]{4,})*$`)

// Defaults for Config's zero values.
const (
	defaultMaxClients  = 500
	defaultMaxPerRoom  = 16
	defaultMaxMsgBytes = 32 * 1024 // SDP is a few KB; this is safe headroom
	defaultRateBurst   = 20
	defaultRatePerSec  = 5
	joinTimeout        = 10 * time.Second
	idleTimeout        = 90 * time.Second
	pingInterval       = 30 * time.Second
	tokenMaxLen        = 128
)

// Config tunes a Hub. Zero values get sensible defaults.
type Config struct {
	Verbose         bool
	MaxClients      int     // max concurrent connections (global)
	MaxPerRoom      int     // max peers per session
	MaxMessageBytes int     // reject frames larger than this
	RateBurst       float64 // per-IP connection burst
	RatePerSec      float64 // per-IP connection refill rate
}

func (c *Config) withDefaults() {
	if c.MaxClients <= 0 {
		c.MaxClients = defaultMaxClients
	}
	if c.MaxPerRoom <= 0 {
		c.MaxPerRoom = defaultMaxPerRoom
	}
	if c.MaxMessageBytes <= 0 {
		c.MaxMessageBytes = defaultMaxMsgBytes
	}
	if c.RateBurst <= 0 {
		c.RateBurst = defaultRateBurst
	}
	if c.RatePerSec <= 0 {
		c.RatePerSec = defaultRatePerSec
	}
}

type msg struct {
	Type  string          `json:"type"`
	Room  string          `json:"room,omitempty"`
	ID    string          `json:"id,omitempty"`
	Peers []string        `json:"peers,omitempty"`
	To    string          `json:"to,omitempty"`
	From  string          `json:"from,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

type client struct {
	id   string
	room string
	ws   *websocket.Conn
	mu   sync.Mutex // serialize writes (x/net/websocket is not write-safe)
}

func (c *client) send(m msg) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return websocket.JSON.Send(c.ws, m)
}

// Hub tracks rooms and their members. Use New.
type Hub struct {
	cfg     Config
	limiter *ipLimiter

	clients atomic.Int64

	mu    sync.Mutex
	rooms map[string]map[string]*client // room -> id -> client
}

// New returns an empty Hub configured by cfg.
func New(cfg Config) *Hub {
	cfg.withDefaults()
	return &Hub{
		cfg:     cfg,
		limiter: newIPLimiter(cfg.RateBurst, cfg.RatePerSec),
		rooms:   map[string]map[string]*client{},
	}
}

// Handler returns an http.Handler that serves the signaling WebSocket endpoint
// at /s/<token> and rejects every other request with 404.
func (h *Hub) Handler() http.Handler {
	wsh := websocket.Handler(h.handle)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, sessionPrefix)
		if !strings.HasPrefix(r.URL.Path, sessionPrefix) || len(token) > tokenMaxLen || !tokenRE.MatchString(token) {
			http.NotFound(w, r) // reject everything that isn't a valid session URL
			return
		}
		if !h.limiter.allow(clientIP(r)) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		if h.clients.Load() >= int64(h.cfg.MaxClients) {
			http.Error(w, "server busy", http.StatusServiceUnavailable)
			return
		}
		wsh.ServeHTTP(w, r)
	})
}

func (h *Hub) handle(ws *websocket.Conn) {
	defer ws.Close()
	ws.MaxPayloadBytes = h.cfg.MaxMessageBytes

	token := strings.TrimPrefix(ws.Request().URL.Path, sessionPrefix)
	if token == "" {
		return
	}

	h.clients.Add(1)
	defer h.clients.Add(-1)

	// The first message must be a join, and must arrive promptly.
	_ = ws.SetReadDeadline(time.Now().Add(joinTimeout))
	var first msg
	if err := websocket.JSON.Receive(ws, &first); err != nil {
		return
	}
	if first.Type != "join" || first.ID == "" {
		return
	}

	c := &client{id: first.ID, room: token, ws: ws}
	existing, ok := h.join(c)
	if !ok {
		_ = c.send(msg{Type: "full"}) // room at capacity
		return
	}
	_ = c.send(msg{Type: "peers", Peers: existing})
	if h.cfg.Verbose {
		log.Printf("rp6-signal: %s joined session (%d present)", c.id, len(existing))
	}
	defer func() {
		h.leave(c)
		if h.cfg.Verbose {
			log.Printf("rp6-signal: %s left session", c.id)
		}
	}()

	// Keepalive: ping periodically; a dead peer's failed write closes the conn
	// (which unblocks the read loop below). Clients reply with a pong, which
	// also refreshes the idle read deadline.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(pingInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if err := c.send(msg{Type: "ping"}); err != nil {
					ws.Close()
					return
				}
			}
		}
	}()

	for {
		_ = ws.SetReadDeadline(time.Now().Add(idleTimeout))
		var m msg
		if err := websocket.JSON.Receive(ws, &m); err != nil {
			return
		}
		if m.Type == "signal" && m.To != "" {
			h.relay(c, m)
		}
		// other types (pong, unknown) just count as liveness
	}
}

func (h *Hub) join(c *client) ([]string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.rooms[c.room]
	if room == nil {
		room = map[string]*client{}
		h.rooms[c.room] = room
	}
	if len(room) >= h.cfg.MaxPerRoom {
		return nil, false
	}
	existing := make([]string, 0, len(room))
	for id, other := range room {
		existing = append(existing, id)
		_ = other.send(msg{Type: "peer-join", ID: c.id})
	}
	room[c.id] = c
	return existing, true
}

func (h *Hub) leave(c *client) {
	h.mu.Lock()
	room := h.rooms[c.room]
	if room != nil {
		delete(room, c.id)
		if len(room) == 0 {
			delete(h.rooms, c.room)
		}
	}
	peers := make([]*client, 0, len(room))
	for _, other := range room {
		peers = append(peers, other)
	}
	h.mu.Unlock()
	for _, other := range peers {
		_ = other.send(msg{Type: "peer-leave", ID: c.id})
	}
}

func (h *Hub) relay(from *client, m msg) {
	h.mu.Lock()
	var target *client
	if room := h.rooms[from.room]; room != nil {
		target = room[m.To]
	}
	h.mu.Unlock()
	if target == nil {
		return
	}
	m.From = from.id
	_ = target.send(m)
}

// clientIP returns the caller's IP, honoring X-Forwarded-For when set by a
// trusted reverse proxy (e.g. Caddy terminating TLS in front of us).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if before, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(before)
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- per-IP token-bucket rate limiter ----

type bucket struct {
	tokens float64
	last   time.Time
}

type ipLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	burst   float64
	refill  float64 // tokens per second
}

func newIPLimiter(burst, refill float64) *ipLimiter {
	return &ipLimiter{buckets: map[string]*bucket{}, burst: burst, refill: refill}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Crude unbounded-growth guard: reset the table if it gets large (a flood
	// of distinct source IPs). Fine for a small self-hosted signaling server.
	if len(l.buckets) > 10000 {
		l.buckets = map[string]*bucket{}
	}
	now := time.Now()
	b := l.buckets[ip]
	if b == nil {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[ip] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.refill
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
