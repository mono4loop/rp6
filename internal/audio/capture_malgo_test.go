//go:build capture

package audio

import (
	"encoding/binary"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// f32le encodes float32 values as the little-endian byte buffer miniaudio hands
// to the capture callback.
func f32le(vals ...float32) []byte {
	b := make([]byte, len(vals)*4)
	for i, v := range vals {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	return b
}

// TestMalgoCapturerDecodesAndReusesBuffer checks onData decodes the LE float32
// bytes correctly and reuses a single scratch buffer across callbacks instead
// of allocating a fresh slice every time (g5vm).
func TestMalgoCapturerDecodesAndReusesBuffer(t *testing.T) {
	c := &malgoCapturer{}
	var got []float32
	var ptr uintptr
	c.fn = func(s []float32) {
		got = append(got[:0], s...) // consumer copies out; must not retain s
		ptr = reflect.ValueOf(s).Pointer()
	}

	c.onData(nil, f32le(0.5, -0.25), 1)
	assert.Equal(t, []float32{0.5, -0.25}, got)
	first := ptr

	c.onData(nil, f32le(0.1, 0.2), 1)
	assert.Equal(t, []float32{0.1, 0.2}, got)
	assert.Equal(t, first, ptr, "capture buffer should be reused across callbacks, not reallocated")
}
