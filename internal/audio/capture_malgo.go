//go:build capture

package audio

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"

	"github.com/gen2brain/malgo"
)

// malgoCapturer captures audio via miniaudio (malgo), which loads a system
// backend (ALSA/PulseAudio/PipeWire) at runtime — no link-time audio deps.
type malgoCapturer struct {
	ctx    *malgo.AllocatedContext
	device *malgo.Device
	format Format
	name   string

	mu sync.Mutex
	fn FrameFunc
}

// OpenCapture opens the first capture device whose name contains nameMatch
// (case-insensitive), capturing float32 frames.
func OpenCapture(nameMatch string) (Capturer, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio: init context: %w", err)
	}

	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("audio: enumerate devices: %w", err)
	}

	names := make([]string, len(infos))
	for i := range infos {
		names[i] = infos[i].Name()
	}
	log.Printf("audio: capture devices: %q", names)

	// Match on alphanumerics only so "P-6" matches "Roland P-6", "P-6 ", etc.
	// Skip PulseAudio/PipeWire "Monitor of ..." sources (those capture what the
	// computer plays TO the device, not the device's own output); fall back to
	// a monitor only if nothing else matches.
	want := normalize(nameMatch)
	pick := func(allowMonitor bool) *malgo.DeviceInfo {
		for i := range infos {
			n := normalize(infos[i].Name())
			if !strings.Contains(n, want) {
				continue
			}
			if !allowMonitor && strings.Contains(n, "monitor") {
				continue
			}
			return &infos[i]
		}
		return nil
	}
	chosen := pick(false)
	if chosen == nil {
		chosen = pick(true)
	}
	if chosen == nil {
		_ = ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("audio: no capture device matching %q in %q: %w", nameMatch, names, ErrUnavailable)
	}
	log.Printf("audio: capturing from %q", chosen.Name())

	const channels = 2
	const sampleRate = 48000

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = channels
	cfg.Capture.DeviceID = chosen.ID.Pointer()
	cfg.SampleRate = sampleRate

	c := &malgoCapturer{ctx: ctx, name: chosen.Name(), format: Format{SampleRate: sampleRate, Channels: channels}}

	device, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: c.onData})
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("audio: init device: %w", err)
	}
	c.device = device
	return c, nil
}

// onData converts miniaudio's little-endian float32 byte buffer into samples.
func (c *malgoCapturer) onData(_, in []byte, frames uint32) {
	c.mu.Lock()
	fn := c.fn
	c.mu.Unlock()
	if fn == nil {
		return
	}
	n := len(in) / 4
	samples := make([]float32, n)
	for i := 0; i < n; i++ {
		samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(in[i*4:]))
	}
	fn(samples)
}

func (c *malgoCapturer) Start(fn FrameFunc) error {
	c.mu.Lock()
	c.fn = fn
	c.mu.Unlock()
	return c.device.Start()
}

func (c *malgoCapturer) Stop() error {
	c.mu.Lock()
	c.fn = nil
	c.mu.Unlock()
	return c.device.Stop()
}

func (c *malgoCapturer) Format() Format { return c.format }

func (c *malgoCapturer) Name() string { return c.name }

func (c *malgoCapturer) Close() error {
	if c.device != nil {
		c.device.Uninit()
	}
	if c.ctx != nil {
		_ = c.ctx.Uninit()
		c.ctx.Free()
	}
	return nil
}

// normalize lowercases and keeps only letters/digits, so device-name matching
// is tolerant of punctuation/spacing ("P-6" ~ "Roland P-6").
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
