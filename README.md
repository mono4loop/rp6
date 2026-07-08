# RP6

A touch-friendly controller for the **Roland P-6** (AIRA Compact sampler). It
also bundles a software sampler and step sequencer, so it works without the
hardware too.

Runs on Linux desktop, [in the browser](https://rp6.rbel.co) (experimental), and
on Android. Built with [Fyne](https://fyne.io).

Talks to the Roland P-6 and the [Adafruit Macropad](/docs/hardware/macropad/README.md) over USB MIDI.

<p align="center">
  <img src="web/screenshot.png" alt="rp6 on the desktop" height="380">
  &nbsp;&nbsp;
  <img src="web/screenshot-pixel.png" alt="rp6 on Android" height="380">
</p>

__Note: RP6 is alpha quality. It works best on desktops and larger Android
screens (tablets); the UI doesn't adapt gracefully to small resolutions yet.__

## Features

- **48 pads, banks A–H** — a grid paged A–D / E–H (or a dense single-page
  view). Tap to trigger; the last hit is highlighted.
- **Step sequencer** — up to 8 tracks, each 1–4 bars and looping at its own
  length (polymeter), 16 steps/bar, tempo-synced, with per-track **mute**. Assign
  a pad to a track with a tap, program steps, press play. Save/load **16
  sequence slots**; your work autosaves and reloads on next launch.
  ([sequencer guide](docs/sequencer.md))
- **Transport & tempo** — illuminated Play/Stop, a TEMPO knob and a PATTERN
  selector, driving the P-6 over MIDI clock.
- **Effects** — a per-pad effects rack (tempo-synced **Roll**) and global
  **Delay / Reverb** controls.
- **Built-in emulator** — no P-6 needed: RP6 bundles a playable "modular-hits"
  sample kit. Tap the device badge to switch between the emulator and a connected
  P-6.
- **Shared jam sessions** *(experimental, desktop only)* — play together over the internet. See the
  [setup guide](docs/jams.md).
- **Activity / VU meter** — a level meter on the right (real USB-audio VU on
  desktop builds).
- **Amber hardware-style UI** — backlit rack panels, knobs and 7-segment
  readouts; works entirely by touch.

## Running it

There are pre-built Android APK and Flatpak packages in [releases](https://github.com/mono4loop/rp6/releases).

Plug in the P-6 or an [Adafruit MacroPad](/docs/hardware/macropad/README.md) over
USB and it connects automatically. Unplug it and RP6 falls back to the built-in
emulator, reconnecting when it reappears. With no hardware at all, it just runs
the emulator.

## Using it with a P-6

For full control, set these on the P-6 (MENU):

- **MIDI Clock Sync (SYnC) = USB** — so Play/Stop and the TEMPO knob drive it.
- **Rx Program Change = On** — so the PATTERN selector switches patterns.

Pads always trigger by note, so you can play all 48 pads from the grid, a
connected controller, or the sequencer regardless of the above.

## Without a P-6 (emulator)

Without a P-6 connected, RP6 uses a built-in software sampler.

The bundled kit is one-shot hits from the “Modular-Hits” pack by *publicsamples*
(https://github.com/publicsamples/Modular-Hits).

## With the Adafruit Macropad

You'll need to set up your Macropad with the CircuitPython code
[here](/docs/hardware/macropad/code.py).

## License

MIT — see [LICENSE](LICENSE). RP6 is an independent project and is not affiliated
with or endorsed by Roland.
