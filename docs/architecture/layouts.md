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

RP6 uses a **fixed-form-factor policy**: rather than continuously adapting to any
window size, each layout is designed for a discrete form factor and a size maps
to exactly one variant. The supported set is `resolutions.txt`; the windowed
desktop is a single **fixed, non-resizable** size, mobile is phone-or-tablet by
device size, and desktop full screen is the console. There is deliberately **no
continuous "compact" reflow and no size-driven rack hiding** — those were the
main sources of layout bugs (see the git history around this change).

`Document.Select(env)` walks variants in order and takes the **first** whose
`when` matches (a bare `layout` with no `when` is the default / last). The
environment is built in `cmd/rp6/layout.go` `layoutEnv` each relayout:

| flag / var | meaning |
|---|---|
| `mobile` / `web` / `desktop` | platform, from compile-time build tags (overridable per-scenario in the inspection tests via `mobileForTest`/`tabletForTest`) |
| `fullscreen` | the user's console intent (desktop only — see §7/§8) |
| `tablet` | tablet-class touchscreen (`isTabletSize`: smallest side ≥ 600) |
| `seq_docked` | sequencer docked as a side column |
| `pads_visible` | pad grid shown and not floating |
| `width` / `height` | live canvas size (numeric) |

The shipped variants and their guards (order matters — the loader concatenates
tablet + console first, so more-specific guards win):

| variant | guard | target(s) in `resolutions.txt` |
|---|---|---|
| `tablet` | `mobile && tablet` | OnePlus Pad 3 3392×2400 |
| `console` | `fullscreen && desktop` | ThinkPad 1920×1200, Asus ROG 3440×1440 |
| `phone` | `mobile` | Pixel 10 Pro / Pro XL |
| `window` | *(default)* | ThinkPad 850×950 windowed |

A size not in `resolutions.txt` resolves to the nearest of these by the same
predicates (best-effort): an unlisted desktop full-screen size still gets
`console` (its proportional splits absorb the aspect difference), an unlisted
Android device gets `phone` or `tablet` by size, and any desktop window is the
fixed `window` size. See §9 for the files.

## 5. Widget IDs

Top-level racks: `transport` · `p6` · `fx` · `keysfx` · `seq` · `keys` · `paks` · `pads` · `vu` ·
`status`. Rack-internal sub-widgets (see §6): `play`/`tempo`/`pattern` (transport);
`delayTime`/`delayLevel`/`reverbTime`/`reverbLevel` (p6); `fxRoll`/`fxRate`
(fx); `keysFXTone`/`keysFXComp`/`keysFXChorus`/`keysFXDelay`/`keysFXReverb`
(keysfx); `keyboardOct`/`keyboardKeys` (keys); `padFloat`/`padListen`/`padDensity`/`badge`/`padGrid`
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
- **`seq(tracks: N)`** — the sequencer's default track count for the variant
  (`applyDefaultTracks` → `sequencerRack.SetTrackCount`). Variant-entry only, like
  `show:`. The window variant uses `tracks: 4`; console/tablet use `6`.
- **`pads(layout: paged|twobank|dense)`** — the pad grid's default paging for the
  variant (`applyDefaultPadLayout` → `applyPadLayout` **without persisting**, so it
  doesn't clobber the user's density-button preference). Variant-entry only. The
  window variant uses `twobank` (12 pads, four A–B…G–H tabs); console/tablet `paged`.
- **`pads/seq(expand: horizontal|vertical|both)`** — fill that axis of the
  allocation with the **rack frame** while its children stay their natural size,
  centered (`applyRackExpand` → `ContentFit.SetExpand`; doesn't change MinSize, so
  it never forces the window larger). Applied every relayout (idempotent), so a
  variant without it resets the rack to content-sized. The window variant sets
  `pads(expand: horizontal)` so the pad rack fills the fixed window width.

The window's fixed size + full-screen console interaction is covered in §8; the
per-variant default set (which properties each variant declares) is in §4.

## 8. The full-screen / console gotcha

`console` is chosen `when fullscreen && desktop`. **Do not** read
`Window.FullScreen()` for this: Android apps report `FullScreen() == true`
inherently, which would force the console layout on unbidden. Instead the app
tracks its **own** `fullScreen` intent bool, flipped by `toggleFullScreen` (F11 /
Ctrl+Shift+Enter on desktop) and by the bottom-bar **CONSOLE** toggle
(`toggleConsole`). CONSOLE is **desktop-only** — on mobile the phone/tablet
variant is chosen by device size, so the toggle is omitted from the bar there.

On desktop `setConsole` drives the OS window: **console = full screen**, while
**windowed = a single fixed, non-resizable size** (`windowedWidth`×
`windowedHeight`, `SetFixedSize(true)`). Entering console clears the fixed-size
lock before `SetFullScreen(true)`; leaving restores the fixed windowed size. The
tablet gets its own `tablet` variant (a paks rail beside a seq-over-pads column),
distinct from the desktop console's three-rail split.

`SetFullScreen` is applied asynchronously by Fyne, so the console layout keys off
the fullscreen *intent* (not pixel size) and its proportional splits reflow as
the window settles — the synchronous relayout in `setConsole` is authoritative,
and `onCanvasResize` only rebuilds if the discrete variant actually changes (see
§9 / `variantFor`).

## 9. Files & app wiring

- **`cmd/rp6/assets/console-tablet.layout`** — the `tablet` variant (paks rail +
  seq-over-pads column for large touch screens; `when mobile && tablet`).
- **`cmd/rp6/assets/console.layout`** — the `console` (desktop full-screen)
  variant (`when fullscreen && desktop`).
- **`cmd/rp6/assets/default.layout`** — the `phone` (`when mobile`) + `window`
  (default, fixed desktop) variants **and the shared `rack` blocks**.

`layoutSource()` concatenates them **tablet-first, then console, then the
default file**, so the more specific `when mobile && tablet` guard is matched
before `when mobile` (a document is just a sequence of blocks, so concatenation
is valid). `loadLayout()` parses it in `build()` — pure, no I/O, safe in tests.

Flow: `build()` → `loadLayout()`; each rack composed once via `composeRack`;
`relayout()` builds the registry, calls `selectLayout` (→ `Document.Select` +
`layoutspec.BuildConfig` with `configureComponent`), and swaps the result into
the stable `contentHolder` (whose `sizeWatch` reports resizes to
`onCanvasResize`). `onCanvasResize` compares `variantFor(size)` to the active
variant and requests a relayout **only when the discrete form factor changes** (a
tablet learning its first size, or the desktop console settling after an async
`SetFullScreen`) — never continuously while a window is dragged (the windowed
size is fixed, and the console's splits reflow on their own). A minimal Go
fallback keeps the window non-blank if the document ever fails to parse (a test
guards that it always parses).

To change the UI: edit a `.layout` file and `make build`. Add a device form
factor by adding a `layout <name> when <cond> { … }` block (order matters —
specific first) and a matching scenario in `inspection_test.go`.

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

### Inspection artifacts

`make inspect-layouts` runs the current UI through every device in
`resolutions.txt` and regenerates three checked-in artifacts per scenario under
`cmd/rp6/testdata/layout-inspection/`:

- `<scenario>.png` — the clean software-rendered canvas,
- `<scenario>-annotated.png` — the same scene with stable semantic rack IDs and
  colored bounds, and
- `<scenario>.json` — logical/pixel canvas sizes, scale, selected variant,
  backend/form-factor state, and every registered element's rect, visible rect,
  minimum size, effective layout visibility, clipping and component state.

The semantic registry is `cmd/rp6/inspection.go`; the generic capture and
contract implementation is `internal/ui/inspect`. Tests assert required/omitted
racks, containment, minimum size, non-overlap and touch-target size before a
visual review of the clean and annotated images. IDs describe RP6 concepts
(`rack.pads`, `sequencer.track.1.step.1`) rather than renderer primitives, so
they remain stable across drawing refactors and will also identify future pages.

Phone native resolutions are converted to Fyne logical units with the Android
driver's DPI bucket: both Pixel targets in `resolutions.txt` fall in Fyne's 3x
bucket. The JSON retains both coordinate spaces and the PNG is native-pixel
sized. Desktop captures use scale 1 because `resolutions.txt` does not specify
desktop DPI/scaling; add explicit scale scenarios if those displays run at a
different OS scale.

Fyne 2.8 exposes `fyne.Accessible` (label + button/container/link/text role) but
no stable automation ID or public accessibility-tree traversal. RP6 custom
interactive controls implement those labels/roles where useful, while the RP6
inspection manifest remains the authoritative automation interface. Fyne's
platform accessibility bridge is experimental, requires the `accessibility`
build tag, and is not implemented on Linux. Its current platform collectors
also recurse through `fyne.Container` children but not custom-widget renderer
children, so controls nested inside RP6's custom `RackPanel` do not yet form a
complete native screen-reader tree. The labels are groundwork, not a claim of
end-to-end assistive-technology support.

The headless software renderer is deterministic and exercises real Fyne layout,
but it does not validate a compositor, GPU, OS window chrome, device safe-area
insets or multi-window canvas ownership. Layout visibility means shown by the
object and its ancestors with non-empty bounds through known clip containers; it
does not attempt arbitrary opaque-sibling occlusion. Keep real Wayland and
physical Android smoke checks for those driver-specific behaviors.

Current validated targets (the `resolutions.txt` set — one scenario each in
`inspection_test.go`, plus a Wayland scale-transition regression guard):

| target | variant | active racks |
|---|---|---|
| ThinkPad X13 850×950 window | `window` | transport, pads, VU, navigation, status |
| ThinkPad X13 1920×1200 full screen | `console` | transport, VU, paks, pad FX, docked sequencer, pads, keyboard, navigation, status |
| Asus ROG 3440×1440 full screen | `console` | transport, VU, paks, pad FX, docked sequencer, pads, keyboard, navigation, status |
| Pixel 10 Pro XL 1344×2992 | `phone` | transport, pads, VU, navigation, status |
| Pixel 10 Pro 1280×2856 | `phone` | transport, pads, VU, navigation, status |
| OnePlus Pad 3 3392×2400 | `tablet` | transport, VU, paks, sequencer, pads, keyboard, navigation, status |

The phone scenarios intentionally leave the sequencer, keyboard, effects and
sample-pak browser off; they do not fit while preserving the 32-unit touch
contract. The desktop console puts pad FX at the bottom of the left rail instead
of reserving a mostly-empty full-height right rail, and its active sequencer
track/bar rows expand to consume available height. The rack set for each variant
is **fixed** — a size that is too small for it simply isn't a supported target;
RP6 no longer hides racks to fit (that size-driven fallback was removed with the
fixed-form-factor policy).

### Physical cell sizing

Pads and sequencer steps use `components.PhysicalGrid`: a fixed-column custom
layout that measures the effective canvas pixel scale through
`Canvas.PixelCoordinateForPosition`, keeps cells square, and converts their
physical-pixel bounds into logical Fyne sizes. Normal pads are 80-130 physical
pixels square; sequencer steps are 40-50. The all-bank dense pad mode is the
explicit exception, using 65-75px squares so eight bank rows remain visible
without adding a pad scrollbar.

`components.ContentFit` wraps the complete pad/sequencer panel and sizes that
panel to the grid plus its controls and rack frame. Extra viewport space remains
outside the panel, horizontally balanced, rather than stretching cells or
appearing as internal rack space. Sequencer tracks and visible bars are packed at
their square-row height; when they exceed the fitted panel height, the existing
vertical track scroller exposes the overflow.

Resolution contracts assert the physical square ranges directly from each
manifest's `pixelRect`, allowing one pixel for raster edge rounding. The
If a pane cannot provide the 80px preferred pad floor, cells still shrink to the
allocation rather than painting outside the rack. The inspection contracts catch
that degraded size separately. On Wayland, the app polls the effective framebuffer
scale and forces a real relayout after a content-scale change because Fyne's
scale callback repaints without guaranteeing layout.

Inspection also declares semantic containment contracts: every visible pad must
stay inside `pads.grid`, pad controls/grid inside `rack.pads`, and sequencer
controls/visible steps inside `rack.sequencer` / `sequencer.grid`. This catches
painting outside a rack even when the rack rectangles themselves do not overlap.
`regression-scale-transition-3072x1920` reproduces a Wayland late content-scale
change (1.25x geometry to 2x rendering) and asserts the corrective relayout runs;
it is a rendering regression guard, not a supported resolution.

## 11. Ideas / not built

- More component properties (grid density, knob ranges) via `configureComponent`.
- Per-target `.layout` files if two clustered targets ever need genuinely
  different structure (today the two desktop full-screen sizes share `console`
  and the two phones share `phone`).
- A `rp6 -check-layout <file>` CLI to validate a layout without a display.
- If runtime editing is ever wanted again, layer a file loader over
  `layoutlang.Parse` — the parser is already file-agnostic.
