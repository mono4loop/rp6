# Jam sessions

Play RP6 **together over the internet** (or a LAN): everyone who joins with the
same session code hears — and sees a blink for — each other's live pad hits.
Only tiny control messages travel between you; each person's own P-6 or built-in
emulator makes the sound.

Jam is included in the normal desktop build (it's compiled unless you build with
`-tags nojam`; web/mobile builds don't include it).

> **Heads up:** live pad jamming is latency-bound. On a LAN it feels essentially
> local; across the internet it gets looser the further apart you are. It's
> great fun, but it's not designed for tight, sample-locked unison.

There are two pieces:

1. a small **signaling server** (`rp6-signal`) that introduces peers to each
   other, and
2. the **RP6 app**, where you join a session.

---

## 1. Run a signaling server

Peers can't find each other without a rendezvous point. The signaling server
only brokers the initial connection — it never sees or relays your pad data
(that flows directly peer-to-peer, encrypted). It's tiny and stateless.

You (or one person in the group) run it somewhere everyone can reach: a laptop
on the same LAN, or a small VPS for internet play.

### Build it

```
go build -o rp6-signal ./cmd/rp6-signal
```

### Run it

```
rp6-signal -addr :1337
```

Options:

| Flag             | Default | Meaning                              |
|------------------|---------|--------------------------------------|
| `-addr`          | `:1337` | listening address / port             |
| `-max-clients`   | 500     | max concurrent connections (global)  |
| `-max-per-room`  | 16      | max peers in one session             |

Open the port on the host's firewall if needed (e.g. `1337/tcp`).

### On a LAN

That's all you need. Peers connect with `ws://<host-ip>:1337/` — for example
`ws://192.168.1.20:1337/`. No TLS required on a trusted local network.

### On the internet (recommended: TLS via Caddy)

For internet play, put the server behind a TLS-terminating reverse proxy so
peers can use a secure `wss://` URL. Run the server bound to localhost:

```
rp6-signal -addr 127.0.0.1:1337
```

and a minimal Caddyfile:

```
jam.example.com {
    reverse_proxy 127.0.0.1:1337
}
```

Caddy fetches the certificate and forwards the WebSocket automatically. Peers
then use `wss://jam.example.com` (or just `jam.example.com` — see below).

Keep it as a service so it survives reboots (systemd, a container with
`--restart=always`, etc.).

### Is it safe to leave public?

Reasonably. It stores nothing, sees no pad data, and can't be used as an open
relay to other hosts. It has built-in guardrails (per-IP rate limiting,
connection caps, message-size limits, idle timeouts), and every session lives at
a hard-to-guess URL path — requests to anything else are rejected. Still:

- Prefer a **fresh session code** each time (the app generates one for you).
- Treat the session URL as a secret (a reverse proxy may log request paths — turn
  off access logs for this host if that matters to you).
- For a private group, binding to a **LAN / VPN / tailnet** instead of the public
  internet is strictly safer.

## 2. Join a session in RP6

1. In the bottom rack, click the **person icon** (it sits with the other rack
   toggles).
2. In the dialog:
   - **Signaling server** — enter your server's address. You can type a bare host
     (`jam.example.com`, which becomes `wss://…` — secure by default), or a full
     `ws://host:1337/` for a plain LAN server.
   - **Session code** — a random code is filled in for you. Keep it, or press
     **Generate** for a new one. This is what you share.
3. Click **Join**. The person button lights up (like the other toggles) while the
   session is active. The status line shows how many peers are connected.

**Invite others:** tell them the same **server address** and **session code**.
They enter the same two values and Join. Now play — your pads sound and blink on
their screens, and theirs on yours.

To see the current session (code, server, peer count) or to leave, click the
person icon again.

### Which hits are shared?

- **Screen taps** and hits from a connected **MIDI controller** (e.g. a MacroPad)
  are always shared.
- Physical **P-6 pad presses** are shared while input-listen (the eye toggle) is
  on — its default for a connected P-6.
- The **sequencer** and **roll** effects are *not* broadcast — only your live,
  manual hits are.

Remote hits play and blink but never change your selection, page, or anything
else in your UI.

## 3. Command-line / auto-join (optional)

Instead of the dialog you can join at launch with environment variables — handy
for a fixed setup:

```
RP6_JAM_SIGNAL=wss://jam.example.com RP6_JAM_CODE=<code> rp6
```

| Variable         | Meaning                                                    |
|------------------|------------------------------------------------------------|
| `RP6_JAM_SIGNAL` | server address (bare host, `ws://…`, or `wss://…`)         |
| `RP6_JAM_CODE`   | session code; if unset, jam stays off                      |
| `RP6_JAM_NAME`   | optional display name (appears in logs)                    |

The app remembers the last server and code you used, so the dialog pre-fills
them next time.

## 4. Troubleshooting

- **"bad status" / won't connect when joining.** Usually the server address is
  wrong or the server isn't reachable. Check it in a browser/terminal:
  `curl -i https://jam.example.com/` should return `404` (the server is up but
  that path isn't a session), which means the address works.
- **Joined, but peers never connect.** Make sure everyone used the *exact* same
  server address and session code. Very restrictive networks (symmetric NAT on
  both ends) can block the direct peer-to-peer link; a LAN or a VPN avoids this.
- **Playing feels laggy.** That's network latency/jitter, not a bug. Wired
  ethernet beats WiFi by a lot (WiFi causes the intermittent spikes). The closer
  the peers, the tighter it feels. Your *own* hits are always instant — only
  what you hear *from* others is delayed.
- **No person icon in the app.** You're on a build without jam (a web/mobile
  build, or one built with `-tags nojam`). Use a normal desktop build.

---

For the design, wire protocol, and internals, see the
[architecture notes](architecture/jams.md).
