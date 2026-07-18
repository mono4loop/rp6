# Adafruit MacroPad RP2040 as an rp6 pad controller

Use an **Adafruit MacroPad RP2040** (product [5100](https://www.adafruit.com/product/5100),
the "DigiKey macropad") as a physical MIDI pad controller for rp6. The MacroPad
plays pads on the real **P‑6** *or* the **emulator** (`rp6 -emu …`) — rp6 routes
its hits through the same fire path as an on‑screen tap.

```
MacroPad  ──USB MIDI──▶  rp6  ──▶  P‑6 (USB MIDI)  or  emulator (audio)
```

The MacroPad is a **separate** USB device from the P‑6, so it never conflicts
with the P‑6's exclusive MIDI node — you can use both at once.

---

## What you need

- Adafruit MacroPad RP2040 with 12 key switches + keycaps installed.
- A USB‑C cable.
- [CircuitPython](https://circuitpython.org/board/adafruit_macropad_rp2040/)
  on the MacroPad (see *Flashing CircuitPython* below if the `CIRCUITPY` drive
  isn't there yet).
- [`circup`](https://github.com/adafruit/circup) to install libraries:
  `pip install --user circup`.

## One‑shot setup

With the MacroPad plugged in and showing its `CIRCUITPY` USB drive:

```bash
docs/hardware/macropad/setup.sh          # auto-detects CIRCUITPY
# or point it at the mount explicitly:
docs/hardware/macropad/setup.sh /run/media/$USER/CIRCUITPY
```

The script installs the required libraries (`adafruit_macropad`,
`adafruit_midi`) with `circup` and copies the rp6 firmware
([`code.py`](code.py)) onto the pad. The MacroPad reloads automatically.

Then just start rp6 — it auto‑detects the MacroPad on launch:

```bash
make run                 # against the P-6 hardware
make run TAGS= -- ...    # (or) build without audio
go run ./cmd/rp6 -emu ./samples   # against the emulator
```

rp6 logs `MIDI input controller: Adafruit MacroPad RP2040 (…)` and shows it in
the status bar when found. No controller present is not an error — rp6 runs
normally.

---

## Controls

| Control                | Action                                                        |
| ---------------------- | ------------------------------------------------------------- |
| **12 keys**            | Trigger pads. Top two rows = one bank, bottom two = the next. |
| **Rotary encoder**     | Page through bank pairs: **A/B → C/D → E/F → G/H** (wraps).    |
| **Encoder press**      | Transport toggle → rp6 **Play/Stop** (MIDI Start/Stop).       |
| **NeoPixels**          | Tinted with rp6's per‑bank colors; flash white on a hit.      |
| **OLED**               | Synth panel: inverted title bar + Play/Stop glyph, a big centered bank‑pair readout, and an A–H selector strip with the active pair underlined. |

Key → pad layout (a page shows two banks, 6 pads each, in reading order):

```
 key0 key1 key2       bank N  pads 1 2 3
 key3 key4 key5       bank N  pads 4 5 6
 key6 key7 key8       bank N+1 pads 1 2 3
 key9 key10 key11     bank N+1 pads 4 5 6
```

The firmware sends **absolute P‑6 pad notes** (48–95, `note = 48 + bank*6 +
(pad−1)`), the same numbers the P‑6 itself uses, so a key always addresses the
same pad regardless of what bank the P‑6 hardware currently has selected.

---

## How it works (for hackers)

- **Firmware** ([`code.py`](code.py)): a CircuitPython app that reads keys/
  encoder and emits USB MIDI. All device‑specific behavior (paging, LED colors,
  OLED) lives here.
- **rp6 map** (`cmd/rp6/assets/midimaps/adafruit-macropad.midimap`): a
  data-driven `.midimap` file served by the generic interpreter
  (`internal/midiin/mapped`) — Note On (48–95) → `pad.trigger` (offset 48),
  realtime Start/Stop → transport. It reuses `p6.ParseMIDI`, the same parser the
  P‑6 input uses. (See `docs/architecture/midimaps.md`.)
- **Framework** (`internal/midiin`): a small pluggable registry. Drivers
  `Register` themselves from `init()`, `main.go` blank‑imports the ones it
  supports, and `midiin.Detect()` opens whichever controller is plugged in. The
  framework speaks only device‑agnostic `Handlers` (fire an absolute pad, toggle
  transport) and has no Fyne dependency — adding new hardware never touches the
  UI.

### Adding another controller

Create `internal/midiin/<device>/`, implement `midiin.Device`, and `Register`
a `midiin.Driver` (a `Detect` that finds its ALSA card — `midiin.FindRawMIDI`
helps — and an `Open`). Blank‑import it in `cmd/rp6/main.go`. That's it; the
UI wiring is shared.

---

## Flashing CircuitPython (if `CIRCUITPY` is missing)

1. Download the MacroPad `.uf2` from
   <https://circuitpython.org/board/adafruit_macropad_rp2040/>.
2. Enter the bootloader: hold the **rotary‑encoder button** while plugging in
   USB (or while tapping **Reset**). A `RPI-RP2` drive appears.
3. Copy the `.uf2` onto `RPI-RP2`. The board reboots as `CIRCUITPY`.
4. Run `setup.sh`.

---

## Troubleshooting

- **rp6 says "no MIDI input controller".** Confirm the pad is running the
  firmware and enumerates as MIDI: `amidi -l` should list a `MacroPad` port.
  rp6 finds it by scanning `/proc/asound/cards` for a card whose name contains
  `macropad`.
- **Keys do nothing but the pad is detected.** Make sure `code.py` copied
  successfully and the libraries are in `CIRCUITPY/lib/` (re‑run `setup.sh`).
  Open the CircuitPython serial console to see any Python tracebacks.
- **Play/Stop doesn't move the P‑6.** Transport needs the P‑6 set to MENU →
  **SYnC = USB** (same requirement as rp6's on‑screen transport).
- **No sound from the emulator.** The emulator only makes sound when built with
  `-tags capture` (e.g. `make run`); otherwise it loads and mixes silently.
