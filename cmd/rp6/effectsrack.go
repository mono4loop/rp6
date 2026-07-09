package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/mono4loop/rp6/internal/effects"
	"github.com/mono4loop/rp6/internal/ui/components"
)

// rollSlot is the effect slot the rack exposes. Only the Roll effect exists, so
// the rack shows a single toggle bound to this slot plus the Rate knob.
const rollSlot = 0

// effectsRack is the control surface for the selected pad's effects: a single
// backlit Roll toggle plus a rotary Rate knob for the Roll rate.
type effectsRack struct {
	fx       *effects.Engine
	onChange func() // called after any edit (e.g. to refresh pad badges)

	padID int // -1 = none selected
	roll  *components.RackToggle
	rate  *components.Knob
	obj   fyne.CanvasObject
}

func newEffectsRack(fx *effects.Engine, onChange func()) *effectsRack {
	e := &effectsRack{fx: fx, onChange: onChange, padID: -1}

	e.roll = components.NewRackToggle(effects.KindRoll.Name(), deviceHwAccent, func() { e.cycleSlot(rollSlot) })

	e.rate = components.NewKnob(components.KnobConfig{
		Label: "RATE", Value: effects.DefaultDiv, Min: 0, Max: len(effects.Divisions) - 1, Step: 1,
		Accent:    deviceHwAccent,
		Indicator: components.BoltIndicator{}, // filling lightning bolt
		Format:    func(i int) string { return effects.Divisions[i].Name },
		OnChange: func(div int) {
			if e.padID >= 0 {
				e.fx.SetRollDiv(e.padID, div)
			}
		},
	})

	// The object is composed by the app (ui.composeRack): from the layout file's
	// `rack fx` block if present, else e.defaultObject — so the sub-widgets are
	// parented into exactly one tree (never a throwaway; matters on mobile).
	return e
}

// defaultObject builds the rack's stock Go composition — used only when the
// layout file has no `rack fx` block (see ui.composeRack).
func (e *effectsRack) defaultObject() fyne.CanvasObject {
	return components.NewRackPanel(container.NewHBox(e.roll, widget.NewSeparator(), e.rate.Object()))
}

// Object returns the CanvasObject to place in a layout.
func (e *effectsRack) Object() fyne.CanvasObject { return e.obj }

// show refreshes the rack to reflect pad id's effect state (id < 0 = none). The
// Roll toggle keeps a constant label and only changes its lit state.
func (e *effectsRack) show(id int) {
	e.padID = id
	if id < 0 {
		e.roll.SetAccent(deviceHwAccent)
		e.roll.SetOn(false)
		return
	}
	st := e.fx.State(id)
	e.roll.SetAccent(bankNRGBAForID(id))
	e.roll.SetOn(st.Slots[rollSlot] != effects.KindNone)
	e.rate.SetValueSilent(st.RollDiv)
}

func (e *effectsRack) cycleSlot(slot int) {
	if e.padID < 0 {
		return
	}
	cur := e.fx.State(e.padID).Slots[slot]
	next := effects.KindRoll
	if cur != effects.KindNone {
		next = effects.KindNone
	}
	e.fx.SetSlot(e.padID, slot, next)
	e.show(e.padID)
	if e.onChange != nil {
		e.onChange()
	}
}
