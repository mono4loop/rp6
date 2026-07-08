//go:build !nojam && !js && !android && !ios

package webrtc

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mono4loop/rp6/internal/jam"
	"github.com/mono4loop/rp6/internal/jam/signal"
)

// requireE2E gates the WebRTC end-to-end tests: they open real connections (ICE
// + DTLS + SCTP over loopback), so they're opt-in to keep the default `go test`
// run fast and hermetic. Enable with RP6_JAM_E2E=1.
func requireE2E(t *testing.T) {
	if os.Getenv("RP6_JAM_E2E") == "" {
		t.Skip("WebRTC end-to-end test; set RP6_JAM_E2E=1 to run")
	}
}

// TestMeshRelaysPadHits stands up an in-process signaling server, connects two
// real WebRTC transports through it, and verifies a pad hit sent by one peer is
// delivered to the other over the peer-to-peer data channel. It exercises the
// full path: signaling handshake -> ICE (host candidates on localhost) -> DTLS
// -> SCTP data channel. Enable with RP6_JAM_E2E=1.
func TestMeshRelaysPadHits(t *testing.T) {
	requireE2E(t)
	srv := httptest.NewServer(signal.New(signal.Config{}).Handler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"

	const room = "test-session"
	ta, err := Dial(Config{Signaling: wsURL, Room: room})
	require.NoError(t, err)
	tb, err := Dial(Config{Signaling: wsURL, Room: room})
	require.NoError(t, err)

	a := jam.New(ta)
	b := jam.New(tb)
	defer a.Close()
	defer b.Close()

	got := make(chan [2]int, 8)
	b.OnPad = func(pad int, vel uint8) { got <- [2]int{pad, int(vel)} }
	a.Start()
	b.Start()

	// The data channel opens asynchronously; retransmit until B hears it (the
	// channel is unreliable, so early sends may be dropped before it's open).
	deadline := time.After(25 * time.Second)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case g := <-got:
			require.Equal(t, [2]int{5, 99}, g)
			return
		case <-tick.C:
			a.SendPad(5, 99)
		case <-deadline:
			t.Fatal("timeout: peers never exchanged a pad hit over WebRTC")
		}
	}
}

// TestReconnectsAfterSignalingDrop verifies the transport auto-recovers when its
// signaling link drops: peer A joins, its underlying TCP connection is severed
// (simulating a network blip / server hiccup — x/net/websocket hijacks the conn,
// so we drop it directly), the server keeps running, then peer B joins. A must
// reconnect and re-mesh with B (proven by A hearing B's pad hit).
func TestReconnectsAfterSignalingDrop(t *testing.T) {
	requireE2E(t)
	base, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ln := &trackingListener{Listener: base, conns: map[net.Conn]struct{}{}}
	wsURL := "ws://" + base.Addr().String() + "/"
	const room = "recon-test"

	srv := &http.Server{Handler: signal.New(signal.Config{}).Handler()}
	go srv.Serve(ln)
	defer srv.Close()

	ta, err := Dial(Config{Signaling: wsURL, Room: room})
	require.NoError(t, err)
	a := jam.New(ta)
	got := make(chan struct{}, 8)
	a.OnPad = func(int, uint8) {
		select {
		case got <- struct{}{}:
		default:
		}
	}
	a.Start()
	defer a.Close()

	time.Sleep(300 * time.Millisecond) // let A's join settle
	ln.dropAll()                       // sever A's signaling link -> A must reconnect

	// New peer joins the still-running server; A must have reconnected to re-mesh.
	tb, err := Dial(Config{Signaling: wsURL, Room: room})
	require.NoError(t, err)
	b := jam.New(tb)
	defer b.Close()
	b.Start()

	deadline := time.After(40 * time.Second)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-got:
			return // A heard B -> A reconnected and re-meshed
		case <-tick.C:
			b.SendPad(1, 1)
		case <-deadline:
			t.Fatal("timeout: A did not reconnect and re-mesh after its signaling link dropped")
		}
	}
}

// trackingListener records accepted connections so a test can forcibly drop
// them (severing hijacked WebSockets that http.Server.Close would otherwise
// leave open).
type trackingListener struct {
	net.Listener
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func (l *trackingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.conns[c] = struct{}{}
	l.mu.Unlock()
	return c, nil
}

func (l *trackingListener) dropAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for c := range l.conns {
		_ = c.Close()
		delete(l.conns, c)
	}
}
