//go:build !js && !android

package p6

import (
	"bytes"
	"errors"
	"io/fs"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageBuilders(t *testing.T) {
	// Note-on on channel 11 (sampler), note 48, velocity 100 -> 0x9A 0x30 0x64.
	assert.Equal(t, []byte{0x9A, 0x30, 0x64}, noteOnBytes(11, 48, 100))
	// Control change on channel 15 (auto), CC 19 (granular head position), 64.
	assert.Equal(t, []byte{0xBE, 0x13, 0x40}, controlChangeBytes(15, 19, 64))
	// Program change on channel 16, program 5.
	assert.Equal(t, []byte{0xCF, 0x05}, programChangeBytes(16, 5))
}

func TestMessageBuildersMaskDataBytes(t *testing.T) {
	// Data bytes must stay within 7 bits.
	on := noteOnBytes(1, 200, 200)
	assert.Equal(t, byte(0x90), on[0])
	assert.Equal(t, byte(200&0x7f), on[1])
	assert.Equal(t, byte(200&0x7f), on[2])
}

func TestClampChannel(t *testing.T) {
	assert.Equal(t, byte(0), clampChannel(1))
	assert.Equal(t, byte(15), clampChannel(16))
	assert.Equal(t, byte(0), clampChannel(0))   // clamps up to 1
	assert.Equal(t, byte(15), clampChannel(99)) // clamps down to 16
}

func TestDeviceTriggerPadWritesCorrectBytes(t *testing.T) {
	var buf bytes.Buffer
	d := New(&buf, DefaultConfig())

	require.NoError(t, d.TriggerPad(4, 1)) // bank E, pad 1 -> note 72
	assert.Equal(t, []byte{0x9A, 72, DefaultVelocity}, buf.Bytes())
}

func TestDeviceTriggerPadVelocity(t *testing.T) {
	var buf bytes.Buffer
	d := New(&buf, DefaultConfig())

	require.NoError(t, d.TriggerPadVelocity(0, 1, 42)) // bank A, pad 1
	assert.Equal(t, []byte{0x9A, 48, 42}, buf.Bytes())
}

func TestDeviceTriggerPadInvalid(t *testing.T) {
	var buf bytes.Buffer
	d := New(&buf, DefaultConfig())

	assert.Error(t, d.TriggerPad(99, 1))
	assert.Empty(t, buf.Bytes(), "no bytes should be written for an invalid pad")
}

func TestDeviceTransport(t *testing.T) {
	var buf bytes.Buffer
	d := New(&buf, DefaultConfig())

	require.NoError(t, d.Start())
	require.NoError(t, d.Stop())
	require.NoError(t, d.Continue())
	require.NoError(t, d.Clock())
	assert.Equal(t, []byte{0xFA, 0xFC, 0xFB, 0xF8}, buf.Bytes())
}

func TestDeviceProgramChange(t *testing.T) {
	var buf bytes.Buffer
	d := New(&buf, DefaultConfig())

	require.NoError(t, d.ProgramChange(63))
	assert.Equal(t, []byte{0xCF, 63}, buf.Bytes())
}

func TestIsP6Line(t *testing.T) {
	assert.True(t, isP6Line(" 3 [P6             ]: USB-Audio - P-6"))
	assert.True(t, isP6Line("                      Roland P-6 at usb-0000:c5:00.3-1, full speed"))
	assert.False(t, isP6Line(" 0 [Generic        ]: HDA-Intel - HD-Audio Generic"))
}

func TestClassifyOpenErr(t *testing.T) {
	const path = "/dev/snd/midiC3D0"

	busy := classifyOpenErr(path, &fs.PathError{Op: "open", Path: path, Err: syscall.EBUSY})
	assert.ErrorIs(t, busy, ErrBusy)
	assert.Contains(t, busy.Error(), path)

	perm := classifyOpenErr(path, &fs.PathError{Op: "open", Path: path, Err: syscall.EACCES})
	assert.ErrorIs(t, perm, ErrPermission)

	other := classifyOpenErr(path, errors.New("boom"))
	assert.NotErrorIs(t, other, ErrBusy)
	assert.NotErrorIs(t, other, ErrPermission)
	assert.Contains(t, other.Error(), "boom")
}
