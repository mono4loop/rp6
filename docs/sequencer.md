# The rp6 step sequencer

rp6 includes a built-in **software step sequencer** that plays your pads for you
in a repeating pattern. Think of it as a little drum machine layered on top of
the P-6: you paint a rhythm onto a grid, press play, and rp6 fires the pads in
time.

It is a *host-side* sequencer — the timing and the pattern live in rp6 on your
computer, and it triggers the P-6's (or the emulator's) sounds. It does **not**
program the P-6's own internal patterns.

> Because both rp6's sequencer and the P-6's own sequencer would fight over the
> timing, run **one or the other**, not both at once.

---

## Opening the sequencer

The sequencer is one of rp6's toggleable "racks". Show or hide it with the
**SEQ** button in the bottom bar (or press **Ctrl+Shift+S**). When lit, the
sequencer panel is visible.

You can also **dock** the sequencer as a column to the right of the pads (see
[Docking](#docking-it-beside-the-pads) below).

---

## The layout at a glance

At the top of the panel is a row of **transport and pattern controls**:

| Control | What it does |
|---------|--------------|
| **Play/Stop** (walking-shoes key) | Starts and stops the sequencer. The shoes "walk" while it plays. |
| **TRK** knob | Number of tracks shown (1–8). |
| **SEQ** knob | Which saved sequence slot is loaded (S01–S16). |
| **Copy** (⧉) | Duplicates the current sequence into the next free slot. |
| **Clear** (🗑) | Empties all the steps. **Ctrl+click** deletes the whole sequence. |
| **SAVE** | Saves the current sequence (name + tempo). |
| **Dock** (▣) | Docks/undocks the sequencer beside the pads. |

Below that is one **track row** per active track. Each track row has, from left
to right:

1. A **mute** key (speaker icon) — tap to mute/unmute that track.
2. A **bar-length** key (a number) — tap to cycle the track's length 1→4 bars.
3. A **pad** key (e.g. `A1`) — names the pad this track plays, and is how you
   change it (see below).
4. The **step grid** — one row of 16 cells per bar. Tap a cell to turn that
   step on or off.

The whole track is tinted in the **bank color** of the pad it plays, so you can
tell tracks apart at a glance.

---

## Assigning a sample to a track

Each track plays exactly one pad. To change which pad a track plays:

1. **Tap the track's pad key** (the one showing `A1`, `B3`, etc.). It lights up
   solidly in its accent color — this is the **armed** state, meaning "waiting
   for a pad."
2. **Tap any pad** on the pad grid (or press the pad on your P-6 hardware). That
   pad becomes the track's new sample.
3. The track key returns to normal automatically.

Because arming is a deliberate two-step gesture, an accidental pad tap can't
silently change a track's sample. If you change your mind, **tap the armed track
key again** to cancel, or arm a different track to move the arm.

> Tip: this works with taps only — no keyboard, no right-click — so it's fully
> usable on a touchscreen, on the web build, and on Android.

---

## Programming a beat

1. Set how many **tracks** you want with the **TRK** knob (drag it, scroll it,
   or focus it and use the arrow keys).
2. Assign each track a pad (see above).
3. Tap **step cells** to place hits. Each cell is one 16th note; a row is one bar
   of 16 steps.
4. Press **Play**. The playhead sweeps across the grid (a bright, moving cell)
   and rp6 fires the pads as it passes lit steps.

### Track length & polymeter

Each track can be **1 to 4 bars** long, set with its bar-length key. Tracks loop
**independently at their own length** — so a 1-bar hi-hat can run against a
3-bar bass line, and they drift in and out of phase. This is called *polymeter*
and is an easy way to get evolving patterns.

### Tempo

The sequencer runs at the global **TEMPO** set on the main toolbar knob. Change
the tempo there and the sequencer follows.

### Muting

Tap a track's **mute** key to silence it without erasing its steps. The key
greys out and shows a muted-speaker icon; tap again to bring it back. Great for
building up or breaking down a groove live.

---

## Saving, loading & organizing sequences

rp6 keeps **16 sequence slots** (S01–S16).

- **SEQ knob** — choose which slot is loaded. If you change slots *while the
  sequencer is playing*, the switch is **quantized to the next bar** so it stays
  in time (the SEQ display pulses until the change lands).
- **SAVE** — stores the current sequence, letting you give it a name.
- **Copy (⧉)** — duplicates the current sequence into the next slot, so you can
  make a variation without disturbing the original.
- **Clear (🗑)** — empties the steps of the current sequence. **Ctrl+click**
  deletes the sequence entirely and closes the gap in the slot list.

Your working sequence is **autosaved when you quit** and reloaded next launch, so
you can pick up where you left off.

> Sequences are stored **per sound set**: the P-6 hardware, and each emulator kit
> you load, keep their own separate list of slots. Switching backends loads that
> backend's own sequences — they never mix.

---

## Docking it beside the pads

Tap the **Dock** key (▣) to move the sequencer into a column on the right-hand
side of the window, next to the pad grid, instead of stacking below. The pads
shrink to share the space. Tap it again to undock. This is handy on wide
displays where you want the pads and the sequence visible together.

---

## Quick reference

- Show/hide sequencer: **SEQ** bottom-bar button / **Ctrl+Shift+S**
- Change a track's sample: **tap the track's pad key**, then **tap a pad**
- Cancel arming: tap the armed track key again
- Place a hit: **tap a step cell**
- Mute a track: **tap its speaker key**
- Track length: **tap its number key** (cycles 1→4 bars)
- Number of tracks: **TRK** knob (1–8)
- Switch sequence: **SEQ** knob (quantized to the bar while playing)
- Duplicate sequence: **Copy (⧉)**
- Clear steps: **Clear (🗑)** — **Ctrl+click** to delete the sequence
- Save: **SAVE**
- Play/Stop: the **walking-shoes** key
</content>
</invoke>
