package recorder

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const manifestName = "project.json"

// Save writes project metadata and one WAV per populated track. The manifest is
// replaced last, so interrupted saves never advertise an incomplete new clip.
func (e *Engine) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("recorder: create project directory: %w", err)
	}
	state := e.Snapshot()
	for i, track := range state.Tracks {
		path := filepath.Join(dir, trackFile(i))
		if !track.HasClip {
			continue
		}
		clip, ok := e.Clip(i)
		if !ok {
			return fmt.Errorf("recorder: track %d clip disappeared during save", i+1)
		}
		if err := writeAtomic(path, func(w io.Writer) error { return EncodeWAV(w, clip) }); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("recorder: encode project: %w", err)
	}
	if err := writeAtomic(filepath.Join(dir, manifestName), func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	}); err != nil {
		return err
	}
	for i, track := range state.Tracks {
		if !track.HasClip {
			_ = os.Remove(filepath.Join(dir, trackFile(i)))
		}
	}
	return nil
}

func writeAtomic(path string, write func(io.Writer) error) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("recorder: create %s: %w", filepath.Base(path), err)
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := write(f); err != nil {
		return fmt.Errorf("recorder: write %s: %w", filepath.Base(path), err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("recorder: sync %s: %w", filepath.Base(path), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("recorder: close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("recorder: replace %s: %w", filepath.Base(path), err)
	}
	ok = true
	return nil
}

func trackFile(track int) string { return fmt.Sprintf("track-%d.wav", track+1) }

// Load restores project metadata and clips. A missing project is an empty
// project, not an error.
func (e *Engine) Load(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if errors.Is(err, os.ErrNotExist) {
		e.Reset()
		return nil
	}
	if err != nil {
		return fmt.Errorf("recorder: read project: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("recorder: decode project: %w", err)
	}
	e.Reset()
	e.Restore(state)
	for i, track := range state.Tracks {
		if !track.HasClip {
			continue
		}
		f, err := os.Open(filepath.Join(dir, trackFile(i)))
		if err != nil {
			return fmt.Errorf("recorder: open track %d: %w", i+1, err)
		}
		clip, decodeErr := DecodeWAV(f)
		closeErr := f.Close()
		if decodeErr != nil {
			return fmt.Errorf("recorder: decode track %d: %w", i+1, decodeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("recorder: close track %d: %w", i+1, closeErr)
		}
		if err := e.SetClip(i, clip); err != nil {
			return fmt.Errorf("recorder: load track %d: %w", i+1, err)
		}
	}
	return nil
}

// ExportWAV writes one track as a portable 16-bit PCM WAV.
func (e *Engine) ExportWAV(track int, w io.Writer) error {
	clip, ok := e.Clip(track)
	if !ok {
		return ErrTrack
	}
	return EncodeWAV(w, clip)
}

// EncodeWAV writes an interleaved clip as 16-bit PCM.
func EncodeWAV(w io.Writer, clip Clip) error {
	channels, rate := validFormat(clip.Channels, clip.SampleRate)
	dataLen := len(clip.Samples) * 2
	var header [44]byte
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataLen))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(header[32:34], uint16(channels*2))
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataLen))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	pcm := make([]byte, dataLen)
	for i, sample := range clip.Samples {
		sample = clamp(sample, -1, 1)
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(sample*32767)))
	}
	_, err := w.Write(pcm)
	return err
}

// DecodeWAV reads the 16-bit PCM WAV format written by EncodeWAV.
func DecodeWAV(r io.Reader) (Clip, error) {
	const maxProjectWAVBytes = MaxRecordSeconds*192000*4 + 1<<20
	data, err := io.ReadAll(io.LimitReader(r, maxProjectWAVBytes+1))
	if err != nil {
		return Clip{}, err
	}
	if len(data) > maxProjectWAVBytes {
		return Clip{}, errors.New("WAV exceeds recorder limit")
	}
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return Clip{}, errors.New("not a WAV file")
	}
	channels, rate, pcm, err := decodeWAVChunks(data)
	if err != nil {
		return Clip{}, err
	}
	samples := make([]float32, len(pcm)/2)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
	}
	samples = samples[:len(samples)-len(samples)%channels]
	return Clip{Samples: samples, Channels: channels, SampleRate: rate}, nil
}

func decodeWAVChunks(data []byte) (channels, rate int, pcm []byte, err error) {
	var bits int
	for pos := 12; pos+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		body := pos + 8
		if body+size > len(data) {
			return 0, 0, nil, errors.New("truncated WAV chunk")
		}
		switch string(data[pos : pos+4]) {
		case "fmt ":
			if size < 16 || binary.LittleEndian.Uint16(data[body:body+2]) != 1 {
				return 0, 0, nil, errors.New("unsupported WAV encoding")
			}
			channels = int(binary.LittleEndian.Uint16(data[body+2 : body+4]))
			rate = int(binary.LittleEndian.Uint32(data[body+4 : body+8]))
			bits = int(binary.LittleEndian.Uint16(data[body+14 : body+16]))
		case "data":
			pcm = data[body : body+size]
		}
		pos = body + size + (size & 1)
	}
	if channels <= 0 || rate <= 0 || bits != 16 || pcm == nil {
		return 0, 0, nil, errors.New("unsupported WAV format")
	}
	return channels, rate, pcm, nil
}
