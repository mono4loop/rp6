package emu

// sink is an audio output the emulator renders into. Start begins pulling
// output frames from render (called on the audio thread; it must fill the whole
// buffer and not block). SampleRate/Channels describe the format render must
// produce. A silent stub sink is used when no audio backend is compiled in.
type sink interface {
	Start(render func(out []float32)) error
	Stop() error
	SampleRate() int
	Channels() int
	Name() string
	Close() error
}
