package emu

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
	"github.com/mono4loop/rp6/p6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// encodeFLAC encodes mono 16-bit samples as a FLAC stream, using a single
// verbatim frame (no prediction) so the test doesn't depend on the encoder's
// analysis heuristics.
func encodeFLAC(t *testing.T, samples []float32, rate int) []byte {
	t.Helper()
	info := &meta.StreamInfo{
		BlockSizeMin:  uint16(len(samples)),
		BlockSizeMax:  uint16(len(samples)),
		SampleRate:    uint32(rate),
		NChannels:     1,
		BitsPerSample: 16,
		NSamples:      uint64(len(samples)),
	}
	var buf bytes.Buffer
	enc, err := flac.NewEncoder(&buf, info)
	require.NoError(t, err)

	ints := make([]int32, len(samples))
	for i, s := range samples {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		ints[i] = int32(s * 32767)
	}
	f := &frame.Frame{
		Header: frame.Header{
			HasFixedBlockSize: true,
			BlockSize:         uint16(len(samples)),
			SampleRate:        uint32(rate),
			Channels:          frame.ChannelsMono,
			BitsPerSample:     16,
		},
		Subframes: []*frame.Subframe{{
			SubHeader: frame.SubHeader{Pred: frame.PredVerbatim},
			Samples:   ints,
			NSamples:  len(ints),
		}},
	}
	require.NoError(t, enc.WriteFrame(f))
	require.NoError(t, enc.Close())
	return buf.Bytes()
}

// writeFLAC writes a mono 16-bit FLAC to path.
func writeFLAC(t *testing.T, path string, samples []float32, rate int) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, encodeFLAC(t, samples, rate), 0o644))
}

func TestDecodeFLACRoundTrip(t *testing.T) {
	in := sine(440, 44100, 2048)
	clip, err := DecodeFLAC(bytes.NewReader(encodeFLAC(t, in, 44100)))
	require.NoError(t, err)
	assert.Equal(t, 1, clip.Channels)
	assert.Equal(t, 44100, clip.SampleRate)
	require.Equal(t, len(in), len(clip.Samples))
	// FLAC is lossless; only the 16-bit quantization error remains.
	for i := range in {
		assert.InDelta(t, in[i], clip.Samples[i], 1e-3)
	}
}

func TestDecodeFLACRejectsNonFLAC(t *testing.T) {
	_, err := DecodeFLAC(bytes.NewReader([]byte("not a flac file at all")))
	assert.Error(t, err)
}

func TestIsAudioFile(t *testing.T) {
	assert.True(t, isAudioFile("A1.wav"))
	assert.True(t, isAudioFile("A1.WAV"))
	assert.True(t, isAudioFile("A1.flac"))
	assert.True(t, isAudioFile("A1.FLAC"))
	assert.False(t, isAudioFile("A1.mp3"))
	assert.False(t, isAudioFile("readme.txt"))
}

// TestScanSamplesFLAC verifies FLAC pad files are picked up across all three
// layouts, mixed with WAV.
func TestScanSamplesFLAC(t *testing.T) {
	dir := t.TempDir()
	// Flat FLAC pad label.
	writeFLAC(t, filepath.Join(dir, "A1.flac"), sine(100, 8000, 100), 8000)
	// Flat WAV coexists.
	writeWAV(t, filepath.Join(dir, "C4.wav"), sine(100, 8000, 100), 8000)
	// Per-bank subdirectory, FLAC.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "B"), 0o755))
	writeFLAC(t, filepath.Join(dir, "B", "2.flac"), sine(100, 8000, 100), 8000)
	// P-6 layout, FLAC inside the pad dir.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "BANK_H", "PAD_6"), 0o755))
	writeFLAC(t, filepath.Join(dir, "BANK_H", "PAD_6", "crash.flac"), sine(100, 8000, 100), 8000)

	paths, err := scanSamples(os.DirFS(dir))
	require.NoError(t, err)
	assert.Len(t, paths, 4)
	assert.Contains(t, paths, padID(0, 1)) // A1
	assert.Contains(t, paths, padID(1, 2)) // B2
	assert.Contains(t, paths, padID(2, 4)) // C4
	assert.Contains(t, paths, padID(7, 6)) // H6
	assert.True(t, isFLAC(paths[padID(0, 1)]))
}

// TestOpenFSFLAC loads a FLAC pad through the full OpenFS path (silent stub sink
// in tests) and triggers it.
func TestOpenFSFLAC(t *testing.T) {
	dir := t.TempDir()
	writeFLAC(t, filepath.Join(dir, "A1.flac"), sine(220, 8000, 200), 8000)

	e, err := Open(dir, p6.DefaultConfig())
	require.NoError(t, err)
	defer e.Close()
	assert.Equal(t, 1, e.Loaded())
	assert.NoError(t, e.TriggerPad(0, 1))
}
