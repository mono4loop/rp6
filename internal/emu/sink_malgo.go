//go:build capture && !js

package emu

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	"github.com/gen2brain/malgo"
)

// malgoSink plays audio via miniaudio (malgo), which loads a system backend
// (ALSA/PulseAudio/PipeWire) at runtime — no link-time audio deps, matching the
// capture path in internal/audio.
type malgoSink struct {
	ctx      *malgo.AllocatedContext
	device   *malgo.Device
	channels int
	rate     int
	name     string

	mu     sync.Mutex
	render func(out []float32)
	buf    []float32 // reused across callbacks to avoid per-callback allocation
}

// openSink opens the default playback device as interleaved float32 stereo.
func openSink() (sink, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("emu: init audio context: %w", err)
	}

	const channels = 2
	const sampleRate = 48000

	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = channels
	cfg.SampleRate = sampleRate
	// A small period (~5 ms @ 48 kHz) keeps trigger latency and — in
	// buffer-aligned timing mode — the worst-case flam low. Sample-accurate
	// timing (the default) also benefits from the lower latency.
	cfg.PeriodSizeInFrames = 256

	s := &malgoSink{ctx: ctx, channels: channels, rate: sampleRate, name: "default output device"}
	device, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: s.onData})
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("emu: init playback device: %w", err)
	}
	s.device = device
	return s, nil
}

// onData fills miniaudio's little-endian float32 output buffer from render.
func (s *malgoSink) onData(out, _ []byte, frames uint32) {
	s.mu.Lock()
	r := s.render
	n := int(frames) * s.channels
	if cap(s.buf) < n {
		s.buf = make([]float32, n)
	}
	buf := s.buf[:n]
	s.mu.Unlock()

	if r != nil {
		r(buf)
	} else {
		for i := range buf {
			buf[i] = 0
		}
	}
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(buf[i]))
	}
}

func (s *malgoSink) Start(render func(out []float32)) error {
	s.mu.Lock()
	s.render = render
	s.mu.Unlock()
	return s.device.Start()
}

func (s *malgoSink) Stop() error {
	s.mu.Lock()
	s.render = nil
	s.mu.Unlock()
	return s.device.Stop()
}

func (s *malgoSink) SampleRate() int { return s.rate }
func (s *malgoSink) Channels() int   { return s.channels }
func (s *malgoSink) Name() string    { return s.name }

func (s *malgoSink) Close() error {
	if s.device != nil {
		s.device.Uninit()
	}
	if s.ctx != nil {
		_ = s.ctx.Uninit()
		s.ctx.Free()
	}
	return nil
}
