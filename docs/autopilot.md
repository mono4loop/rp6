# Scripted runs (autopilot)

RP6 can be driven by a script: taps, drags and typing, with every touch marked
on the window. Use it to record demos that come out the same on every take, and
for smoke checks on a real display or an Android device, where
`adb shell input tap` coordinates don't survive a rotated screen.

It's [Fade's autopilot](https://code.rbel.co/rubiojr/fade/src/branch/main/docs/guide/15-scripted-demos.md);
that guide documents the script language. This page covers what's specific to
RP6.

## Turn it on

It takes a build tag and a script:

```sh
FADE_AUTOPILOT=docs/autopilot/smoke.txt \
  make run TAGS="capture wayland migrated_fynedo jam autopilot"
```

Without the `autopilot` tag none of it is compiled in, so normal builds stay as
they are. Without `FADE_AUTOPILOT`, a tagged build runs as usual.

Sequences and preferences survive between runs. Start every take from the same
state by pointing them at empty directories:

```sh
make build TAGS="capture wayland migrated_fynedo jam autopilot"
XDG_DATA_HOME=$(mktemp -d) XDG_CONFIG_HOME=$(mktemp -d) FADE_AUTOPILOT=demo.txt ./build/rp6
```

A binary built without the tag ignores `FADE_AUTOPILOT`, so the script never
runs.

With no P-6 plugged in, RP6 falls back to the emulator's built-in kit, so a run
sounds the same every time. With a P-6 connected, the script plays the P-6.

### On Android

The app reads `autopilot.txt` from its own folder of shared storage:

```sh
make android ANDROID_ABI=android/arm64 ANDROID_TAGS=capture,migrated_fynedo,autopilot
adb install -r build/android/RP6.apk
adb push docs/autopilot/smoke.txt /sdcard/Android/data/io.github.mono4loop.rp6/files/autopilot.txt
adb logcat -s Fyne
```

It runs on every start until you delete the file. `make android` bumps
`cmd/rp6/version.go`; revert it unless you're cutting a release.

## Targets

- **Text on screen**: `tap "LOOP"`, `tap "A2"`, `wait "TEMPO"`. Pad labels
  also show on the sequencer's pad-assign keys, so `"A1"` can match either;
  pick one with `#2` or `in`, or use a name.
- **Names**: every inspection ID in `cmd/rp6/inspection.go` (the IDs in the
  `make inspect-layouts` manifests), with underscores for dots:
  `pads_cell_01`, `sequencer_track_1_step_5`, `sequencer_play`,
  `transport_tempo`, `navigation_page_loop`, `recorder_track_2_record`. Pad
  cells count grid positions, not pad labels: on the E–H page `pads_cell_01`
  is E1.
- **`wait idle`** waits until no sample pak is loading in the background and
  no relayout is queued. Use it after selecting a sample pak or samples folder:
  the swap stops the transport when the load finishes. Page switches and device
  connects finish before the next step anyway, so there it's only a short
  pause. It doesn't know about a window resize still on its way from the
  compositor (leaving the console layout), so add a `wait` there.

## Example

```text
# Program a four-on-the-floor on track 1, play it, slow it down.
wait idle
tap sequencer_track_1_step_1
tap sequencer_track_1_step_5
tap sequencer_track_1_step_9
tap sequencer_track_1_step_13
tap sequencer_play
wait 4s
drag transport_tempo by 0 -60 over 1s
wait 4s
tap sequencer_play
```

[`autopilot/smoke.txt`](autopilot/smoke.txt) is a short smoke check. On
success RP6 quits; a step that fails stops the script and leaves RP6 open, so
run it under `timeout` and treat a timeout as a failure.

## Limits

- **No modifiers.** Ctrl+click (Ctrl+click Clear deletes the whole sequence)
  and the Ctrl+Shift shortcuts can't be scripted. Tap the buttons instead.
- **`quit` skips RP6's shutdown.** It doesn't autosave the working sequence
  or send MIDI Stop to a P-6. Stop the transport before quitting; with
  throwaway XDG directories nothing is lost.
- **Main window only.** A floated pad rack lives in a window of its own,
  which the script doesn't see. Dock it first.
- **No `scroll` or `hover` on Android.** Drag a knob instead.
- **No recording.** Use a screen recorder (`scrcpy --record` on Android).

## How it's wired

`cmd/rp6/autopilot.go` attaches the script to the main window, resolving names
through the inspection targets and reporting idle.

Fade's autopilot on Linux needs Fade's patched GLFW, which adds Wayland touch.
`go.mod` replaces `github.com/go-gl/glfw/v3.4/glfw` with it for every build, not
only tagged ones, so a tagged and an untagged build run the same GLFW. Keep the
`fade` requirement and the replacement on the same Fade commit when you update
them.

Widgets must give their `canvas.Text` a size, not only a position: Fyne draws
zero-size text, but autopilot doesn't count it as on screen
(`TestCaptionsAreSized` guards the knob, rack toggle and device badge).
