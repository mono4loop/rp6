package mapped

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFullMap(t *testing.T) {
	src := `
// a comment
device "Synido TempoPAD C16" {
  match "tempopad", "c16"   /* inline block */
  channel 1

  note 36..83            -> pad.trigger  offset=36
  cc 97 when value=127   -> transport.play
  cc 9  abs              -> tempo.set
  cc 16 rel=twoscomp     -> tempo.delta
  cc 20 abs scale=10..120 -> delay.set
}
`
	m, err := Parse(src)
	require.NoError(t, err)
	assert.Equal(t, "Synido TempoPAD C16", m.Name)
	assert.Equal(t, []string{"tempopad", "c16"}, m.Match)
	assert.Equal(t, 1, m.Channel)
	require.Len(t, m.Bindings, 5)

	// note range -> pad.trigger with offset
	b := m.Bindings[0]
	assert.Equal(t, srcNote, b.Src.kind)
	assert.Equal(t, uint8(36), b.Src.num)
	assert.Equal(t, uint8(83), b.Src.hi)
	assert.Equal(t, "pad.trigger", b.Intent)
	assert.Equal(t, 36, b.Offset)

	// cc when value=
	assert.Equal(t, srcCC, m.Bindings[1].Src.kind)
	assert.Equal(t, whenEq, m.Bindings[1].When)
	assert.Equal(t, uint8(127), m.Bindings[1].WhenVal)

	// abs
	assert.True(t, m.Bindings[2].Abs)

	// rel
	assert.Equal(t, "twoscomp", m.Bindings[3].Rel)

	// abs with scale
	assert.True(t, m.Bindings[4].Abs)
	assert.Equal(t, uint8(10), m.Bindings[4].ScaleLo)
	assert.Equal(t, uint8(120), m.Bindings[4].ScaleHi)
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no device":        `match "x"`,
		"no match":         `device "X" {` + "\n}",
		"no arrow":         `device "X" {` + "\nmatch \"x\"\nnote 36 pad.trigger\n}",
		"unknown source":   `device "X" {` + "\nmatch \"x\"\nwiggle 1 -> tempo.set\n}",
		"bad note":         `device "X" {` + "\nmatch \"x\"\nnote 200 -> pad.trigger\n}",
		"inverted range":   `device "X" {` + "\nmatch \"x\"\nnote 80..40 -> pad.trigger\n}",
		"abs and rel":      `device "X" {` + "\nmatch \"x\"\ncc 1 abs rel=twoscomp -> tempo.set\n}",
		"bad rel":          `device "X" {` + "\nmatch \"x\"\ncc 1 rel=nope -> tempo.set\n}",
		"unterminated":     `device "X" {` + "\nmatch \"x\"",
		"bad channel":      `device "X" {` + "\nmatch \"x\"\nchannel 99\n}",
		"unknown modifier": `device "X" {` + "\nmatch \"x\"\ncc 1 wobble -> tempo.set\n}",
	}
	for name, src := range cases {
		if _, err := Parse(src); err == nil {
			t.Errorf("%s: expected a parse error", name)
		}
	}
}

func TestParseMinimal(t *testing.T) {
	m, err := Parse(`device "Mini" {` + "\n" + `match "mini"` + "\n" + `pc -> pattern.set` + "\n}")
	require.NoError(t, err)
	require.Len(t, m.Bindings, 1)
	assert.Equal(t, srcPC, m.Bindings[0].Src.kind)
	assert.Equal(t, "pattern.set", m.Bindings[0].Intent)
}
