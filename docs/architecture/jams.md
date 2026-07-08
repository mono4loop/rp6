# Shared jam sessions

Developer-oriented notes on RP6's **shared jam sessions**: how peers play
together over the network, how the code is structured, and the trade-offs baked
into the design. If you're just *using* it, the short version is in the
[README](../../README.md#shared-jam-sessions).

---

## 1. What it is (and isn't)

A jam session lets several RP6 instances share **live pad hits**: when you tap a
pad (on screen, on a connected controller, or on the P-6 itself), your peers
hear it on *their* device and see the pad **blink** — exactly as if it had come
from an external MIDI controller. Nothing else about your UI reacts to a peer:
no selection change, no page switch, no status churn. Just sound + blink.

What it deliberately does **not** do (today):

- **No audio streaming.** RP6 ships tiny *control* messages (a pad id + a
  velocity); the sound is made locally by each peer's P-6/emulator. This sidesteps
  codecs, bandwidth and buffering — the hard parts of networked audio — but it
  does *not* escape latency (see §7).
- **No shared clock / sequencer.** Each peer's sequencer and tempo are its own.
  Only manual, human pad hits are shared. Synchronised sequencing (Ableton
  Link-style tempo + beat-phase) is a possible future direction, not built.
- **No re-broadcast of sequenced/effect hits.** Only the *live* human sources
  broadcast; the sequencer and the effects roller do not (that would double or
  flood).

The feature is **compiled by default** on desktop builds. It can be dropped with
`-tags nojam`, and is automatically excluded on web (wasm) and mobile builds,
where the pion/webrtc dependency doesn't apply.

## 2. Design principles

These mirror the rest of the codebase (see the repo's `AGENTS.md`):

- **The core is generic.** `internal/jam` imports no Fyne, no `p6`, and no
  networking library. It's a callback surface (`OnPad`) plus a `Send*` API over
  a pluggable `Transport`, exactly like `internal/effects` and
  `internal/sequencer`. It's unit-testable with an in-memory loopback transport
  and zero network.
- **The network layer is swappable and build-tagged.** The concrete WebRTC
  transport lives in `internal/jam/webrtc`, built by default on desktop and
  excluded (replaced by a stub, no pion) with `-tags nojam` or on web/mobile
  (`//go:build !nojam && !js && !android && !ios`). So a wasm build, a mobile
  build, or a `-tags nojam` desktop build shows **0 pion packages** in
  `go list -deps ./cmd/rp6/`.
- **The app wiring is contained.** All app-side logic is in the build-tagged
  `cmd/rp6/jam.go`; `cmd/rp6/jam_stub.go` provides no-op stand-ins so the handful
  of call sites in `main.go` compile unchanged. No jam fields live on the `ui`
  struct (RP6 is a single-window app, so the tagged file uses package-level
  handles).
- **MIT, not AGPL.** The "watch-party" prior art (`multiplex`) uses
  [`weron`](https://github.com/pojntfx/weron), which is **AGPL-3.0** and would
  relicense RP6. We use [`pion/webrtc`](https://github.com/pion/webrtc)
  **directly** (MIT) and write the small amount of signaling/mesh glue ourselves,
  keeping RP6 MIT-clean. Using pion directly also lets us pick an unreliable data
  channel (see §5), which weron's stream abstraction hides.

## 3. Package layout

```
internal/jam/                 pure core — no Fyne, no p6, no pion
  jam.go        Transport interface; Engine (OnPad, SendPad, Start, Close);
                async send loop; receive loop
  message.go    wire codec: [ 'J', kind, pad, velocity ]
  loopback.go   NewLoopback() -> two in-memory Transports wired to each other
  code.go       NewCode() (~80-bit session code); NormalizeCode()
  jam_test.go   engine/codec/loopback tests (plain `go test`, no network)

internal/jam/webrtc/          the pion transport (default on desktop; !nojam,!js,!android,!ios)
  transport.go  Dial; Transport (implements jam.Transport); signaling client;
                supervised reconnect; NormalizeURL
  peer.go       per-peer PeerConnection + data channel; ICE; RTT logging
  doc.go        stub (nojam || js || android || ios) so the package always builds
  *_test.go     in-process mesh, reconnect, and URL-normalize tests

internal/jam/signal/          the signaling hub — no pion, just x/net/websocket
  signal.go     Hub + Handler(): path gating, rate limit, caps, keepalive

cmd/rp6-signal/               standalone signaling server binary
cmd/rp6/jam.go                button, dialog, wiring (default on desktop)
cmd/rp6/jam_stub.go           no-op stand-ins (nojam || js || android || ios)
```

Generic UI support: `components.PadGrid.FlashPad(page,row,col)` flashes a pad
**without** changing selection or page — the "blink but don't disturb my UI"
primitive the receive path needs.

## 4. Data flow

### Send (local hit → peers)

Broadcast happens only at the **live human sources**, so sequenced/effect hits
are never shipped:

- `onPadTrigger` (grid tap) — `cmd/rp6/main.go`
- `fireExternalPad` (external MIDI controller) — `cmd/rp6/main.go`
- `onMIDIIn` (physical P-6 pad press, when input-listen is on) — `cmd/rp6/main.go`

Each calls `u.jamBroadcastPad(id, velocity)` (a no-op without the tag) →
`jamEngine.SendPad`. `SendPad` **never blocks the caller**: it does a
non-blocking enqueue to a buffered channel drained by a background `sendLoop`
goroutine, dropping on a full queue. This guarantees that a network stall can't
lag your *own* playing. A dropped live hit is preferable to a stalled one.

### Receive (peer hit → local sound + blink)

The transport's read goroutine decodes a frame and calls `Engine.OnPad`, wired
to `cmd/rp6/jam.go`'s `applyRemotePad`:

1. `firePadVel(id, velocity)` — plays it on the local P-6/emulator (this path is
   already concurrency-safe via `devMu`).
2. `fyne.Do(func(){ grid.FlashPad(...) })` — blinks the pad on the UI thread,
   **without** touching selection/page/effects/status.

Because remote hits are applied through `applyRemotePad` (which does not call
`jamBroadcastPad`), there is **no echo**.

## 5. WebRTC transport (`internal/jam/webrtc`)

- **Topology:** full mesh — each peer holds a `PeerConnection` + one data
  channel to every other peer. Fine for small jams (say 2–6). Beyond that you'd
  want an SFU/relay (out of scope).
- **Data channel:** created `Ordered: false, MaxRetransmits: 0` — i.e.
  UDP-like. For discrete, time-critical hits this is correct: a late or lost hit
  is dropped, not retransmitted (no head-of-line blocking, lowest latency). This
  is the concrete reason we use pion directly rather than a reliable-stream
  abstraction.
- **Frames:** the 4-byte `internal/jam` wire format rides the channel;
  `OnMessage` copies the bytes and hands them to the Engine via `deliver` (a
  buffered, drop-on-full channel).
- **Signaling:** a small JSON protocol over a WebSocket to an `rp6-signal`
  server (§6). On join the server returns the current member list; the
  **newcomer always offers** to each existing peer (a deterministic initiator
  rule that avoids SDP glare). ICE is trickled; candidates arriving before the
  remote description is set are queued and flushed.
- **STUN/TURN:** defaults to a public Google STUN server for NAT traversal.
  There's no TURN by default, so two peers behind symmetric NATs may fail to
  connect P2P; add TURN via `Config.ICEServers` if needed.
- **Peer count:** a peer is counted once its data channel opens and uncounted on
  close; the transport reports the live count via `Config.OnPeers`, which the app
  uses to drive the button and status line.
- **RTT:** each peer polls the nominated ICE candidate pair's stats and logs a
  rolling **min/avg/max** (~10s window), so jitter is obvious in the log — this
  is the latency you actually feel on remote hits.

### Reconnect & liveness

The signaling link is **supervised**:

- `Dial` connects synchronously once (so a bad URL / unreachable server surfaces
  immediately), then hands the connection to a `supervise` goroutine.
- If the link drops before `Close`, `supervise` tears down the now-stale peer
  connections (`resetPeers` → `OnPeers(0)`) and reconnects with **capped
  exponential backoff** (1s → 2s → … → 30s). On reconnect it rejoins; the fresh
  member list re-forms the mesh automatically.
- The read loop sets a **90s idle deadline**; the server pings every ~30s and
  clients reply `pong`, so a silently half-open socket is detected and
  reconnected rather than hanging.

Not handled: a peer's P2P path failing while the signaling link stays healthy
(pion marks it `failed`, we drop that peer, and it only re-forms on a signaling
event). The fix would be **ICE restart** on the initiator; most real drops take
the WebSocket with them, so signaling reconnect covers the common cases.

### Session code as a bearer capability

The session code travels in the URL **path** (`/s/<code>`), not the body. It's
minted by `jam.NewCode()` (~80 bits, base32 groups) and doubles as the sole
access gate. `webrtc.NormalizeURL` lets users type a bare host
(`rp6-signal.example.com` → `wss://…`, secure by default), `http(s)://`, or
`ws(s)://`.

## 6. Signaling server (`internal/jam/signal`, `cmd/rp6-signal`)

A tiny WebSocket relay that connects peers and **never sees pad data** (that's
pure P2P). It's hardened for public exposure:

- **Path gating:** only `/s/<token>` (token shape-validated) is accepted; every
  other request is `404`ed before any work — squatters/scanners get nothing.
- **Rate limiting:** per-IP token bucket (honors `X-Forwarded-For` from a
  trusted proxy like Caddy) → `429`.
- **Caps:** global max connections (`503`) and max peers per room.
- **Message-size cap** (`MaxPayloadBytes`) kills the giant-frame OOM vector.
- **Deadlines + keepalive:** a join handshake timeout, a 90s idle timeout, and a
  30s server→client ping (client replies `pong`), reaping dead links.

Run it standalone:

```
rp6-signal -addr :1337 [-max-clients N] [-max-per-room N]
```

Put it behind Caddy for TLS (`reverse_proxy` transparently upgrades WebSockets);
peers then use `wss://…`. See the server's package doc for details.

## 7. Latency, honestly

Live pad hits are **latency-bound** — no transport trick beats physics:

- LAN / same room: sub-ms to a few ms — effectively local.
- Same city/ISP: ~10–30ms one-way — playable, slightly loose.
- Cross-continent: ~80–150ms+ — call-and-response, not tight unison.

Two design rules make it feel as good as physically possible:

1. **Local-first.** You always hear your *own* hits immediately (fired locally;
   broadcast is async). Only remote hits arrive late.
2. **Drop, don't buffer.** The unreliable channel discards stale hits rather than
   retransmitting. We deliberately did **not** add a jitter buffer: for discrete
   percussive events it would trade the low latency floor for a constant delay,
   which feels worse to play against. Fix jitter at the source (wired > WiFi).

The RTT log (`min/avg/max`) is the tool for diagnosing "sometimes laggy": a low
floor with high `max` is jitter (usually WiFi), not distance.

## 8. Security & privacy

- Data channels are **DTLS-encrypted** by default (pion).
- The signaling server routes only the handshake; it never sees pad data, and
  P2P means peers exchange it directly.
- The session code is an ~80-bit **capability** in the URL path. Treat the URL as
  a secret (note: a reverse proxy may log the path — disable access logs for the
  signaling host if that matters), and use a fresh `NewCode()` per session.

## 9. Build, run, configure

Jam is in the normal desktop build — nothing special to build:

```
make run          # jam included by default
make run TAGS="capture wayland migrated_fynedo nojam"   # explicitly disable it
```

In-app: click the **person icon** in the bottom rack → the guided dialog
(server URL + a pre-generated session code) → Join. The button lights (border +
icon, like the other toggles) while a session is active; peer count shows in the
status line and the status dialog.

Headless / launch auto-join (also used for testing):

| Variable          | Meaning                                             |
|-------------------|-----------------------------------------------------|
| `RP6_JAM_CODE`    | session code; if unset, jam is inert                |
| `RP6_JAM_SIGNAL`  | signaling URL/host; falls back to the last-used one |
| `RP6_JAM_NAME`    | optional display name (logs only)                   |

Preferences persisted: `jam.signal`, `jam.code` (last-used, for the dialog).

## 10. Testing

All jam tests run without hardware:

- `internal/jam` (plain `go test`): engine relays hits, no self-echo,
  out-of-range/garbage rejection, code format, closed-transport behavior — via
  the loopback transport, no network.
- `internal/jam/webrtc`: an **in-process** end-to-end test stands up a real
  `signal.Hub`, connects two real WebRTC transports, and asserts a hit crosses
  the data channel (`TestMeshRelaysPadHits`); a **reconnect** test severs the
  live socket and asserts the mesh re-forms (`TestReconnectsAfterSignalingDrop`);
  plus `TestNormalizeURL`. The two end-to-end tests open real connections, so
  they're opt-in — set `RP6_JAM_E2E=1` (otherwise they skip, keeping the default
  `go test ./...` fast and hermetic). Run with `-race` to cover the
  peer-count/reconnect concurrency.

## 11. Ideas / not built

- **ICE restart** for peer paths that fail without a signaling drop.
- **Synchronised sequencer / tempo** (Ableton Link-style phase lock) for shared
  grooves rather than just live hits.
- **Browser peers:** RP6 has a wasm build and browsers speak WebRTC natively; a
  wasm `Transport` (via `syscall/js`) could let a laptop and a phone jam.
- **TURN** support / bundling for symmetric-NAT peers.
- A roster UI (names, per-peer RTT) beyond the current count.
