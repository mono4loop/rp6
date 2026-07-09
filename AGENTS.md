# AGENTS.md

Guidance for AI agents (and humans) working on **rp6**, a touch-friendly Roland
P-6 pad controller written in Go with the Fyne GUI toolkit.

This file is knowledge transfer. Read the "P-6 MIDI reality" section before
adding any feature that talks to the hardware — several obvious-sounding
features are simply **not possible** over the P-6's MIDI implementation, and
that has shaped the whole design.

---

## 1. What rp6 is

A desktop GUI that drives a Roland **P-6** (AIRA Compact sampler) over its
class-compliant **USB MIDI** port:

- A 4×6 grid of 24 finger-friendly pads, paged between banks **A–D** and **E–H**
  (all 48 pads reachable), with a selection highlight on the last-tapped pad.
  The pad rack has a slim **left tool column** of backlit icon toggles
  (`components.RackToggle`, lit when active): the first **floats** the
  pad rack into its own window and **docks** it back (also on window close); the
  second toggles **listening to P-6 MIDI input** (reflecting hardware pad
  presses in the UI — eye icon; on by default for a connected P-6, off for the
  emulator, which has no MIDI input — see `setListenDefault`); the third is
  **double density**
  (grid icon) — all 8 banks on a single page with half-size pads (off by
  default). Density rebuilds the grid and swaps it into a holder container.
- A "rack unit" toolbar: illuminated **Play/Stop** transport (with a MIDI clock
  generator so Play actually runs), a **TEMPO** rotary knob and a **PATTERN**
  rotary knob (drag / scroll / arrow keys; lit ring when focused), and
  **Delay/Reverb** sliders with red 7-segment readouts.
- A toggleable vertical **master activity meter** on the right (real USB-audio
  VU when built with `-tags capture`, otherwise a trigger-activity meter),
  framed as a rack panel. The pad rack has a modest per-pad min size so it
  **adapts** (pads shrink to fit) rather than forcing the window larger — no
  scrollbars — including when the sequencer is docked as a right-hand column.
- A toggleable **effects rack** for the selected pad (4 slots + Roll rate).
- A toggleable **software step sequencer** (up to 8 assignable tracks, default
  6; each track 1–4 **bars** long and looping at its own length = polymeter;
  16 steps/bar, tempo-synced, own Play/Stop, per-track mute) that drives the
  pads host-side. Each track row is just a **pad-assign** key (the `A1`..`H6`
  label) followed by that track's step cells. Mute and bar-length are **not**
  per-track keys: a **shared second control row** (a speaker-icon **mute** and a
  **BARS** toggle that cycles 1→4) acts on the currently-**armed** track and
  greys out when none is armed. Arming a track and assigning its pad are one
  **touch-friendly gesture** (no modifier keys): tap a track's pad-assign key to
  **arm** it (it lights hardest — the `RackToggle.armed` state), which both
  points the second-row mute/bars controls at that track and makes the next
  tapped pad (grid, hardware, or external MIDI) its sample; assigning a pad
  disarms the track automatically (so an accidental pad hit can't change it).
  Tap the armed key again to cancel; arm another track to move the arm. It can
  also **dock** as a right-hand column beside the pads (the pad rack then adapts
  to the remaining space).
  **Sequences are saved to SQLite** (`internal/store`) in numbered slots with a
  name + tempo; the working slot autosaves on quit and reloads on launch.
  Persistence is **scoped to a profile** (`"p6"` for hardware / no-`-emu` runs,
  `"emu:<abs-samples-dir>"` per emulator kit) so P-6 and emulator sequences
  never intermingle (see `ui.storeProfile`).
  Changing the sequence slot (SEQ knob) while playing is **quantized to the next
  bar** (queued in `pendingSlot`, applied from the step callback / on stop).
- A **shared jam session** (bottom-bar person-icon toggle): several RP6s connect
  peer-to-peer over WebRTC and share **live pad hits** — a peer's tap plays on
  your device and blinks the pad (like an external MIDI controller), without
  disturbing your selection/UI. **Desktop-only**, built by default (disable with
  `-tags nojam`; excluded on web + mobile). Needs a small signaling server
  (`cmd/rp6-signal`). Design + setup: `docs/architecture/jams.md`, `docs/jams.md`.
- The pad grid, delay/reverb, effects and sequencer are all **toggleable racks**
  (see the bottom-bar toggles below).
- A **rack-framed bottom bar** that hosts the **visibility toggles** — backlit
  rack-label `components.RackToggle`s (`PADS · DLY/REV · FX · SEQ · VU`, lit when
  shown / greyed when hidden, also **Ctrl+Shift+P/D/F/S/M**) — a red/green
  connection **LED** (breathing glow), status text, and an **ⓘ info** dialog
  button. There is **no Reconnect button**: the app auto-connects/reconnects
  (see the device watcher in §4).
- Amber/orange theme; **Ctrl+Q** quits.

It runs on the machine the P-6 is plugged into (GUI + USB both needed). On Linux
that's the host, not a VM/container (USB passthrough + display).

---

## 2. Build / test / dev commands

```bash
make run          # go run -tags "capture wayland migrated_fynedo" ./cmd/rp6  (needs a display + ideally the P-6)
make build        # -> build/rp6  (capture backend + native Wayland + fyne.Do model)
make test         # go test ./...  (NO build tags -> no audio backend needed)
make check        # fmt + vet + test + staticcheck (incl. -tags capture on audio)
```

The default build tags are **`capture wayland migrated_fynedo`**:
- **`capture`** pulls in the malgo/miniaudio audio backend for the live VU
  meter.
- **`wayland`** builds Fyne's glfw driver against GLFW's native Wayland
  backend (rather than X11).
- **`migrated_fynedo`** opts into Fyne 2.8's fyne.Do threading model
  (`DisableThreadChecks`), silencing the "not migrated" startup warning. Safe
  because every background-goroutine→UI update already goes through `fyne.Do`
  (meter/LED/tap-flash animators, sequencer `OnStep`, MIDI-in reflection); the
  effects/sequencer engines only fire MIDI (no UI) from their clock goroutines.

`make run`/`build`/`install` set all three by default; override via `TAGS=`, e.g.
`make run TAGS=capture` (X11 driver) or `make run TAGS=wayland` (no audio).
Tests never use tags (they exercise the stub + a fake capturer, and keep Fyne's
thread-safety checks active), so `go test ./...` needs no audio libraries.

**Jam sessions** are compiled by default on desktop and pull in `pion/webrtc`
(MIT); disable with **`-tags nojam`** (also auto-excluded on web/mobile). Runtime
config is env-only: `RP6_JAM_SIGNAL` (server), `RP6_JAM_CODE` (session), plus the
in-app dialog. The WebRTC end-to-end tests are opt-in — set **`RP6_JAM_E2E=1`**
(otherwise they skip, so `go test ./...` stays fast/hermetic). See §3 and
`docs/architecture/jams.md`.

Manual quality gate used throughout development (run after edits):

```bash
gofmt -w .
go build ./cmd/rp6/... ./internal/... ./p6/...
go test ./cmd/rp6/... ./internal/... ./p6/...
staticcheck ./cmd/rp6/... ./internal/... ./p6/...
go vet ./cmd/rp6/... ./internal/... ./p6/...
```

Prefer `gopls` diagnostics after edits. **Note:** gopls sometimes reports
*stale* "for loop can be modernized" hints at wrong line numbers right after a
file is created/moved; re-run diagnostics on just that file to confirm it's
clean. Verify with `grep -n "for .* := 0; " <file>` before "fixing" anything.

### Build prerequisites (Linux)

Fyne needs cgo + graphics dev headers: `gcc` and GL/`libxkbcommon`. The default
`wayland` build also needs the Wayland client libs (`wayland-devel`,
`libxkbcommon-devel`, `mesa-libGL-devel` on Fedora); an X11 build (`TAGS=capture`)
needs the X11 packages instead (`libX11-devel libXcursor-devel libXrandr-devel
libXinerama-devel libXi-devel libXxf86vm-devel mesa-libGL-devel
libxkbcommon-devel`). The Fyne binary is ~24 MB; first cgo/glfw build is slow.

---

## 3. Architecture & package boundaries

```
cmd/rp6/            the application (P-6-specific wiring)
  main.go           app struct `ui`, layout, transport/tempo/pattern/FX handlers,
                    connect/close, Ctrl+Q, meter animator
  padgrid.go        P-6 config for the generic PadGrid: bankColors, page->bank
                    mapping (bankPad), PadLabel labels
  ui_test.go        headless UI tests
p6/                 dependency-free MIDI client for the P-6 (NO Fyne, NO cgo)
  pad.go            bank/pad <-> note mapping (48..95), labels
  midi.go           status byte + message builders (NoteOn/CC/PC/realtime)
  cc.go             Control Change numbers + AutoCC/GranularCC helpers
  device.go         ALSA rawmidi discovery + open (O_RDWR) + Send methods, Config
  device_alsa.go    (!js && !android) ALSA rawmidi backend (Discover/Open/OpenPath)
  device_js.go      (js) Web MIDI backend; device_android.go (android) MIDI-bridge
                    backend — both reuse device.go builders + input.go parser,
                    only the byte transport differs (see midibridge/)
  input.go          MIDI input parser (running status/realtime) + Device.Listen
  clock.go          Clocker: MIDI Start/Stop + timing-clock generator
  controller.go     Controller interface (pad/CC/PC/transport/Listen) — the
                    swap point: *Device and *emu.Emulator both implement it
internal/emu/       software P-6 emulator: plays WAV samples (NO Fyne)
  emu.go            Emulator: implements p6.Controller; loads a P-6-style
                    sample set (A1..H6) from an fs.FS (os.DirFS or the embedded
                    kit), fires pads into a mixer
  kit.go            //go:embed of the built-in "modular-hits" kit + credits;
                    OpenDefault() loads it (48 pads, playable out of the box)
  assets/modular-hits/  the embedded default kit: A1.wav..H6.wav + CREDITS.txt
  wav.go            dependency-free WAV decode/encode + channel/rate resample
  mixer.go          voice mixer (16-voice cap, sums+clamps) — pure, testable
  sink.go           sink interface (audio output the mixer renders into)
  sink_stub.go      default (no tag): silent sink (loads+mixes, no sound)
  sink_malgo.go     //go:build capture: miniaudio/malgo playback backend
  sink_js.go        (js) Web Audio sink (AudioWorklet, resumed on a user gesture)
internal/effects/   host-side effects engine (NO Fyne, NO p6 — pure logic)
  effects.go        Engine: per-pad slots, Roll (tempo-synced retrigger), Tap,
                    background rollers; fires pads via a Trigger callback
  icons.go          Kind.Icon() -> image.Image badge (stdlib image only)
internal/sequencer/ host-side step sequencer (NO Fyne, NO p6 — pure logic)
  sequencer.go      Engine: tracks×(bars×16) grid, per-track bar length &
                    mute, tempo-synced drift-compensated tick clock; fires pads
                    via Trigger, playhead via OnStep(tick); Snapshot/Restore
internal/midiin/    pluggable MIDI *input* controllers (NO Fyne) — the input-
                    side mirror of p6; drivers register via init(), the app
                    blank-imports them, Detect() opens whichever is plugged in
  midiin.go         Handlers (TriggerPad/Transport) + Device/Driver + registry
  alsa.go           FindRawMIDI: /proc/asound/cards scan by card-name substring
  macropad/         Adafruit MacroPad RP2040 driver (note 48..95 -> pad,
                    realtime Start/Stop -> transport); reuses p6.ParseMIDI.
                    macropad_alsa.go (!android && !js) opens the rawmidi node;
                    macropad_android.go (android) reads from midibridge instead
                    — the MIDI->Handlers mapping (handle) is shared
  webmidi/          (js) Web MIDI input driver — the browser counterpart to the
                    ALSA drivers; webmidi_js.go uses the Web MIDI API + p6.ParseMIDI
                    (webmidi.go is a no-op stub elsewhere)
midibridge/         (NO Fyne, NO p6 — pure Go, gomobile-bindable) the Android
                    MIDI transport bridge: the Java MidiManager layer reports
                    devices + shuttles bytes (AddDevice/RemoveDevice/SetOutput/
                    PushInput/Reset), the Go backends grab Writer(id)/OpenReader(
                    id). Used by p6/device_android.go + macropad_android.go. See
                    docs/android-midi.md for the Java contract + remaining work
internal/androidusb/ (android) reads USB-MIDI straight from Go over JNI — no
                    Java: driver.RunNative gives the JVM/Context, then it drives
                    UsbManager (enumerate/permission/bulkTransfer) and feeds
                    midibridge. usbmidi.go is the pure-Go USB-MIDI packet decoder
                    (DecodeUSBMIDI, unit-tested); androidusb_android.go is the
                    cgo/JNI reader; androidusb_cb.go the //export callbacks;
                    androidusb_stub.go a no-op elsewhere. Wired by cmd/rp6/
                    android.go (startAndroidMIDI). This is how Android USB MIDI
                    actually works today (see docs/android-midi.md)
internal/store/     sequence persistence (NO Fyne, NO p6); (profile,slot) ->
                    (name, JSON blob) + (profile,key) meta, every op scoped to a
                    profile; InsertGap/DeleteSlot shift slots so copy=insert,
                    delete=close. store_common.go holds the shared, backend-
                    agnostic bits; the backend is build-tagged per platform:
  store.go          (!js && !android && !ios) desktop: pure-Go modernc.org/sqlite,
                    DB at XDG_DATA_HOME/rp6 (legacy pre-profile DBs migrate their
                    rows to "p6" on open)
  store_mobile.go   (android||ios) a JSON file in private storage (modernc sqlite
                    trips Android's seccomp filter); store_js.go (js) a localStorage
                    JSON blob (no fs / no sqlite on wasm) — both mirror the SQLite
                    store's API + profile scoping exactly
internal/audio/     reusable audio capture (NO Fyne, NO p6)
  audio.go          Capturer interface, Peak/RMS, NormDB, Meter (smoothed VU)
  capture_stub.go   default (no tag): OpenCapture -> ErrUnavailable
  capture_malgo.go  //go:build capture: miniaudio/malgo capture backend
internal/jam/       host-side shared jam sessions (NO Fyne, NO p6, NO pion) —
                    peers broadcast live pad hits to each other. Generic +
                    callback-based like effects/sequencer, loopback-testable with
                    no network. DESKTOP-ONLY: compiled by default, dropped by
                    -tags nojam and on web/mobile. Full design: docs/architecture/jams.md
  jam.go            Engine: SendPad (async, off the UI/audio path) + OnPad, over a
                    pluggable Transport; message.go 4-byte codec; code.go session
                    codes; loopback.go in-memory Transport for tests
  webrtc/           (!nojam && !js && !android && !ios) pion/webrtc mesh transport:
                    unreliable/unordered data channel, WS signaling client,
                    supervised reconnect, RTT logging; doc.go is the !jam stub that
                    keeps pion out of the excluded builds
  signal/           the WS signaling hub (NO pion): path-gated /s/<code> + rate
                    limits/caps/keepalive; served by the cmd/rp6-signal binary.
                    App wiring is cmd/rp6/jam.go (+ jam_stub.go no-ops) — the JAM
                    button, join dialog, and broadcast/apply at the live pad sources
internal/ui/components/   GENERIC, reusable Fyne widgets (NO p6 import!)
  pad.go            Pad: colored, selectable, tap-flash key + bottom badge icons
  padgrid.go        PadGrid: generic paged, selectable grid (Cell/Badges/OnTrigger);
                    the page selector is a row of backlit RackToggles (PageAccent)
  stepbutton.go     StepButton: backlit physical seq-key: dim(off)/fully-lit
                    (programmed)/white-hot (playhead); SetAccent tints it with
                    the track's pad bank color
  sevenseg.go       SevenSeg: red 7-segment numeric readout
  knob.go           Knob: a machined gunmetal rotary cap (seated on the rack)
                    on the left + a dark LCD display (amber caption + value) to
                    its right; focusable (cap ring lights when focused); mouse
                     drag / scroll wheel / arrow keys change it. TEMPO + PATTERN,
                    and the sequencer's TRK (track count) + SEQ (slot) knobs
  transportbutton.go TransportButton: illuminated play/stop key (the triangle +
                    ■ are drawn as images, not font glyphs, so they render on web
                    too); NewTransportToggle is the toolbar's Play<->Stop toggle,
                    NewWalkerToggle the same toggle with walking-feet icons (the
                    sequencer's Play/Stop)
  levelmeter.go     LevelMeter: vertical segmented LED meter + peak hold
  led.go            LED: round status indicator, soft glow, optional breathe
  racktoggle.go     RackToggle: backlit rack-label on/off toggle (lit in accent
                    when on, greyed when off; hover brightens the lit state; an
                    "armed" state floods the whole plate with the accent color —
                    see SetArmed); text or icon; optional Ctrl+click alt action
                    (via desktop.Mouseable). Used for the bottom-bar section
                    toggles (PADS/DLY-REV/FX/SEQ/VU), the pad rack's left tool
                    strip (float/listen/density icons), the pad grid's A-D/E-H
                    page selector, the sequencer's armed-track mute + bar-length
                    controls (a shared second row), and the sequencer's per-track
                    pad-assign keys (tap to arm, then tap a pad to assign —
                    tinted with the track's pad bank color)
  devicebadge.go    DeviceBadge: backlit synth "nameplate" naming the connected
                    device (name + mode tag + status LED + settings gear),
                    backlit in an accent color; fixed footprint (never reflows).
                    DeviceState = Offline/Searching/Online. A plate tap fires
                    OnToggle (switch backend); the gear fires OnSettings.
                    Generic — the app supplies name/tag/accent (amber=P-6
                    hardware, cyan=emu) and the actions.
  rackpanel.go      RackPanel: gunmetal rack-unit frame with corner screws
internal/ui/layoutspec/  layout builder IR (NO p6): a tree of Nodes (Ref/RefWith,
                    VBox/HBox/Stack/Border/Split/Grid/RackPanel/Spacer/Separator)
                    that Build/BuildConfig turn into Fyne containers by resolving
                    Ref ids against a Registry; RefWith + a Configurator carry
                    per-widget properties. Full design: docs/architecture/layouts.md
internal/ui/layoutlang/  the text layout language (imports only layoutspec +
                    stdlib): Parse -> Document; Select(env) picks a `layout … when
                    …` variant and Rack(name,env) a `rack …` block, compiling to
                    layoutspec Nodes; conditions evaluate over an Env of flags.
internal/ui/theme/theme.go   Amber: dark Fyne theme with amber accent
cmd/rp6/padgrid.go    P-6 grid config + padID<->(bank,number) mapping + colors
cmd/rp6/effectsrack.go the "effects rack" for the selected pad (4 slots + Rate)
cmd/rp6/sequencerrack.go the step-sequencer panel (tracks + step grid + transport)
cmd/rp6/layout.go     loads the embedded UI layout, selects a variant per env,
                    composes rack internals + applies component properties
cmd/rp6/assets/*.layout  the compiled-in UI layouts (console + default/compact
                    variants and the shared `rack` blocks) — see docs/architecture/layouts.md
```

### The one rule that matters

- **`internal/ui/components` must never import `p6`** (or any device knowledge).
  Components are generic (a pad grid, a stepper, a meter — usable by any
  controller). Device-specific data (bank count, colors, `A1..H6` labels, note
  mapping, MIDI channels) lives in `cmd/rp6` and is passed in via config/callbacks.
- `p6` must never import Fyne. It's a pure MIDI library and is fully unit-tested
  without hardware by writing to an `io.Writer` (see `p6.New`).
- `internal/effects` is also generic — no Fyne, no `p6`. It fires pads through a
  `Trigger func(padID int)` callback and reads tempo via `SetTempo`. The app
  supplies `Trigger = firePad` (which sends the MIDI note). **`Trigger` may be
  called from a background roll goroutine**, so `firePad` reads the device under
  a mutex (`devMu`); `p6.Device.Send` is itself concurrency-safe.
- `internal/audio` is generic too (no Fyne, no `p6`): a `Capturer` yields raw
  float32 frames and a `Meter` smooths them to a 0..1 level. The app selects a
  `meterSource` (live `audioSource` if `OpenCapture` succeeds, else the decaying
  `activitySource`). Backend note: **PortAudio's Fedora build links JACK**, which
  isn't installed on a PipeWire box (link fails), so the backend is **malgo /
  miniaudio**, which `dlopen`s ALSA/PipeWire at runtime — no link-time audio deps.
- `internal/sequencer` is the same shape: a host-side step sequencer that fires
  pads through `Trigger func(pad int, vel uint8)` and reports the playhead via
  `OnStep` (both called from its clock goroutine — the app marshals `OnStep`
  through `fyne.Do`). It's a *parallel* sequencer (sound from the P-6, timing
  from the host); it does NOT program the P-6's own patterns (impossible), so
  run one or the other, not both.
- When something feels device-specific inside a component, it's usually just
  *configuration* — push the data out to the caller (see how `PadGrid` takes
  `Cell(page,row,col)`, `Badges(page,row,col)` and `OnTrigger`, while
  `cmd/rp6/padgrid.go` supplies the bank math).
- **The window layout is data, not imperative Go.** `internal/ui/layoutspec` +
  `internal/ui/layoutlang` (both generic — no `p6`) describe the UI in a small
  text language compiled into the binary (`cmd/rp6/assets/*.layout`); `relayout`
  selects a variant per form factor and builds it. Rearranging the UI — even the
  controls *inside* a rack — is a `.layout` edit + rebuild, not a Go change.
  Widgets are still built/wired in Go and referenced by ID. **Read
  `docs/architecture/layouts.md` before touching layout code** — it covers the
  compose-once (object→canvas 1:1) rule and the Android full-screen gotcha.

### The emulator (use rp6 without the hardware)

- `p6.Controller` is the swap point: everything the app needs from a "P-6"
  (trigger pads, CC/PC, transport, `Listen`, `Config`/`Path`/`Close`). Both the
  real `*p6.Device` and `*emu.Emulator` implement it, so `ui.dev` is a
  `p6.Controller` and `p6.NewClocker` takes a `p6.ClockTarget` (a subset).
- Run it with **`rp6 -emu /path/to/samples`** (or env **`RP6_EMU_SAMPLES`**).
  `openDevice()` picks the emulator when `u.useEmu` (loading `emu.Open(emuDir)`,
  or the embedded **built-in kit** via `emu.OpenDefault()` when no dir is set),
  else `p6.Open()`. `useEmu` starts from the `-emu` flag but is **togglable at
  runtime**: tap the top-rack DeviceBadge to switch backends
  (`toggleBackend`/`switchBackend` — to the P-6 only if one is `Discover`-able;
  to the emulator **always** — with no samples dir it loads the built-in
  "modular-hits" kit so it's playable out of the box). The badge's **gear** opens
  device settings: the emulator's has a **folder picker** (`setEmuSamples`) to
  choose/replace its samples directory. The picked folder goes through
  `resolveEmuSamples`: desktop uses the local path directly, but **Android**'s
  picker returns a Storage Access Framework `content://` tree whose `uri.Path()`
  (`/tree/primary%3A…`) isn't a filesystem path, so `emuimport_android.go`
  **copies** its WAVs (via Fyne's `storage`) into private storage and points the
  emulator there. Note SAF `URI.Name()` returns the full URL-encoded document id,
  so `leafName` decodes it to the real `BANK_A`/`PAD_1`/`kick.wav` names the
  loader expects. Switching stops transport, autosaves the
  current profile's sequence, reconnects, re-scopes the store to the new profile
  and reloads its sequence (`reconnectProfile`). The built-in kit's own profile
  is `"emu:builtin"`; its credits show in the ⓘ info dialog
  (`emu.DefaultKitAttribution`).
- **Same sample layout as the hardware:** the 48 pads are matched by pad label,
  case-insensitively — flat `A1.wav`..`H6.wav` in one dir, per-bank subdirs
  `A/1.wav`..`H/6.wav`, or the P-6's own export/import layout
  `BANK_A/PAD_1/*.wav`..`BANK_H/PAD_6/*.wav` (any WAV inside the pad dir); a
  trailing description is allowed (`A1 kick.wav`). First match per pad wins. So
  one directory feeds both the emulator and the P-6 SampleTool.
- The emulator **only plays pad triggers** — it has no internal
  sequencer/patterns/granular/FX, so `Start/Stop/Clock/ProgramChange/*CC` are
  accepted **no-ops** and `Listen` returns `p6.ErrNoInput` (the connect
  goroutine special-cases it). rp6's host-side step sequencer still works: it
  fires pads via `firePadVel` → `TriggerPadVelocity`, which the emulator plays.
- **Sequences are profile-scoped** so they don't intermingle with the P-6's:
  each `-emu` samples directory persists under its own `"emu:<abs-dir>"` profile
  in the store, and hardware/no-`-emu` runs use `"p6"` (see §3 store notes).
- **Audio is behind the `capture` build tag** (same as the VU meter): with it,
  `sink_malgo.go` opens the default output via malgo/miniaudio; without it,
  `sink_stub.go` is a **silent** sink so the emulator still loads + mixes (fully
  testable — `go test` needs no audio). WAV decode/mixer/loader are pure Go and
  unit-tested; samples are resampled once to the sink format on load.

---

## 4. P-6 MIDI reality (READ THIS before hardware features)

Sources: official Roland P-6 Owner's Manual (MIDI Implementation Chart + Control
Change message page, firmware v1.02) and the community CSV
(`github.com/pencilresearch/midi`, `Roland/P-6.csv`). All of the following was
also **verified live** against real hardware.

### Connection
- USB-C = class-compliant **USB MIDI + USB Audio**, no driver.
- On Linux it's an ALSA card named `P-6`; the raw MIDI node is
  `/dev/snd/midiC<card>D0`. `p6.Discover()` finds it by scanning
  `/proc/asound/cards`. We open the node **O_RDWR** and **write MIDI bytes
  straight to it** — pure Go, no `librtmidi`/`portmidi`, no cgo. (Fyne brings
  cgo; `p6` itself does not.) `Device.Listen` reads the **input** direction and
  parses it (`input.go`): the app reflects hardware pad presses (Note On on the
  Sampler channel) by selecting+flashing the pad and nudging the meter — it does
  NOT re-trigger (the hardware already played the sound). Needs the P-6 to
  actually transmit pad presses (**verify on hardware**).
- The rawmidi node is **exclusive**: if the GUI holds it open, `amidi` etc. get
  "Device or resource busy", and vice-versa. Close one to use the other.
- **Robustness / non-happy paths.** `p6.OpenPath` classifies open failures into
  `p6.ErrBusy` (port held by another program) and `p6.ErrPermission` (not in the
  `audio` group / udev), alongside `p6.ErrNotFound`; `connect()` (in `main.go`)
  matches these with `errors.Is` to show cause-specific status. `connect()` is
  guarded (`u.connecting`) against overlapping calls and stamps each connection
  with a generation (`u.devGen`). **Mid-session unplug** is detected
  from write/read failures: `p6.Clocker.SetOnError` reports a failed clock pulse
  once per Start, `firePadVel`/PC/FX writes and the `Listen` goroutine's read
  error all funnel into `u.deviceFailed(gen, err)`, which flips the UI to
  disconnected exactly once per connection (generation + `u.devLost` guards) and
  is safe from any goroutine (`fyne.Do`). `close()` bumps the generation so
  teardown-time goroutine failures don't marshal onto the tearing-down loop.
  The **top-rack DeviceBadge** reflects this lifecycle: cyan=emulator,
  amber=P-6, plus Offline/Searching/Online state.
- **Auto-fallback to the emulator + auto-reconnect (no Reconnect button).** If
  no P-6 is present at launch (`main()`: `!useEmu && dev == nil`) or one is lost
  mid-session (`deviceFailed` while `!useEmu`), the app calls `fallbackToEmu()` →
  `switchBackend(true)`, loading the built-in kit so it stays playable, and sets
  `u.emuFallback`. A background **device watcher** (`startDeviceWatch`, 2s tick)
  polls `p6.Discover()` while `emuFallback` is set and, on a **rising edge** (a
  P-6 newly appearing — so a present-but-busy device can't cause a retry storm),
  auto-switches back to hardware (`switchBackend(false)`). A **deliberate**
  emulator choice (`-emu`, tapping the badge, or picking a samples folder)
  clears `emuFallback`, suppressing the auto-switch; tap the badge to
  force hardware.

### Default MIDI channels (factory; stored on device, changeable in MENU)
- **11** = Sample pads
- **4**  = Granular pad
- **15** = Auto (currently-selected pad + Control Change)
- **16** = Program Change (pattern select)

### What you CAN control
- **Trigger any of the 48 pads**: Note On, note numbers **48–95** (C3–B6) on the
  Sampler channel. `note = 48 + bankIndex*6 + (pad-1)`, bankIndex 0=A..7=H.
  Note-Off is **ignored** — the pad's own GATE/one-shot/loop governs the tail.
- **Switch patterns**: Program Change **0–63** on the PC channel. Requires the
  device setting **`rxPc` = On**. (Verified working.)
- **Transport / tempo**: MIDI **Start/Stop/Continue + Clock**. Requires device
  setting **MIDI Clock Sync (`SYnC`) = USB**. Crucially, `Start` alone does
  nothing visible — the sequencer only advances on a stream of **clock pulses**
  (24 PPQN). That's why `p6.Clocker` streams `0xF8` at the set tempo; the tempo
  slider *is* the P-6's tempo when clock-slaved. (Verified: Play/Stop/tempo all
  work once `SYnC=USB`.)
- **Granular engine + global Delay/Reverb** via Control Change (see `p6/cc.go`).

### What you CANNOT control (do not build these; verified impossible)
- **Switching the selected bank** (A/E–D/H buttons). No SysEx, no CC (CC0/CC32
  Bank Select are unassigned). You don't need it: address pads absolutely by note
  and keep the "current page" as PC-side state (that's exactly what the UI does).
- **Any sample-pad parameter** — LOOP, GATE, playback direction, level, filter,
  lo-fi *of a sample pad*. There is no MIDI for these. Verified live: sending
  `CC7 (Level)=0` on ch15 did **not** silence the selected sample pad.
- **Moving the hardware's selected pad/bank remotely.** Triggering a pad by note
  does NOT change what's selected on the device. So CCs on the Auto channel hit
  whatever the *user* physically selected — you can't steer it. This is why the
  early "Lo-Fi/Loop on selected pad" toolbar idea was dropped.
- **Reading audio levels / master volume.** Master volume is a physical knob;
  output level isn't exposed over **MIDI**. But the P-6 is also a class-compliant
  **USB-audio** device with a capture endpoint (`/dev/snd/pcmC<card>D0c`), so the
  real output level *can* be metered by capturing that stream — see `internal/
  audio` and the `capture` build tag. Without the tag the "master meter" falls
  back to app-triggered pad **activity** (a decaying VU).

### Control Change summary
Every CC the P-6 accepts targets the **GRANULAR** engine or the **global
Delay/Reverb**, received on ch4 (granular) or ch15 (auto), v1.02+. There are
**no NRPNs** and **no SysEx**. See `p6/cc.go` for the mapped numbers
(e.g. `CCLoFiSwitch=87`, `CCDelayTime=90`, `CCReverbLevel=91`, `CCSample=88`
selects the granular source A1..H6).

---

## 5. Go conventions

- Go 1.26; module `github.com/mono4loop/rp6`.
- Format with `gofmt`/`go fmt`; keep `staticcheck` and `go vet` clean.
- Errors: wrap with `%w` and a `p6:` prefix in the library.
- Prefer small, testable units. `p6` has no I/O dependencies in tests
  (`p6.New(io.Writer, cfg)` + a `bytes.Buffer`, or a mutex-wrapped buffer for
  the concurrent `Clocker`).

---

## 6. Fyne / UI conventions & gotchas

- Custom widgets: embed `widget.BaseWidget`, call `ExtendBaseWidget(self)`,
  implement `CreateRenderer`. Renderers implement `Layout/MinSize/Refresh/
  Objects/Destroy`. See any file in `components/` for the pattern.
- **Modifier+click** (e.g. Ctrl+click the seq Clear key to delete the whole
  sequence) isn't available
  from `Tapped` (a `*fyne.PointEvent` has no modifiers). Implement
  `desktop.Mouseable` and capture `MouseDown(*desktop.MouseEvent).Modifier`
  (fires before `Tapped`); see `components.RackToggle`.
- **Background-goroutine UI updates must go through `fyne.Do(func(){...})`**
  (Fyne ≥ 2.6). Used by the pad/transport tap-flash and the meter animator.
  Do NOT call `fyne.Do` from an async path in a way a test asserts synchronously
  — it caused a real test regression once (an async re-select clobbered typed
  text). Keep committed/asserted state synchronous.
- **GL vsync used to be force-disabled at startup — no longer.** Older Fyne
  (2.7) with the X11/GLX driver froze the whole UI when swapping a maximized/
  occluded *second* window (`main()` set `vblank_mode=0`/`__GL_SYNC_TO_VBLANK=0`
  before driver init to work around it — the render loop calls `glfwSwapBuffers`
  **synchronously for every dirty window**, so a vsync-gated swap blocked for
  seconds; confirmed by a goroutine dump stuck in `_Cfunc_glfwSwapBuffers`).
  Fyne **2.8** on the native **Wayland** driver fixes this, so the env-var hack
  is gone. The `RP6_DIAG=1` probe (`startDiagnostics`) still logs UI-loop lag +
  dumps goroutine stacks to `/tmp/rp6-stall.txt` when the loop freezes >1.5s.
- **Coalesce periodic `fyne.Do` updates — don't flood the loop.** Fyne runs one
  render loop for all windows; `fyne.Do` closures queue on it. Periodic animators
  (the VU meter and the LED breathe, both 40 ms) skip posting while their
  previous update is still pending (an `atomic.Bool` guard), so a slow frame
  can't grow the queue unboundedly. (Hygiene — the historical multi-window freeze
  was the vsync/`SwapBuffers` stall above, not queue flooding.)
- **Toggling visibility doesn't relayout by itself.** Fyne's border layout
  respects `Visible()`, but you must trigger a relayout: keep a reference to the
  container (`u.root`) and call `.Refresh()` after `Show()/Hide()` (see
  `toggleVisible`, the shared show/hide helper behind every section toggle).
- **Don't move a CanvasObject tree between windows — rebuild it.** Fyne keys the
  object→canvas association in a global 1:1 map (`internal/cache/canvases.go`,
  `SetCanvasForObject` uses `LoadOrStore`), so re-parenting the *same* tree to a
  second window via `SetContent` leaves its children pointing at the old canvas:
  refresh routing and per-window textures break, and a maximized second window
  mis-renders / re-rasterizes and goes unresponsive. So float/dock **rebuilds**
  the pad rack fresh for the host window (`buildPadRack`, re-applying button and
  selection state) rather than moving `padRackObj`. Each window then owns its
  objects, so normal `Refresh()` works there (flash included) with no repaint
  hacks. (Docking a rack that stays in the *same* window — e.g. the sequencer
  side-column, or the density grid swap — is fine; that's not a window change.)
- **Keep widget footprints fixed if they swap content**, or the layout jumps —
  either reserve the space (`container.NewGridWrap`) or make the widget always
  render the same layout rather than hiding/showing sub-objects on state change.
- **Focus/first-show is fragile.** An editable widget created hidden and focused
  on first show may not behave (this bit an earlier double-click inline editor).
  The reliable fix was to keep such a widget **always visible** — no hide/show,
  no first-show focus penalty — rather than revealing it on demand.
- Icons: don't rely on font glyphs for important shapes — the `▶`/`■` glyphs
  looked bad and don't render on web, so the transport Play triangle and Stop
  square are both drawn as antialiased images (`triangleImage` / `squareImage`).
- Theme: `internal/ui/theme.Amber` overrides `Primary/Hyperlink/Focus/Selection/
  Hover` to amber and forces the dark variant. Accent = `#E1873B` (bank-B
  orange) chosen so **white** text stays readable on highlighted buttons.
  Applied in `main` via `a.Settings().SetTheme(uitheme.Amber{})`.

---

## 7. Testing patterns & gotchas

- Use `testify` (`assert`/`require`).
- Drive real UI behavior with the `fyne.io/fyne/v2/test` package:
  `test.NewApp()`, `test.NewWindow(obj)`, `test.Tap`, `test.DoubleTap`,
  `test.Type`, `w.Canvas().Focus/Focused()`, `Entry.SelectedText()`. This is how
  we proved focus/selection logic instead of theorizing.
- **The default test theme lacks a bold-monospace font**, so rendering monospace
  `canvas.Text` (the LCD) panics in the headless driver. Fix in test setup:
  `a := test.NewApp(); a.Settings().SetTheme(theme.DefaultTheme())`. Done in
  `newTestUI` and the LCD/transport tests.
- **Don't start long-lived goroutines in `build()`** — the meter animator and
  the LED breathing pulse (`LED.StartPulse`) are started in `main()`, not
  `build()`, so tests that call `build()` don't spawn ticking goroutines that
  outlive them.
- For concurrent writers (the `Clocker`), tests use a mutex-wrapped buffer
  (`safeBuf`/`syncBuf`) and assert loosely (starts with `0xFA`, contains `0xFC`).
- **`go test -race` flags the tap-flash / pulse goroutines** (pad, transport,
  LED). This is a *test-driver artifact*, not a production race: the real GLFW
  `fyne.Do` marshals the closure onto the main event loop (same goroutine as the
  renderer), whereas the headless test driver runs it inline on the worker
  goroutine. The project gate is plain `go test` (see `make check`); don't add
  mutexes to widget render paths to chase this.
- Limits of the harness: it can't reproduce **real GLFW double-tap detection** or
  **OS window focus**, and it realizes widgets immediately (so it won't reproduce
  first-show timing bugs). Say so honestly rather than guessing.

---

## 8. Testing against real hardware

The P-6 responds to raw MIDI via ALSA tools (quit the GUI first — exclusive
port):

```bash
amidi -l                                   # list; confirm "P-6" is present
amidi -p hw:3,0,0 --send-hex "9A 30 64"    # NoteOn ch11 note48(=A1) vel100 -> trigger bank A pad 1
amidi -p hw:3,0,0 --send-hex "9A 5F 64"    # note95 = bank H pad 6
amidi -p hw:3,0,0 --send-hex "BE 07 00"    # CC7(Level)=0 on ch15 (granular/selected)
amidi -p hw:3,0,0 --send-hex "CF 05"       # Program Change 5 on ch16 (pattern; needs rxPc=On)
```

Status byte = `type_nibble | (channel-1)`: NoteOn ch11 = `0x9A`, CC ch15 =
`0xBE`, PC ch16 = `0xCF`. Realtime: Start `0xFA`, Stop `0xFC`, Continue `0xFB`,
Clock `0xF8`.

When asking a human to verify, watch the **pad/bank LEDs and the 4-char display**
and report sound — that's how the "banks don't switch" / "CCs don't hit sample
pads" facts were nailed down.

---

## 10. Ideas / not-yet-built

- Velocity control for pad hits (currently fixed `p6.DefaultVelocity`).
- More effect kinds in `internal/effects` (probability, ratchet, delay-throw…):
  add a `Kind`, an `Icon()`, and behavior. **Roll** is the only one implemented;
  it's a host-side *retrigger*, not the hardware LOOP (which has no MIDI). A pad
  with a Roll slot toggles rolling on tap, tempo-synced to the current BPM.
- PC-keyboard shortcuts (keys → visible row of pads, space = Play/Stop).
- A scripting CLI over the `p6` package (`rp6 pad E1`, `rp6 pattern 5`, ...).
- A capture-based looper (record the P-6's audio via `internal/audio` and loop
  it host-side) — the `Capturer` interface already yields the raw frames it needs.
