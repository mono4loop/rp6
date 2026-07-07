# rp6 — Adafruit MacroPad RP2040 firmware
#
# Turns the MacroPad into a USB-MIDI pad controller for rp6. It sends *absolute*
# P-6-style pad notes (48..95, note = 48 + bank*6 + (pad-1)) so rp6 can trigger
# the matching pad on the real P-6 *or* the emulator without any extra mapping.
#
#   - 12 keys  = two P-6 banks at once (top two rows = bank N, bottom two = N+1),
#                6 pads per bank, laid out to mirror rp6's 6-wide grid.
#   - Encoder  = page through the bank pairs: A/B, C/D, E/F, G/H (wraps).
#   - Encoder press = transport toggle (MIDI Start / Stop) -> rp6 Play/Stop.
#   - NeoPixels are tinted with rp6's per-bank colors; a key flashes white when hit.
#   - The OLED shows a synth-panel UI: inverted title bar + transport glyph, an
#     A..H bank selector strip, and a center 12-bar equalizer that spikes on each
#     hit (one bar per key) with a readout of the pad + MIDI note being sent.
#
# Deps (install with docs/hardware/macropad/setup.sh, or `circup install`):
#   adafruit_macropad, adafruit_midi
#   (displayio / vectorio / terminalio are built into CircuitPython)
#
# See docs/hardware/macropad/README.md for wiring/flashing and rp6 details.

import time

import displayio
import terminalio
import vectorio
from adafruit_display_text import label

import usb_midi
import adafruit_midi
from adafruit_midi.note_on import NoteOn
from adafruit_midi.start import Start
from adafruit_midi.stop import Stop

from adafruit_macropad import MacroPad

# --- P-6 layout -------------------------------------------------------------

NUM_BANKS = 8          # A..H
PADS_PER_BANK = 6      # 1..6
FIRST_PAD_NOTE = 48    # C3 -> A1
VELOCITY = 100         # mechanical keys have no velocity; match p6.DefaultVelocity
SAMPLER_CHANNEL = 11   # P-6 factory Sampler channel (1-based); rp6 only needs a
                       # valid pad note, so the exact channel is not critical.

# rp6's bank colors (cmd/rp6/padgrid.go), one RGB per bank A..H.
BANK_COLORS = (
    (0xE1, 0x4B, 0x4B),  # A  red
    (0xE1, 0x87, 0x3B),  # B  orange
    (0xC9, 0xA2, 0x27),  # C  gold
    (0x4C, 0xAF, 0x50),  # D  green
    (0x26, 0xA6, 0x9A),  # E  teal
    (0x44, 0x77, 0xDD),  # F  blue
    (0x7E, 0x57, 0xC2),  # G  purple
    (0xD2, 0x4B, 0x9C),  # H  magenta
)
IDLE_DIM = 0.18          # scale bank color when a key is idle
FLASH = (0xFF, 0xFF, 0xFF)
FLASH_SECS = 0.08

# Center "hit" visualizer: a 12-bar equalizer (one bar per key) that spikes on a
# press and decays, with a readout plate showing the pad + MIDI note being sent.
BAND_TOP = 17            # top of the center band (just below the title bar)
BASE_Y = 40              # baseline the bars grow up from (leaves a gap to the strip)
MAX_BAR_H = BASE_Y - BAND_TOP
BAR_SLOT = (128 - 6) // 12  # horizontal pitch per bar across the inner width
BAR_W = BAR_SLOT - 4
BAR_DECAY = 0.80         # height multiplier applied each animation tick
ANIM_SECS = 0.03         # min seconds between animation ticks (~33 fps)
NOTE_HOLD_SECS = 0.7     # how long the note readout stays before reverting

# The MacroPad is 3 columns x 4 rows (keys 0..11, row-major). We fill it two
# banks at a time: keys 0..5 = the "base" bank, keys 6..11 = base+1. Within a
# bank, keys map to pads 1..6 in reading order.
def key_bank_pad(key, base_bank):
    if key < PADS_PER_BANK:
        return base_bank % NUM_BANKS, key + 1
    return (base_bank + 1) % NUM_BANKS, (key - PADS_PER_BANK) + 1


def note_for(bank, pad):
    return FIRST_PAD_NOTE + bank * PADS_PER_BANK + (pad - 1)


def scale(color, factor):
    return (int(color[0] * factor), int(color[1] * factor), int(color[2] * factor))


def bank_label(b):
    return chr(ord("A") + (b % NUM_BANKS))


# --- Setup ------------------------------------------------------------------

macropad = MacroPad()
macropad.pixels.brightness = 0.6

midi = adafruit_midi.MIDI(
    midi_out=usb_midi.ports[1], out_channel=SAMPLER_CHANNEL - 1
)

base_bank = 0          # even index: shows banks base_bank / base_bank+1
last_position = macropad.encoder
playing = False
flash_until = [0.0] * 12

bar_h = [0.0] * 12     # current equalizer bar heights (float, for smooth decay)
center_mode = "bank"   # "bank" (idle) or "note" (recent hit)
note_hold_until = 0.0  # when to revert the readout from "note" back to "bank"
last_anim = 0.0        # last equalizer animation tick

# --- Synth-panel display ----------------------------------------------------
#
# The MacroPad OLED is 128x64, 1-bit. We draw with displayio/vectorio (built in):
# a white "on" palette for lit shapes and a black "ink" palette for punching
# glyphs out of the lit title bar.

WIDTH, HEIGHT = 128, 64
ON = 0xFFFFFF
OFF = 0x000000

lit = displayio.Palette(1)
lit[0] = ON
ink = displayio.Palette(1)
ink[0] = OFF

root = displayio.Group()

# Panel border (four 1px edges) for a rack-panel feel.
root.append(vectorio.Rectangle(pixel_shader=lit, width=WIDTH, height=1, x=0, y=0))
root.append(vectorio.Rectangle(pixel_shader=lit, width=WIDTH, height=1, x=0, y=HEIGHT - 1))
root.append(vectorio.Rectangle(pixel_shader=lit, width=1, height=HEIGHT, x=0, y=0))
root.append(vectorio.Rectangle(pixel_shader=lit, width=1, height=HEIGHT, x=WIDTH - 1, y=0))

# Title bar: a filled bar with black text + a transport glyph punched into it.
root.append(vectorio.Rectangle(pixel_shader=lit, width=WIDTH - 2, height=13, x=1, y=1))
title = label.Label(terminalio.FONT, text="rp6  P-6", color=OFF)
title.anchor_point = (0.0, 0.5)
title.anchored_position = (6, 7)
root.append(title)

# Transport glyphs live at the right of the title bar (black on the lit bar);
# one is shown at a time.
play_glyph = vectorio.Polygon(
    pixel_shader=ink, points=[(0, 0), (0, 9), (8, 4)], x=WIDTH - 15, y=3
)
stop_glyph = vectorio.Rectangle(pixel_shader=ink, width=8, height=8, x=WIDTH - 15, y=3)
root.append(play_glyph)
root.append(stop_glyph)

# Center "hit" band: a 12-bar equalizer (behind) plus a readout plate (on top).
# Bars are drawn first so the readout text overlays them.
bars = []
for i in range(12):
    bx = 3 + i * BAR_SLOT + (BAR_SLOT - BAR_W) // 2
    b = vectorio.Rectangle(pixel_shader=lit, width=BAR_W, height=1, x=bx, y=BASE_Y - 1)
    b.hidden = True
    bars.append(b)
    root.append(b)

# Readout: shows the bank pair when idle, or the pad + note on a press. Its
# black background plate keeps it legible over the bars.
readout = label.Label(
    terminalio.FONT, text="A / B", color=ON, background_color=OFF, scale=2, padding_left=2, padding_right=2
)
readout.anchor_point = (0.5, 0.5)
readout.anchored_position = (WIDTH // 2, (BAND_TOP + BASE_Y) // 2)
root.append(readout)

# Bank selector strip A..H along the bottom, each with an underline that lights
# up for the two active banks.
strip_y = 50
underline_y = 58
underlines = []
for i in range(NUM_BANKS):
    cx = 10 + i * 15
    letter = label.Label(terminalio.FONT, text=bank_label(i), color=ON)
    letter.anchor_point = (0.5, 0.5)
    letter.anchored_position = (cx, strip_y)
    root.append(letter)
    bar = vectorio.Rectangle(pixel_shader=lit, width=9, height=2, x=cx - 4, y=underline_y)
    bar.hidden = True
    underlines.append(bar)
    root.append(bar)

# Attach the group (CircuitPython 9 uses root_group; older uses show()).
try:
    macropad.display.root_group = root
except AttributeError:
    macropad.display.show(root)


def refresh_pixels():
    for key in range(12):
        bank, _ = key_bank_pad(key, base_bank)
        macropad.pixels[key] = scale(BANK_COLORS[bank], IDLE_DIM)


def show_bank():
    lo = base_bank % NUM_BANKS
    hi = (base_bank + 1) % NUM_BANKS
    readout.text = "%s / %s" % (bank_label(lo), bank_label(hi))


def refresh_banks():
    lo = base_bank % NUM_BANKS
    hi = (base_bank + 1) % NUM_BANKS
    for i in range(NUM_BANKS):
        underlines[i].hidden = i not in (lo, hi)
    if center_mode == "bank":
        show_bank()


def hit(key, bank, pad):
    # Spike this key's bar and show the pad + note it just sent.
    global center_mode, note_hold_until
    bar_h[key] = MAX_BAR_H
    readout.text = "%s%d %d" % (bank_label(bank), pad, note_for(bank, pad))
    center_mode = "note"
    note_hold_until = time.monotonic() + NOTE_HOLD_SECS


def animate(now):
    # Decay the bars and, once the note readout has been held long enough,
    # revert to the bank pair. Throttled to ANIM_SECS so it doesn't hog the loop.
    global last_anim, center_mode
    if now - last_anim < ANIM_SECS:
        return
    last_anim = now
    for i in range(12):
        bar_h[i] *= BAR_DECAY
        h = int(bar_h[i])
        if h < 1:
            bar_h[i] = 0.0
            bars[i].hidden = True
        else:
            bars[i].height = h
            bars[i].y = BASE_Y - h
            bars[i].hidden = False
    if center_mode == "note" and now >= note_hold_until:
        center_mode = "bank"
        show_bank()


def refresh_transport():
    play_glyph.hidden = not playing
    stop_glyph.hidden = playing


def page(delta):
    global base_bank
    # Step by two banks so pages don't overlap: A/B, C/D, E/F, G/H.
    base_bank = (base_bank + delta * 2) % NUM_BANKS
    refresh_pixels()
    refresh_banks()


refresh_pixels()
refresh_banks()
show_bank()
refresh_transport()

# --- Main loop --------------------------------------------------------------

while True:
    now = time.monotonic()

    # Keys -> pad notes. Drain all pending events so simultaneous presses each
    # fire and light their own bar.
    while True:
        event = macropad.keys.events.get()
        if not event:
            break
        if event.pressed:
            bank, pad = key_bank_pad(event.key_number, base_bank)
            midi.send(NoteOn(note_for(bank, pad), VELOCITY))
            macropad.pixels[event.key_number] = FLASH
            flash_until[event.key_number] = now + FLASH_SECS
            hit(event.key_number, bank, pad)

    # Encoder rotation -> page banks.
    position = macropad.encoder
    if position != last_position:
        page(1 if position > last_position else -1)
        last_position = position

    # Encoder press -> transport toggle (MIDI Start/Stop).
    macropad.encoder_switch_debounced.update()
    if macropad.encoder_switch_debounced.pressed:
        playing = not playing
        midi.send(Start() if playing else Stop())
        refresh_transport()

    # Animate the equalizer / revert the readout.
    animate(now)

    # Restore idle color after a key's flash expires.
    for key in range(12):
        if flash_until[key] and now >= flash_until[key]:
            bank, _ = key_bank_pad(key, base_bank)
            macropad.pixels[key] = scale(BANK_COLORS[bank], IDLE_DIM)
            flash_until[key] = 0.0
