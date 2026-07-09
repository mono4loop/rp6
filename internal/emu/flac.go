package emu

import (
	"errors"
	"fmt"
	"io"

	"github.com/mewkiz/flac"
)

// DecodeFLAC reads a FLAC stream from r and returns its samples as interleaved
// float32 in [-1, 1], matching DecodeWAV's Clip. FLAC is a lossless codec, so
// this is used for rp6's own emulator sample paks; the P-6 hardware only
// understands WAV, so FLAC paks are emulator-only.
func DecodeFLAC(r io.Reader) (*Clip, error) {
	stream, err := flac.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("emu: parsing FLAC: %w", err)
	}
	defer stream.Close()

	channels := int(stream.Info.NChannels)
	rate := int(stream.Info.SampleRate)
	bits := int(stream.Info.BitsPerSample)
	if channels <= 0 || rate <= 0 || bits <= 0 {
		return nil, fmt.Errorf("%w: FLAC %d ch, %d Hz, %d-bit", errUnsupported, channels, rate, bits)
	}
	// Normalize signed PCM of the given bit depth to [-1, 1).
	scale := float32(int64(1) << (bits - 1))

	// Preallocate when the total sample count is known (0 = unknown).
	var samples []float32
	if n := stream.Info.NSamples; n > 0 {
		samples = make([]float32, 0, int(n)*channels)
	}

	for {
		frame, err := stream.ParseNext()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("emu: decoding FLAC frame: %w", err)
		}
		if len(frame.Subframes) != channels {
			return nil, fmt.Errorf("%w: FLAC frame has %d subframes, want %d", errUnsupported, len(frame.Subframes), channels)
		}
		// Interleave the per-channel subframe samples (already decorrelated by
		// Frame.Parse) into frame-major float32.
		n := frame.Subframes[0].NSamples
		for i := range n {
			for ch := 0; ch < channels; ch++ {
				samples = append(samples, float32(frame.Subframes[ch].Samples[i])/scale)
			}
		}
	}

	if len(samples) == 0 {
		return nil, errNoData
	}
	return &Clip{Samples: samples, Channels: channels, SampleRate: rate}, nil
}
