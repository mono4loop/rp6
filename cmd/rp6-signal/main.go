// Command rp6-signal is a tiny WebSocket signaling server for rp6 shared jam
// sessions. It relays the WebRTC handshake (SDP offers/answers and ICE
// candidates) between peers that join the same room (session code). It never
// sees the pad/jam data itself — that flows directly peer-to-peer over WebRTC,
// DTLS-encrypted.
//
// Run it anywhere reachable by the peers (a laptop on the LAN, a small VPS):
//
//	rp6-signal -addr :1337
//
// then have each RP6 join with the same server + session code (via the in-app
// JAM dialog, or environment variables):
//
//	RP6_JAM_CODE=<code> RP6_JAM_SIGNAL=ws://<host>:1337/ rp6
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/mono4loop/rp6/internal/jam/signal"
)

func main() {
	addr := flag.String("addr", ":1337", "listening address")
	maxClients := flag.Int("max-clients", 0, "max concurrent connections (0 = default)")
	maxPerRoom := flag.Int("max-per-room", 0, "max peers per session (0 = default)")
	flag.Parse()

	h := signal.New(signal.Config{
		Verbose:    true,
		MaxClients: *maxClients,
		MaxPerRoom: *maxPerRoom,
	})
	http.Handle("/", h.Handler())
	log.Printf("rp6-signal: listening on %s (sessions at /s/<code>)", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatalf("rp6-signal: %v", err)
	}
}
