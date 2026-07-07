package components

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSegText(t *testing.T) {
	assert.Equal(t, "  0", segText(0, 3))
	assert.Equal(t, "  7", segText(7, 3))
	assert.Equal(t, " 42", segText(42, 3))
	assert.Equal(t, "127", segText(127, 3))
	assert.Equal(t, "000", segText(1000, 3)) // keeps least-significant digits
}

func TestSetValueUpdatesState(t *testing.T) {
	s := NewSevenSeg(3)
	s.SetValue(64)
	assert.Equal(t, 64, s.Value())
}

func TestSegmentsForKnownDigits(t *testing.T) {
	// "8" lights all seven segments; "1" lights only b and c.
	assert.Equal(t, [7]bool{true, true, true, true, true, true, true}, segmentsFor['8'])
	assert.Equal(t, [7]bool{false, true, true, false, false, false, false}, segmentsFor['1'])
	assert.Equal(t, [7]bool{false, false, false, false, false, false, false}, segmentsFor[' '])
}
