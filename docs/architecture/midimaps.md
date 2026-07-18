# MIDI maps (pluggable input controllers)

**Status: implemented.** This documents how RP6 supports external MIDI *input*
controllers (drum pads, keyboards, grid controllers) by shipping a **text map
file** instead of writing a Go driver, so new hardware becomes a data edit, not a
code change. It builds on the `internal/midiin` framework and the `p6` MIDI
parser. Read `internal/midiin/midiin.go` and `docs/architecture/layouts.md`
first — the map language deliberately mirrors the `.layout` precedent
(config-as-text compiled against Go-owned objects). The Adafruit MacroPad and the
Arturia keyboards were originally hand-written Go drivers and are now shipped as
`.midimap` files (`cmd/rp6/assets/midimaps/`).

The worked example throughout is the **Synido TempoPAD C16**, a 16-pad, 4-bank,
fully-assignable controller — the stress test for "drive most of RP6 from one
device."

---

## 1. What it is (and isn't)

A **MIDI map** is a small declarative file that binds incoming MIDI messages
from one controller to RP6 **control intents** (named actions like
`transport.toggle` or `pad.trigger`). A generic interpreter reads the file,
parses incoming MIDI with the existing `p6.ParseMIDI`, and dispatches intents to
the app. Adding a controller = dropping in a `.midimap` file.

What it deliberately is **not**:

- **Not a scripting engine.** Unlike Mixxx (XML + JavaScript) or Bome MIDI
  Translator (rules/variables), there is no embedded language. A binding is a
  static message→intent rule with a few typed modifiers. If a controller needs
  real logic, write a Go driver (the `midiin.Driver` seam stays open — that's
  what a hand-written driver like the Web MIDI one is).
- **Not outbound / no LED feedback.** RP6 only *consumes* controller output. We
  never light the controller's pads or drive its displays. (Confirmed moot for
  the C16 anyway: its LEDs are local-only and reject host MIDI.)
- **Not device parameter editing.** The map describes what the controller
  *sends*; how the controller is *configured* to send it is the paired **device
  template** (§10), a human setup note — not something RP6 writes.
- **Not hot-reloaded per keystroke.** Maps load at startup (embedded defaults +
  a user directory) and are re-scanned when a device is (re)attached — no file
  watcher. This differs from `.layout` (embed-only): device support is meant to
  be **user-extensible**, so a user directory is the whole point (§9).

### Why a vocabulary, not raw MIDI passthrough

The single most important decision is **§4, the control vocabulary** — the fixed
set of named intents a map can target. It is the stable contract that makes maps
portable, shippable and testable, and it keeps device specifics out of the app
(the same discipline that keeps `p6` out of `internal/ui/components`). This
mirrors Ardour's `.map` model (`function="transport-start"`), which is the right
inspiration; we adopt its *model*, not its XML.

---

## 2. Package boundaries

```
cmd/rp6/                       the app — OWNS the vocabulary
  midimap.go        (new)      builds the intent Dispatch from u's handlers;
                               loads embedded + user maps; registers a driver
                               per map; embeds assets/midimaps/*.midimap
  assets/midimaps/*.midimap    shipped maps (synido-c16.midimap, …)

internal/midiin/               generic framework (NO Fyne, NO p6-model, NO vocab)
  midiin.go                    Handlers/Device/Driver/Register/Detect/Present
  alsa.go                      FindRawMIDI (shared discovery)
  mapped/           (new)      the generic map interpreter
    mapfile.go                 Parse(text) -> Map (bindings + match + meta)
    device.go                  a midiin.Device: p6.ParseMIDI -> match -> Dispatch
    encoder.go                 relative-CC decoders (two's-comp / signed-bit / …)
```

The layering rule (same as everywhere else in RP6):

- **`internal/midiin` and `internal/midiin/mapped` never learn the RP6
  vocabulary.** The interpreter matches MIDI and emits an abstract
  `mapped.Intent{Name string; …args}`. It does not know that `transport.toggle`
  exists.
- **`cmd/rp6` owns the vocabulary.** It builds a `Dispatch` — a
  `map[string]func(mapped.Intent)` (or a switch) — wiring each intent name to an
  existing app handler (`u.play`, `u.firePadVel`, the TEMPO knob, …). Unknown
  intent names in a map are a **load-time error** (validated against the
  registered names), never a silent no-op.
- **`p6` stays the parser.** `mapped/device.go` reuses `p6.ParseMIDI` (running
  status, interleaved realtime, velocity-0→NoteOff) exactly like the MacroPad
  driver — no re-implementation. Note SysEx is currently **skipped** by the
  parser (`p6/input.go` `skipSysex` emits nothing), which is why MMC needs a
  small `p6` change — see §8.

```
   controller ──raw MIDI──► p6.ParseMIDI ──Event──► mapped.Map.match
                                                        │  (data, from .midimap)
                                                        ▼
                                              mapped.Intent{Name,args}
                                                        │
                                                        ▼
                              cmd/rp6 Dispatch ──► u.play / u.firePadVel / …
```

### The `midiin` surface

`Device.Run(h Handlers)` takes the typed callbacks (`TriggerPad`/`PlayNote`/
`Transport`) that hand-written drivers (e.g. the web-only Web MIDI driver) use,
plus a `Dispatch func(Intent)` sink that the mapped device targets. The app wires
both in `midiInHandlers` (`cmd/rp6/main.go`): the typed callbacks and `Dispatch`
funnel into the same underlying handlers (`fireExternalPad`, `playExternalNote`,
play/stop), so a mapped controller and a hand-written one behave identically.

---

## 3. Intent shapes

Every binding has a **source** (a MIDI message pattern) and a **target** (an
intent). Sources come in shapes; intents accept a matching shape:

| Shape | Source example | Intent arg | Used for |
|---|---|---|---|
| **trigger** | Note On, or CC≥threshold | velocity (1..127) | firing pads/notes |
| **button** | Note/CC press (`when value=127`) | — | discrete actions (play, next page) |
| **toggle** | CC alternating 127/0 | bool (on/off) | on/off state (mute, listen) |
| **value** | absolute CC / fader (0..127) | float 0..1 | continuous params (delay, reverb) |
| **delta** | relative encoder (±1 detents) | signed int steps | tempo, pattern, seq slot |

Rules:

- A **value** intent (`*.set`) receives a **normalized 0..1**; the app scales to
  the parameter's real domain and clamps. A binding may sub-range with
  `scale=lo..hi` (in MIDI units) before normalizing.
- A **delta** intent (`*.delta`) receives a **signed step count**; the binding's
  `rel=<encoding>` (§7) decodes the encoder's wire format to ±N detents.
- A **trigger** carries velocity (respecting the P-6's velocity, subject to the
  eye/listen toggle — same gate as `fireExternalPad`).
- A **button** fires once per qualifying message; `when value=…` filters which.

---

## 4. The control vocabulary

The named intents a map may target. Grounded in what RP6/​P-6 can actually do
(see AGENTS.md §4 "P-6 MIDI reality" — several tempting controls are impossible
and are absent here on purpose). The set is **extensible**: adding an intent is a
new entry in the app's `Dispatch` + a row here.

### Pads & keyboard

| Intent | Shape | Notes |
|---|---|---|
| `pad.trigger` | trigger | absolute pad id 0..47 (`= bank*6 + (pad-1)`); needs `id=` from the matched note (§5) |
| `pad.trigger.rel` | trigger | pad `offset` within the **current input bank** register (§6); for controllers that don't page in firmware |
| `note.play` | trigger | chromatic note → on-screen keyboard (pitches the selected sample) |

### Grid paging (UI state — *not* the P-6's own bank)

| Intent | Shape | Notes |
|---|---|---|
| `page.select` | button | arg `a_d` \| `e_h` — which visible pad page |
| `page.next` / `page.prev` | button | cycle the visible page |
| `density.toggle` | toggle | double-density (all 8 banks) |

> The hardware's selected bank **cannot** be moved over MIDI (verified
> impossible). `page.*` only steers RP6's *UI*; pads are always addressed
> absolutely by `pad.trigger`.

### Transport, tempo, pattern

| Intent | Shape | Notes |
|---|---|---|
| `transport.play` / `.stop` | button | drives `u.play` / `u.stop` + the transport light |
| `transport.toggle` | button | flips play/stop |
| `transport.set` | toggle | explicit play/stop from a bool source; the well-known intent `Handlers.Transport` becomes (§2) |
| `tempo.set` | value | scaled to the TEMPO range |
| `tempo.delta` | delta | ± BPM steps (encoder) |
| `pattern.set` | value | P-6 pattern 0..63 via Program Change (needs `rxPc=On`) |
| `pattern.delta` | delta | ± pattern (encoder) |

### Global P-6 effects (the only pad-adjacent CCs the P-6 accepts)

| Intent | Shape | Notes |
|---|---|---|
| `delay.set` | value | `CCDelayLevel` (92) |
| `reverb.set` | value | `CCReverbLevel` (91) |
| `delay.time.set` / `reverb.time.set` | value | `CCDelayTime`/`CCReverbTime` |

> Per-sample-pad params (LOOP/GATE/level/filter) have **no MIDI** and are not in
> the vocabulary. Don't add them; verified impossible.

### Host step sequencer

| Intent | Shape | Notes |
|---|---|---|
| `seq.play` / `seq.stop` / `seq.toggle` | button | the software sequencer's own transport |
| `seq.slot.delta` | delta | change the saved sequence slot (quantized to next bar while playing) |
| `seq.track.arm` | button | arg `n` — arm track n (then a pad hit assigns it) |
| `seq.track.mute` | toggle | mute the armed track |
| `seq.tracks.delta` | delta | active track count |
| `seq.bars.cycle` | button | armed track bar length 1→4 |
| `seq.clear` | button | clear the grid |

### Recorder / looper (LOOP page)

| Intent | Shape | Notes |
|---|---|---|
| `rec.arm` | button | arm next track for record-on-pad |
| `rec.play` / `rec.stop` | button | arg `track` (or `all`) |
| `rec.track.mute` / `.solo` | toggle | arg `track` |

### App shell

| Intent | Shape | Notes |
|---|---|---|
| `app.page.play` / `app.page.loop` | button | PLAY/LOOP page nav |
| `view.toggle` | toggle | arg `pads`\|`seq`\|`fx`\|`dlyrev`\|`vu`\|`keys` |
| `input.bank.set` / `input.bank.next` | button | set the interpreter's input-bank register (§6) |

---

## 5. The `.midimap` language

A small text DSL, C-style `//` and `/* */` comments, `;` optional. One
`device` block per file (a file *is* one controller).

```
device "<display name>" {
  match   "<substr>", "<substr>"      // /proc card name / bridge port name (case-fold)
  channel <1..16>                     // optional default channel filter for all bindings

  <binding>
  <binding>
  …
}
```

A **binding** is `<source> -> <intent> [args]`:

```
source   := note <n | lo..hi>
          | cc   <n>
          | pc                        // Program Change
          | mmc  <play|stop|record|…> // MIDI Machine Control (SysEx)
          | realtime <start|stop|continue>

modifier := on <ch>                   // per-binding channel override
          | when value=<v>            // only when the data value equals v
          | when value>=<v>           // …or a threshold (for CC-as-trigger)
          | abs [scale=<lo>..<hi>]    // absolute value  → value shape
          | rel=<encoding>            // relative encoder → delta shape (§7)
          | offset=<n>                // note→pad id base for pad.trigger

intent   := <name from §4> [arg]
```

Worked lines:

```
note 48..95           -> pad.trigger      offset=48     // note 48 = pad 0 (A1)
note 60               -> note.play                       // keyboard
cc 49 when value=127  -> transport.toggle                // toggle button
cc 16 rel=twoscomp    -> tempo.delta                     // endless encoder
cc 20 abs scale=0..127 -> delay.set                       // fader → 0..1
pc                    -> pattern.set                      // PC value → pattern
mmc play              -> transport.play                   // if device is in MMC mode
```

### Note ranges → pad ids

`note lo..hi -> pad.trigger offset=lo` maps a contiguous note block to pad ids
`note-offset`. This single line covers the whole 48-pad P-6 grid when the
controller emits absolute notes 48..95 (the MacroPad and a firmware-banked C16).
Non-contiguous drum layouts use an explicit table instead:

```
note map { 36->0, 38->1, 40->2, 42->3, … } -> pad.trigger
```

---

## 6. Input banks (state for controllers that don't page in firmware)

The **C16 pages in firmware**: PAD BANK A/B/C/D changes what notes the 16 pads
emit, so RP6 sees absolute notes and needs **no state** — `note 48..95 ->
pad.trigger offset=48` reaches all 48 pads across the device's 4 banks.

A dumber 16-pad controller sends the *same* 16 notes plus a separate bank
button. For those, the interpreter keeps one integer, the **input-bank
register**:

```
cc 50 when value=127  -> input.bank.next          // bank button
note 36..51           -> pad.trigger.rel offset=36 // 16 pads, relative to bank
```

`pad.trigger.rel` computes `id = bank*16 + (note-offset)`. This is the *only*
piece of interpreter state, and it stays declarative (a register + two intents),
not a scripting language. The C16 map doesn't use it.

---

## 7. Relative-encoder encodings

Endless encoders send *relative* CC; cheap controllers disagree on the wire
format, so `rel=<encoding>` selects the decoder (`mapped/encoder.go`):

| `rel=` | Wire format | Example |
|---|---|---|
| `twoscomp` | signed two's complement around 0 (`0x01`=+1, `0x7F`=−1) | most "relative 2" |
| `signbit` | bit 6 = direction, low bits = magnitude (`0x41`=+1, `0x01`=−1) | "relative 1" |
| `binoffset` | offset-64 (`65`=+1, `63`=−1) | "relative 3" |

Each decodes to a signed detent count fed to the `*.delta` intent. Absolute
faders/knobs use `abs` instead and target `*.set`.

---

## 8. Transport: CC vs MMC

The C16's Play/Stop/Record buttons send **either CC or MMC**, user-selectable.

- **CC (recommended):** set the buttons to CC and bind `cc N when value=127 ->
  transport.*`. Trivial; no new decoding.
- **MMC (optional):** MMC is short SysEx (`F0 7F <dev> 06 <cmd> F7`; `01`=stop,
  `02`=play, `06`=record). **Not free today:** `p6.ParseMIDI` currently
  *discards* SysEx payloads (`skipSysex` emits only interleaved realtime), so
  `Event`s never carry the MMC command. Supporting `mmc <cmd>` needs a small,
  contained `p6` change — surface a new `EventSysEx` (or `EventMMC`) carrying the
  payload — after which `mapped/device.go` matches it. Ship CC-only first; add
  `mmc` (and the parser change) when a target device makes it necessary.

---

## 9. Discovery & loading

Mirrors the input-controller path (`startMIDIInput`, `cmd/rp6/main.go`) with maps
supplying the driver data:

1. At startup `cmd/rp6/midimap.go` parses **embedded** maps
   (`//go:embed assets/midimaps/*.midimap`) and, if present, files in the **user
   directory** `XDG_CONFIG_HOME/rp6/midimaps/*.midimap` (user files override
   embedded ones by device name). Parse/validation errors are logged and skip
   that file — a bad map never breaks launch.
2. For each valid map it calls `midiin.Register` with a `Driver`:
   - `Detect = func() (string,bool) { return mapped.Detect(map.Match) }` — a
     build-tagged seam: the ALSA `/proc/asound/cards` scan on desktop
     (`platform_alsa.go`), the `midibridge` input-name scan on Android
     (`platform_android.go`), a no-op on the web build (`platform_js.go`).
   - `Open = func(path) { return mapped.Open(path, map) }`; the per-app intent
     `Dispatch` arrives later through `Device.Run(Handlers)`.
3. `midiin.Present()`/`Detect()` and the 2s attach poller then treat mapped
   controllers like any driver — several can be open at once.

Because registration is data-driven, **the user adds a file and relaunches** —
no rebuild. (Shipping a *new* map in the repo is `assets/midimaps/` + rebuild,
like `.layout`.)

---

## 10. The Synido TempoPAD C16 pair

Because the C16 is fully assignable, a map is only meaningful against a known
device configuration. The shippable unit is a **pair**: a setup template + the
matching map. This is the explicit analogue of the MacroPad's firmware
(`docs/hardware/macropad`).

### Device template (set on the C16, on-device or in the companion app)

- **PAD BANK A** → Notes **48–63**, channel 1, momentary.
- **PAD BANK B** → Notes **64–79**.
- **PAD BANK C** → Notes **80–95**. (A–C cover P-6 banks A–H: 48 pads.)
- **PAD BANK D** → free (leave as notes or reuse for a second layout).
- **Transport Play / Stop** → **CC 118 / CC 119**, channel 1, momentary (value
  127 on press).
- **Encoder 1** → **CC 16**, relative (two's-complement) → tempo.
- **Encoder 2** → **CC 17**, relative → pattern.
- **Fader 1 / Fader 2** → **CC 20 / CC 21**, absolute → delay / reverb.
- **Buttons** (any 6) → **CC 80–85**, toggle → sequencer + view toggles.

### `cmd/rp6/assets/midimaps/synido-c16.midimap`

```
// Synido TempoPAD C16 — pair with the device template in
// docs/architecture/midimaps.md §10. Pads must be set to absolute notes 48..95
// across PAD BANK A/B/C (firmware paging), channel 1.
device "Synido TempoPAD C16" {
  match   "tempopad", "c16"
  channel 1

  // --- pads: 3 device banks emit notes 48..95 = P-6 A1..H6 (no interpreter state)
  note 48..95            -> pad.trigger        offset=48

  // --- transport (CC, momentary)
  cc 118 when value=127  -> transport.play
  cc 119 when value=127  -> transport.stop

  // --- endless encoders (relative) → continuous host params
  cc 16 rel=twoscomp     -> tempo.delta
  cc 17 rel=twoscomp     -> pattern.delta

  // --- faders (absolute) → global P-6 delay/reverb
  cc 20 abs              -> delay.set
  cc 21 abs              -> reverb.set

  // --- assignable buttons (toggle) → sequencer + view
  cc 80 when value=127   -> seq.toggle
  cc 81 when value=127   -> seq.track.mute
  cc 82 when value=127   -> page.next
  cc 83                  -> view.toggle   seq
  cc 84                  -> view.toggle   fx
  cc 85                  -> app.page.loop

  // Optional: if you leave transport buttons in MMC mode instead of CC,
  // comment out the cc 118/119 lines and use:
  // mmc play             -> transport.play
  // mmc stop             -> transport.stop
}
```

That single file drives pads (all 48), transport, tempo, pattern, delay, reverb,
the sequencer and page/rack navigation — "most of RP6" from one controller, no
Go code.

---

## 11. Testing

Maps are pure data + a pure interpreter, so they unit-test with **no hardware and
no Fyne**, exactly like `internal/midiin/mapped/device_test.go`:

- Feed a byte stream into `mapped.Device.Run` (an `io.Reader`) and assert the
  ordered `Intent`s captured by a fake `Dispatch`. E.g. with the C16 map,
  `90 30 64` (NoteOn ch1 note48 vel100) → `Intent{Name:"pad.trigger", Pad:0,
  Velocity:100}`, while an off-channel note is filtered out.
- Parser tests: a `.midimap` string → `Map`, asserting bindings, and that an
  **unknown intent name** or malformed source is a parse error.
- Encoder tests: `rel=twoscomp` `0x7F` → delta −1, etc.
- A repo-level test validates every **embedded** map: it parses and every intent
  name resolves against the app's registered vocabulary (guards against a map
  drifting from a renamed intent).

---

## 12. Open questions

- **Vocabulary surface vs. the `Handlers` struct.** Grow `Handlers` into an
  intent dispatch (recommended) vs. keep `Handlers` and add a parallel sink.
  Settle before coding (affects `midiin` and `midiInHandlers`).
- **Value smoothing.** Absolute faders jumping to `*.set` can snap a param;
  decide whether the app soft-takes-over (catch on cross) — an app concern, not
  the map's.
- **Multiple maps, same device.** User dir overrides embedded by device name;
  do we also allow layering (a user "patch" on top of an embedded base)? Start
  with whole-file override.
- **MMC scope.** Ship CC-only first; add `mmc` sources when a target device
  needs it.
- **Per-map channel learn.** A future `rp6 midimap learn` CLI could capture a
  controller's messages into a starter `.midimap` (the Mixxx/Ardour "MIDI learn"
  wizard idea), lowering authoring cost further.
```