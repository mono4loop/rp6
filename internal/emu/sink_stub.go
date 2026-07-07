//go:build !capture && !js

package emu

// openSink without the "capture" build tag returns a silent sink: the emulator
// still loads samples and mixes voices (so it is fully swappable and testable),
// it just produces no sound. Build with -tags capture (as `make run`/`build`
// do) to hear it. The format matches the malgo backend so loaded clips are
// resampled consistently.
func openSink() (sink, error) { return silentSink{}, nil }

type silentSink struct{}

func (silentSink) Start(func(out []float32)) error { return nil }
func (silentSink) Stop() error                     { return nil }
func (silentSink) SampleRate() int                 { return 48000 }
func (silentSink) Channels() int                   { return 2 }
func (silentSink) Name() string {
	return "silent (no audio backend — build with -tags capture)"
}
func (silentSink) Close() error { return nil }
