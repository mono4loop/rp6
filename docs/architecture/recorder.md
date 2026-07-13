# Recorder rack

The recorder is a host-side eight-track audio clip recorder. It records PCM
audio, not MIDI events, so tracks can play simultaneously, mute independently,
and use independent DSP effects.

## Signal paths

- P-6 hardware: the app owns one strict, non-monitor USB capture stream. Its
  callback fans frames to the VU meter and recorder. A separate host output
  pulls the recorder mix.
- Emulator: the recorder attaches to the emulator's existing output callback.
  The capture tap receives the emulator mix after its limiter but before
  recorder playback is added, preventing feedback. Recorder playback then joins
  the emulator output and passes through a final limiter.
- Builds without an audio output can still load/edit/export projects, but cannot
  produce host playback. Web and mobile emulator builds use the emulator's own
  Web Audio/miniaudio output.

`internal/recorder` owns all audio state and imports no Fyne, MIDI, or device
packages. `cmd/rp6/recorderrack.go` is only the touch UI adapter.

## Real-time rules

- Audio callbacks do not write files or update Fyne directly.
- Playback processing uses fixed preallocated buffers and performs no callback
  allocations.
- A recording allocates one bounded 30-second take when armed pad input starts;
  the capture callback only copies into it.
- Play All computes one target frame and queues every populated track against
  that frame, preserving exact alignment.
- Quantization is optional: OFF, next beat, or next bar at the global tempo.
- Playback quantization uses the output frame clock; recording quantization uses
  the capture frame clock. Hardware input/output devices may drift, so the two
  domains are deliberately independent.
- Arming reserves the bounded take buffer before the live pad press. A take that
  reaches the 30-second limit is retained and marked `MAX` in the rack.

## Persistence

Projects are scoped to the same hardware/emulator-kit profile as sequences.
Metadata is JSON and each populated track is a 16-bit WAV under the RP6 data
directory's `recordings/` tree. The profile directory uses a full stable hash so
filesystem characters in emulator paths cannot escape the storage root. Saves
replace clips and the manifest atomically; the manifest is replaced last.

The browser currently keeps recorder projects for the session and supports WAV
export; durable large-clip browser storage remains part of the recorder epic.
