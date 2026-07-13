package components

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"github.com/stretchr/testify/assert"
)

func TestInteractiveComponentsExposeAccessibility(t *testing.T) {
	accent := color.NRGBA{R: 0xe1, G: 0x87, B: 0x3b, A: 0xff}
	tests := []struct {
		name  string
		obj   fyne.Accessible
		label string
	}{
		{"pad", NewPad("A1", accent, nil), "A1"},
		{"toggle", NewRackToggle("PADS", accent, nil), "PADS"},
		{"cycle", NewRackCycle(nil, accent, nil), "Rack selector"},
		{"knob", NewKnob(KnobConfig{Label: "TEMPO", Value: 120, Min: 40, Max: 300}), "TEMPO 120"},
		{"transport", NewTransportToggle(nil), "Play"},
		{"step", NewStepButton(nil), "Sequencer step off"},
		{"device", NewDeviceBadge("EMULATOR", "SOFTWARE", accent), "EMULATOR SOFTWARE"},
		{"piano", NewPianoKeyboard(PianoConfig{}), "Piano keyboard"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.label, tt.obj.AccessibilityLabel())
			assert.Equal(t, fyne.AccessibleRoleButton, tt.obj.AccessibilityRole())
		})
	}
}

func TestRackPanelExposesContentForInspection(t *testing.T) {
	content := NewRackToggle("FX", color.NRGBA{}, nil)
	panel := NewRackPanel(content)
	assert.Equal(t, "Rack panel", panel.AccessibilityLabel())
	assert.Equal(t, fyne.AccessibleRoleContainer, panel.AccessibilityRole())
	assert.Equal(t, []fyne.CanvasObject{content}, panel.InspectionChildren())
}
