//go:build capture && !js

package audio

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	"github.com/gen2brain/malgo"
)

type malgoOutput struct {
	ctx    *malgo.AllocatedContext
	device *malgo.Device
	format Format

	mu     sync.Mutex
	render func([]float32)
	buf    []float32
}

// OpenOutput opens the default float32 stereo output.
func OpenOutput() (Output, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio: init output context: %w", err)
	}
	format := Format{SampleRate: 48000, Channels: 2}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = uint32(format.Channels)
	cfg.SampleRate = uint32(format.SampleRate)
	cfg.PeriodSizeInFrames = 256
	o := &malgoOutput{ctx: ctx, format: format}
	device, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: o.onData})
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("audio: init output device: %w", err)
	}
	o.device = device
	return o, nil
}

func (o *malgoOutput) onData(out, _ []byte, frames uint32) {
	o.mu.Lock()
	render := o.render
	n := int(frames) * o.format.Channels
	if cap(o.buf) < n {
		o.buf = make([]float32, n)
	}
	buf := o.buf[:n]
	o.mu.Unlock()
	clear(buf)
	if render != nil {
		render(buf)
	}
	for i, sample := range buf {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(sample))
	}
}

func (o *malgoOutput) Start(render func([]float32)) error {
	o.mu.Lock()
	o.render = render
	o.mu.Unlock()
	return o.device.Start()
}

func (o *malgoOutput) Stop() error {
	o.mu.Lock()
	o.render = nil
	o.mu.Unlock()
	return o.device.Stop()
}

func (o *malgoOutput) Format() Format { return o.format }
func (o *malgoOutput) Name() string   { return "default output device" }

func (o *malgoOutput) Close() error {
	if o.device != nil {
		o.device.Uninit()
	}
	if o.ctx != nil {
		_ = o.ctx.Uninit()
		o.ctx.Free()
	}
	return nil
}
