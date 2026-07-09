package emu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
)

// Clip is decoded PCM audio: interleaved float32 samples in [-1, 1], plus the
// channel count and sample rate they were decoded at.
type Clip struct {
	Samples    []float32 // interleaved: frame0[ch0,ch1,...], frame1[...], ...
	Channels   int
	SampleRate int
}

// errNotWAV and friends describe decode failures.
var (
	errNotWAV      = errors.New("emu: not a RIFF/WAVE file")
	errNoFmt       = errors.New("emu: WAV missing fmt chunk")
	errNoData      = errors.New("emu: WAV missing data chunk")
	errUnsupported = errors.New("emu: unsupported WAV format")
)

// WAV audio format codes.
const (
	wavPCM        = 1
	wavIEEEFloat  = 3
	wavExtensible = 0xFFFE
)

// DecodeWAV reads a linear-PCM or IEEE-float WAV from r. It supports 8/16/24/32
// bit integer PCM and 32/64 bit float, mono or multi-channel, and returns the
// samples as interleaved float32 in [-1, 1]. This is a small, dependency-free
// decoder — enough for the sample files the P-6 (and its SampleTool) use.
func DecodeWAV(r io.Reader) (*Clip, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("emu: reading WAV: %w", err)
	}
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errNotWAV
	}

	var (
		haveFmt            bool
		audioFmt, channels int
		sampleRate         int
		bitsPerSample      int
		dataBytes          []byte
		haveData           bool
	)

	// Walk the chunks after the 12-byte RIFF/WAVE header.
	pos := 12
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		body := pos + 8
		if body+size > len(data) {
			size = len(data) - body // tolerate a truncated final chunk
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errUnsupported
			}
			audioFmt = int(binary.LittleEndian.Uint16(data[body : body+2]))
			channels = int(binary.LittleEndian.Uint16(data[body+2 : body+4]))
			sampleRate = int(binary.LittleEndian.Uint32(data[body+4 : body+8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[body+14 : body+16]))
			if audioFmt == wavExtensible && size >= 26 {
				// The real format code lives in the SubFormat GUID's first 2 bytes.
				audioFmt = int(binary.LittleEndian.Uint16(data[body+24 : body+26]))
			}
			haveFmt = true
		case "data":
			dataBytes = data[body : body+size]
			haveData = true
		}
		// Chunks are word-aligned: skip a pad byte after odd sizes.
		pos = body + size
		if size%2 == 1 {
			pos++
		}
	}

	if !haveFmt {
		return nil, errNoFmt
	}
	if !haveData {
		return nil, errNoData
	}
	if channels <= 0 || sampleRate <= 0 {
		return nil, errUnsupported
	}

	samples, err := pcmToFloat32(dataBytes, audioFmt, bitsPerSample)
	if err != nil {
		return nil, err
	}
	// Trim any trailing partial frame so len is a whole number of frames.
	if n := len(samples) % channels; n != 0 {
		samples = samples[:len(samples)-n]
	}
	return &Clip{Samples: samples, Channels: channels, SampleRate: sampleRate}, nil
}

// pcmToFloat32 converts a raw PCM/float data chunk to []float32 in [-1, 1].
func pcmToFloat32(b []byte, audioFmt, bits int) ([]float32, error) {
	switch audioFmt {
	case wavPCM:
		switch bits {
		case 8: // 8-bit PCM is unsigned (0..255, midpoint 128)
			out := make([]float32, len(b))
			for i, v := range b {
				out[i] = (float32(v) - 128) / 128
			}
			return out, nil
		case 16:
			n := len(b) / 2
			out := make([]float32, n)
			for i := range n {
				s := int16(binary.LittleEndian.Uint16(b[i*2:]))
				out[i] = float32(s) / 32768
			}
			return out, nil
		case 24:
			n := len(b) / 3
			out := make([]float32, n)
			for i := range n {
				u := uint32(b[i*3]) | uint32(b[i*3+1])<<8 | uint32(b[i*3+2])<<16
				if u&0x800000 != 0 { // sign-extend
					u |= 0xFF000000
				}
				out[i] = float32(int32(u)) / 8388608
			}
			return out, nil
		case 32:
			n := len(b) / 4
			out := make([]float32, n)
			for i := range n {
				s := int32(binary.LittleEndian.Uint32(b[i*4:]))
				out[i] = float32(s) / 2147483648
			}
			return out, nil
		}
	case wavIEEEFloat:
		switch bits {
		case 32:
			n := len(b) / 4
			out := make([]float32, n)
			for i := range n {
				out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
			}
			return out, nil
		case 64:
			n := len(b) / 8
			out := make([]float32, n)
			for i := range n {
				out[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(b[i*8:])))
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("%w: format %d, %d-bit", errUnsupported, audioFmt, bits)
}

// EncodeWAV writes samples (interleaved float32 in [-1, 1]) as a 16-bit PCM
// WAV to w. It is the inverse of DecodeWAV for the common case and is used by
// tests and to synthesize demo samples.
func EncodeWAV(w io.Writer, samples []float32, channels, sampleRate int) error {
	if channels <= 0 {
		channels = 1
	}
	pcm := make([]byte, len(samples)*2)
	for i, s := range samples {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		v := int16(s * 32767)
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(v))
	}
	dataLen := len(pcm)
	byteRate := sampleRate * channels * 2
	blockAlign := channels * 2

	var hdr [44]byte
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(36+dataLen))
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(hdr[20:22], wavPCM)
	binary.LittleEndian.PutUint16(hdr[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(hdr[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(hdr[34:36], 16) // bits per sample
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(dataLen))

	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("emu: writing WAV header: %w", err)
	}
	if _, err := w.Write(pcm); err != nil {
		return fmt.Errorf("emu: writing WAV data: %w", err)
	}
	return nil
}

// Resample converts a clip to the given channel count and sample rate,
// returning interleaved float32 samples. Channel remixing is done first (mono
// duplicated to N channels, extra channels averaged down to fewer), then the
// rate is converted with a band-limited (windowed-sinc) resampler. A clip
// already in the target format is returned as-is (copied).
func (c *Clip) Resample(dstChannels, dstRate int) []float32 {
	if dstChannels <= 0 {
		dstChannels = 1
	}
	if dstRate <= 0 {
		dstRate = c.SampleRate
	}
	// 1. Remix channels.
	remixed := remixChannels(c.Samples, c.Channels, dstChannels)
	// 2. Resample rate.
	if c.SampleRate == dstRate {
		return remixed
	}
	return resampleSinc(remixed, dstChannels, c.SampleRate, dstRate)
}

// remixChannels converts interleaved audio from srcCh to dstCh channels.
func remixChannels(in []float32, srcCh, dstCh int) []float32 {
	if srcCh <= 0 {
		srcCh = 1
	}
	if srcCh == dstCh {
		out := make([]float32, len(in))
		copy(out, in)
		return out
	}
	frames := len(in) / srcCh
	out := make([]float32, frames*dstCh)
	for f := range frames {
		// Average the source channels to a single mono value, then fan it out
		// to every destination channel. Equal-channel counts already returned
		// above, so this covers mono->stereo (duplicate) and any downmix
		// (fold all source channels in) — sensible defaults for pad samples.
		var sum float32
		for ch := 0; ch < srcCh; ch++ {
			sum += in[f*srcCh+ch]
		}
		mono := sum / float32(srcCh)
		for ch := range dstCh {
			out[f*dstCh+ch] = mono
		}
	}
	return out
}

// Windowed-sinc resampler parameters. sincHalf taps each side gives a 2*sincHalf
// tap kernel; sincPhases quantizes the fractional sample position. These give a
// stopband well below −70 dB — far cleaner than linear interpolation, whose
// imaging/roll-off is audible as a subtle "lo-fi" downsampled quality.
const (
	sincHalf   = 16
	sincTaps   = 2 * sincHalf
	sincPhases = 512
)

// resampleSinc resamples interleaved audio from srcRate to dstRate using a
// band-limited windowed-sinc (Blackman-windowed) polyphase kernel. For
// downsampling the sinc cutoff is lowered to the destination Nyquist to prevent
// aliasing; for upsampling the cutoff is the source Nyquist. The kernel is
// precomputed per phase (no transcendentals in the inner loop) and normalized so
// the passband gain is unity.
func resampleSinc(in []float32, channels, srcRate, dstRate int) []float32 {
	if channels <= 0 {
		channels = 1
	}
	srcFrames := len(in) / channels
	if srcFrames == 0 {
		return nil
	}
	dstFrames := int(int64(srcFrames) * int64(dstRate) / int64(srcRate))
	if dstFrames <= 0 {
		return nil
	}

	cutoff := 1.0 // normalized to the source Nyquist
	if dstRate < srcRate {
		cutoff = float64(dstRate) / float64(srcRate)
	}
	table := sincTable(cutoff)

	out := make([]float32, dstFrames*channels)
	ratio := float64(srcRate) / float64(dstRate) // input frames per output frame
	for f := range dstFrames {
		center := float64(f) * ratio
		i0 := int(center)
		phase := int((center - float64(i0)) * sincPhases)
		if phase >= sincPhases {
			phase = sincPhases - 1
		}
		kernel := &table[phase]
		for c := 0; c < channels; c++ {
			var acc float32
			for t := 0; t < sincTaps; t++ {
				idx := i0 + t - (sincHalf - 1) // tap 0 -> i0-(sincHalf-1)
				if idx < 0 || idx >= srcFrames {
					continue
				}
				acc += in[idx*channels+c] * kernel[t]
			}
			out[f*channels+c] = acc
		}
	}
	return out
}

// sincTable returns the windowed-sinc kernel table for cutoff, building it once
// and caching it. A kit's pads usually share the same source→sink rate ratio (so
// the same cutoff), and the table is ~64 KB of transcendental evaluations; caching
// it avoids rebuilding an identical table for every one of the (up to 48) pads
// loaded, which was a real chunk of the pak-load time. The cached table is
// read-only, so the shared pointer is safe for concurrent resamples.
func sincTable(cutoff float64) *[sincPhases][sincTaps]float32 {
	sincCacheMu.Lock()
	defer sincCacheMu.Unlock()
	if t, ok := sincCache[cutoff]; ok {
		return t
	}
	t := buildSincTable(cutoff)
	sincCache[cutoff] = t
	return t
}

var (
	sincCacheMu sync.Mutex
	sincCache   = map[float64]*[sincPhases][sincTaps]float32{}
)

// buildSincTable precomputes the windowed-sinc kernel for each fractional phase,
// at the given cutoff (normalized to the source Nyquist). Each phase's taps are
// normalized to sum to 1 so the resampler has unity passband gain.
func buildSincTable(cutoff float64) *[sincPhases][sincTaps]float32 {
	var table [sincPhases][sincTaps]float32
	for p := range sincPhases {
		frac := float64(p) / float64(sincPhases)
		var sum float64
		var w [sincTaps]float64
		for t := range sincTaps {
			k := t - (sincHalf - 1) // input offset for this tap
			x := frac - float64(k)  // distance (in source samples) from the output point
			w[t] = windowedSinc(x, cutoff)
			sum += w[t]
		}
		if sum == 0 {
			sum = 1
		}
		for t := range sincTaps {
			table[p][t] = float32(w[t] / sum)
		}
	}
	return &table
}

// windowedSinc evaluates a Blackman-windowed sinc at distance x (in source
// samples) with the given cutoff (normalized to the source Nyquist, ≤1). It is
// zero outside the ±sincHalf window.
func windowedSinc(x, cutoff float64) float64 {
	if x <= -float64(sincHalf) || x >= float64(sincHalf) {
		return 0
	}
	var s float64
	if x == 0 {
		s = cutoff
	} else {
		px := math.Pi * x
		s = math.Sin(cutoff*px) / px
	}
	// Blackman window over [-sincHalf, sincHalf].
	n := (x + float64(sincHalf)) / (2 * float64(sincHalf)) // 0..1
	win := 0.42 - 0.5*math.Cos(2*math.Pi*n) + 0.08*math.Cos(4*math.Pi*n)
	return s * win
}
