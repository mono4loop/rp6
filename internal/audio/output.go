package audio

// Output is an audio device that pulls interleaved float32 PCM from Render.
// Render runs on the audio thread, must fill the entire buffer, and must not
// block or allocate.
type Output interface {
	Start(render func(out []float32)) error
	Stop() error
	Format() Format
	Name() string
	Close() error
}
