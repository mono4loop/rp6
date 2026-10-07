package main

import (
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
)

// autopilotScriptName mirrors the rule fade's script parser applies to bare
// target names: a letter, then letters, digits, '-' or '_'.
func autopilotScriptName(word string) bool {
	for i, r := range word {
		if !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r) && r != '-' && r != '_') {
			return false
		}
	}
	return word != ""
}

// Every inspection target must be reachable from an autopilot script: its ID,
// spelled with underscores for dots, is a valid script name that resolves
// back to the same object.
func TestAutopilotNamesResolveInspectionTargets(t *testing.T) {
	u, _ := newInspectionUI(t)
	for _, target := range u.inspectionTargets() {
		assert.NotContains(t, target.ID, "_", "inspection ID %q is ambiguous as a script name", target.ID)
		name := strings.ReplaceAll(target.ID, ".", "_")
		assert.True(t, autopilotScriptName(name), "%q is not a valid script name", name)
		assert.True(t, u.autopilotNamed(name) == target.Object, "%s does not resolve to %s", name, target.ID)
	}
	assert.Nil(t, u.autopilotNamed("no_such_target"))
}

func TestAutopilotIdleWaitsForPendingWork(t *testing.T) {
	u := newTestUI(t)
	assert.True(t, u.autopilotIdle())

	u.relayoutReq <- struct{}{}
	assert.False(t, u.autopilotIdle(), "a queued relayout is not idle")
	<-u.relayoutReq

	u.loadingSamples.Store(true)
	assert.False(t, u.autopilotIdle(), "a sample pak loading in the background is not idle")
	u.loadingSamples.Store(false)
	assert.True(t, u.autopilotIdle())
}
