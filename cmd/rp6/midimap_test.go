package main

import (
	"testing"

	"github.com/mono4loop/rp6/internal/midiin/mapped"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddedMapsValid guards that every shipped .midimap parses and targets
// only known intents — so a renamed intent or a typo in a map fails the build's
// test run rather than silently doing nothing at runtime.
func TestEmbeddedMapsValid(t *testing.T) {
	maps := loadMIDIMaps()
	require.NotEmpty(t, maps, "expected at least one embedded midimap")

	have := map[string]bool{}
	for _, m := range maps {
		assert.NoError(t, validateMap(m), "embedded map %q has an unknown intent", m.Name)
		have[m.Name] = true
	}
	// The controllers migrated from Go drivers + the C16 must all ship as maps.
	for _, name := range []string{
		"Synido TempoPAD C16",
		"Adafruit MacroPad RP2040",
		"Arturia KeyStep 37",
		"Arturia MicroLab",
	} {
		assert.True(t, have[name], "expected embedded midimap for %q", name)
	}
}

func TestValidateMapRejectsUnknownIntent(t *testing.T) {
	m, err := mapped.Parse(`device "Bad" {` + "\nmatch \"bad\"\ncc 1 abs -> not.a.real.intent\n}")
	require.NoError(t, err)
	assert.Error(t, validateMap(m))
}

func TestValidateMapAcceptsInternalIntent(t *testing.T) {
	m, err := mapped.Parse(`device "Ok" {` + "\nmatch \"ok\"\ncc 1 when value=127 -> input.bank.next\n}")
	require.NoError(t, err)
	assert.NoError(t, validateMap(m))
}
