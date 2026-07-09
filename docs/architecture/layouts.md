# UI layouts

Developer notes on RP6's **declarative layout system**: how the UI arrangement
is described in a small text language (compiled into the binary), how variants
adapt it to the device/form factor, and the constraints — several of them
Fyne-specific — that shaped the design. If you just want to *rearrange* the UI,
skip to §9 (edit `cmd/rp6/assets/*.layout` and rebuild).

---

## 1. What it is (and isn't)

The window layout is **data**, not imperative Go. Instead of hand-assembling
`container.New…` trees in `relayout()`, the app:

1. builds and wires every widget in ordinary Go and registers it under a stable
   **ID** (`pads`, `transport`, `vu`, …),
2. parses a text **layout document** (compiled in via `//go:embed`) that says
   *where* those IDs go, per form factor, and
3. realizes the selected arrangement into a real Fyne object tree.

So **structure/placement lives in the layout file; behavior/wiring stays in Go.**
Rearranging the UI — including the controls *inside* a rack — is a file edit +
rebuild, not a Go refactor.

What it deliberately is **not**:

- **Not a general GUI toolkit / not runtime-hot-reloaded.** The layout is
  embedded and compiled in; editing it means `make build`. There's no external
  layout file, no file watcher (both existed in an early prototype and were
  dropped as needless complexity).
- **Not a widget factory.** It never *creates* widgets or wires callbacks — it
  only positions widgets the app already made. Behavior is Go's job.
- **Not able to enumerate generated content.** Collections built by loops (the
  48 pad cells, the sequencer's per-track rows) are exposed to the layout as a
  single **holder** container (`padGrid`, `seqGrid`); the layout positions the
  holder, Go fills it (see §6).

## 2. The two packages

Both are generic `internal/ui` code — **no `p6`, no MIDI, no device knowledge**
(same rule as `internal/ui/components`).

- **`internal/ui/layoutspec`** — the *builder IR*. A tree of `Node`s
  (`Ref`/`RefWith`, `VBox`, `HBox`, `Stack`, `Border`, `Split`, `Grid`,
  `RackPanel`, `Spacer`, `Separator`) plus `Build`/`BuildConfig`, which turn the
  tree into `fyne.CanvasObject`s by resolving `Ref`s against a
  `Registry` (`map[string]fyne.CanvasObject`). It's the only package here that
  imports Fyne widgets (`container`, `widget`, `components`).
- **`internal/ui/layoutlang`** — the *text language*. `Parse` lexes + parses a
  document into variants and rack blocks; `Document.Select(env)` picks a variant
  and compiles it to a `layoutspec.Node`; `Document.Rack(name, env)` compiles a
  rack-internal block. Imports only `layoutspec` + stdlib.

Layering: `layoutlang` → `layoutspec` → `components`. The application
(`cmd/rp6`) drives both and supplies the registry + the device-specific bits.

## 3. The language

A document is a sequence of **`layout`** variants and **`rack`** blocks. C-style
`//` and `/* */` comments; `;` entry terminators are optional.

```
layout <name> [when <condition>] { <node> }   // a whole-window arrangement
rack   <name>                  { <node> }     // a rack's internal arrangement
```

**Nodes** are one of:

- a **widget reference** — a bare id (`pads`), optionally with properties
  (`vu(orientation: horizontal)`); resolves against the Registry, `nil` if absent;
- a **container** — `VBox`/`HBox`/`Stack` (positional children), `Border`
  (`top`/`bottom`/`left`/`right`/`center` regions), `Split` (`leading`/`trailing`
  + `horizontal:`/`offset:` props), `Grid` (`cols:` + children), `RackPanel`
  (gunmetal frame around one child);
- the keywords **`Spacer`** (expanding gap) and **`Separator`** (divider).

Any node entry may carry an **`if <condition>`** suffix; a false condition drops
it (a dropped `Split` side collapses to the other; an empty box resolves to nil).

**Conditions** combine boolean flags and numeric comparisons with `!`, `&&`,
`||`, parentheses: e.g. `when width < 500`, `seq if !seq_docked`,
`fx if fx_visible`. Values come from the `Env` the app supplies (§4).

Example (abridged):

```
layout console when fullscreen {
  Border {
    left:   VBox { transport; vu(orientation: horizontal); Spacer }
    center: Split { horizontal: true; offset: 0.65
                    leading: pads if pads_visible; trailing: seq if seq_docked }
    right:  VBox { fx(show: true); dlyrev(show: true); seq if !seq_docked }
    bottom: status
  }
}
```

## 4. Variant selection & the environment

`Document.Select(env)` walks variants in order and takes the **first** whose
`when` matches (a bare `layout` with no `when` is the default / last). The
environment is built in `cmd/rp6/layout.go` `selectLayout` each relayout:

| flag / var | meaning |
|---|---|
| `mobile` / `web` / `desktop` | platform, from compile-time build tags |
| `fullscreen` | the user toggled full screen (see §7) |
| `compact` | narrow/portrait window (aspect ratio, hysteresis — `classifyCompact`) |
| `seq_docked` | sequencer docked as a side column |
| `pads_visible` | pad grid shown and not floating |
| `width` / `height` | live canvas size (numeric) |

The shipped variants: **`consoletablet`** (`when fullscreen && mobile`),
**`console`** (`when fullscreen`), **`compact`** (`when compact`), **`default`**
(windowed, unguarded). Order matters — the tablet console is listed first so its
more-specific guard wins on a mobile device (see §9). See §9 for the files.

## 5. Widget IDs

Top-level racks: `transport` · `dlyrev` · `fx` · `seq` · `keys` · `pads` · `vu` ·
`status`. Rack-internal sub-widgets (see §6): `play`/`tempo`/`pattern` (transport);
`delayTime`/`delayLevel`/`reverbTime`/`reverbLevel` (dlyrev); `fxRoll`/`fxRate`
(fx); `keyboardOct`/`keyboardKeys` (keys); `padFloat`/`padListen`/`padDensity`/`badge`/`padGrid`
(pads); `seqHeader`/`seqControls`/`seqGrid` (seq).

## 6. Rack internals (`rack` blocks) + the holder pattern

A `rack NAME { … }` block lays out the controls *inside* a rack from the same
primitives. The app registers that rack's sub-widgets and composes them via the
block, falling back to a Go-built default when there's no block. The
dynamically-generated parts are exposed as a **single holder container** the
block positions:

- `padGrid` = the pad grid holder (`container.Stack`; Go swaps its child on a
  density change),
- `seqGrid` = the sequencer's `VScroll` of track rows.

The layout positions the holder; Go fills/regenerates its children. This is the
boundary: **static composition → layout file; generation/behavior → Go.**

### Compose *once* — the object→canvas 1:1 rule

Fyne keys the object→canvas association **1:1** (`SetCanvasForObject` /
`LoadOrStore`). If a widget is parented into *two* trees — even a throwaway one —
refresh routing and per-window textures break. On desktop this is often
tolerated; **the mobile (Android) driver renders the second tree as missing
widgets.** So `ui.composeRack(name, reg, fallback)` builds *either* the DSL block
*or* the Go fallback, **never both** (`cmd/rp6/layout.go`). The rack constructors
(`newEffectsRack`, `newSequencerRack`) expose a `defaultObject()` instead of
building `obj` themselves, so the sub-widgets land in exactly one tree.

(This is the same hazard AGENTS.md flags for float/dock: don't reuse an object
tree across canvases — rebuild it.)

## 7. Component properties (`name(key: value)`)

A `Ref` can carry string properties, handed to a `Configurator` at build time
(`layoutspec.RefWith` + `BuildConfig`). layoutspec stays generic — it only
carries the strings; the app's `configureComponent` (`cmd/rp6/layout.go`) knows
what they mean:

- **`vu(orientation: horizontal|vertical)`** — the VU meter's orientation
  (`applyMeterOrientation`). Applied on every relayout (idempotent). This
  replaced the old Go logic that derived orientation from `compact`.
- **`fx(show: true|false)` / `dlyrev(show: true|false)` / `keys(show: true)`** —
  a rack's default visibility, applied **only when the variant is entered**
  (`variantChanged`), so a variant sets its default visible racks without
  fighting the user's toggles while that variant stays on screen (`applyRackShow`).
  Each override remembers the rack's prior visibility (`ui.forced`, keyed by id)
  and is **undone on the next variant switch** (`restoreForcedRacks`), so leaving
  a variant restores the racks to how the user had them — e.g. leaving the
  console doesn't leave its force-shown racks stuck on in the normal layout. This
  is generic: any variant + any rack using `show:` is handled, no hardcoded list.

## 8. The full-screen / console gotcha

`console` is chosen `when fullscreen`. **Do not** read `Window.FullScreen()` for
this: Android apps report `FullScreen() == true` inherently, which would force
the console layout on unbidden. Instead the app tracks its **own** `fullScreen`
intent bool, flipped by `toggleFullScreen` (F11 / Ctrl+Shift+Enter on desktop)
and by the bottom-bar **CONSOLE** toggle (`toggleConsole`) on any platform. On
desktop CONSOLE also drives the OS window full screen; on mobile there's no
window to toggle (it's already full screen), so it just flips the layout intent.

Because the desktop console's three-rail split squeezes its centre into an
unreadable sliver on a ~4:3 tablet, mobile gets its **own** console variant
(`consoletablet`, `when fullscreen && mobile`) with two wide panes (pads | seq)
instead — see §9. A phone that turns CONSOLE on still gets this tablet variant;
it suits large screens best, so it's an opt-in (off by default on mobile).

`SetFullScreen` is applied asynchronously by Fyne, so `toggleFullScreen` also
restores the saved windowed size on exit and lets `onCanvasResize` re-lay out
once the size settles (some drivers don't shrink the canvas back on their own).

## 9. Files & app wiring

- **`cmd/rp6/assets/console-tablet.layout`** — the `consoletablet` variant (the
  console layout for large touch screens; `when fullscreen && mobile`).
- **`cmd/rp6/assets/console.layout`** — the `console` (desktop full-screen) variant.
- **`cmd/rp6/assets/default.layout`** — the `compact` + `default` variants **and
  the shared `rack` blocks**.

`layoutSource()` concatenates them **tablet-console-first, then desktop console,
then default**, so the more specific `when fullscreen && mobile` guard is matched
before the plain `when fullscreen` (a document is just a sequence of blocks, so
concatenation is valid). `loadLayout()` parses it in `build()` — pure, no I/O,
safe in tests.

Flow: `build()` → `loadLayout()`; each rack composed once via `composeRack`;
`relayout()` builds the registry, calls `selectLayout` (→ `Document.Select` +
`layoutspec.BuildConfig` with `configureComponent`), and swaps the result into
the stable `contentHolder` (whose `sizeWatch` reports resizes to
`onCanvasResize`). A minimal Go fallback keeps the window non-blank if the
document ever fails to parse (a test guards that it always parses).

To change the UI: edit a `.layout` file and `make build`. Add a device layout by
adding a `layout <name> when <cond> { … }` block (order matters — specific
first).

## 10. Testing

- `layoutspec` / `layoutlang` are pure and headless: build trees with
  `canvas.Rectangle` stand-ins, assert structure, variant selection, condition
  eval, `RefWith`/`Configurator`, rack blocks.
- `cmd/rp6/layout_test.go` guards the embedded document (it always parses; every
  recomposed rack has a block that builds), drives the real widgets through
  variant/state transitions (`TestConsoleLayoutSmoke`,
  `TestConsoleRevealsFxDlyRev`, `TestVUOrientationPerVariant`,
  `TestFullScreenSelectsConsole`).
- The layout system was also validated on a physical Android device (the
  full-screen gotcha in §8 was found that way — instrument `selectLayout` and
  read `adb logcat`).

## 11. Ideas / not built

- More component properties (grid density, knob ranges) via `configureComponent`.
- A phone-tailored `layout mobile when mobile { … }` (today phones use `compact`).
- A `rp6 -check-layout <file>` CLI to validate a layout without a display.
- If runtime editing is ever wanted again, layer a file loader over
  `layoutlang.Parse` — the parser is already file-agnostic.
