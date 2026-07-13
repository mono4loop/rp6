package main

import (
	"fmt"
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mono4loop/rp6/internal/sequencer"
	"github.com/mono4loop/rp6/internal/ui/components"
)

// defaultTracks is the number of sequencer tracks shown at startup.
const defaultTracks = 6

// sequencerRack is the software step-sequencer panel: a transport + track-count
// row, a second row of controls that act on the currently-armed track (mute +
// bar-length), then one block per active track — a pad-assign (arm) button and
// one full-width row of step cells per bar.
type sequencerRack struct {
	seq      *sequencer.Engine
	onLayout func()
	onDock   func(bool)
	onSlot   func(slot int) // load a saved-sequence slot
	onCopy   func(slot int) // duplicate the current sequence into slot
	onDelete func()         // delete the current sequence (Ctrl+Clear)
	onSave   func()         // save current sequence
	onStop   func()         // sequencer stopped (e.g. apply a queued slot change)

	docked     bool
	dockBtn    *components.RackToggle
	tracksStep *components.Knob
	slotStep   *components.Knob
	copyBtn    *components.RackToggle
	clearBtn   *components.RackToggle
	saveBtn    *components.RackToggle
	nameEntry  *widget.Entry
	playBtn    *components.TransportButton
	maxSlots   int

	// Second control row: mute + bar-length for the currently-armed track.
	armMuteBtn *components.RackToggle
	armBarsBtn *components.RackToggle

	trackBtns []*components.RackToggle
	blocks    []*fyne.Container          // per track (whole block, shown/hidden)
	barRows   [][]*fyne.Container        // [track][bar] rows of 16 cells
	cells     [][]*components.StepButton // [track][step]
	lastStep  []int                      // per-track last playhead step

	// Sub-section holders, exposed so the layout DSL can (re)compose the rack's
	// internals: the transport/knob row, the armed-track control row, and the
	// scrolling track grid (whose rows are generated in Go).
	header   *fyne.Container
	header2  *fyne.Container
	trackBox *container.Scroll

	armedTrack int // track waiting to adopt the next selected pad (-1 = none)

	obj fyne.CanvasObject
}

func newSequencerRack(seq *sequencer.Engine, onLayout func(), onDock func(bool), maxSlots int, onSlot func(int), onCopy func(int), onDelete func(), onSave func()) *sequencerRack {
	r := &sequencerRack{seq: seq, onLayout: onLayout, onDock: onDock, onSlot: onSlot, onCopy: onCopy, onDelete: onDelete, onSave: onSave, maxSlots: maxSlots, armedTrack: -1}
	maxT, maxB := seq.MaxTracks(), seq.MaxBars()
	spb := sequencer.StepsPerBar

	r.trackBtns = make([]*components.RackToggle, maxT)
	r.blocks = make([]*fyne.Container, maxT)
	r.barRows = make([][]*fyne.Container, maxT)
	r.cells = make([][]*components.StepButton, maxT)
	r.lastStep = make([]int, maxT)

	// Transport: a single Play/Stop toggle key whose icon is a pair of shoes
	// that walk while the sequence plays and stand still when stopped.
	r.playBtn = components.NewWalkerToggle(func(running bool) {
		if running {
			r.play()
		} else {
			r.stop()
		}
	})
	// Clear the steps (Ctrl+click deletes the whole sequence); flashes on use.
	r.clearBtn = components.NewRackToggleIcon(theme.DeleteIcon(), deviceHwAccent, func() {
		seq.Clear()
		r.refreshCells()
		r.clearBtn.Flash()
	})
	r.clearBtn.SetOnCtrlTap(func() {
		if r.onDelete != nil {
			r.onDelete()
		}
		r.clearBtn.Flash()
	})

	// Track count — rotary knob (same style as the toolbar tempo/pattern), its
	// indicator a stack of lanes lighting one per active track.
	r.tracksStep = components.NewKnob(components.KnobConfig{
		Label: "TRK", Value: defaultTracks, Min: 1, Max: maxT, Step: 1, Width: 112,
		Accent:    deviceHwAccent,
		Indicator: components.LanesIndicator{},
		Format:    strconv.Itoa,
		OnChange:  func(n int) { r.applyTracks(n); r.layoutChanged() },
	})

	// Sequence slot selector — rotary knob (e.g. "S03"), its indicator a 4×4
	// tile grid highlighting the active slot. The knob has no + so the duplicate
	// action lives on a separate copy button below.
	r.slotStep = components.NewKnob(components.KnobConfig{
		Label: "SEQ", Value: 1, Min: 1, Max: maxSlots, Step: 1, Width: 116,
		Accent:    deviceHwAccent,
		Indicator: components.GridIndicator{Cols: 4, Rows: 4},
		Format:    func(v int) string { return fmt.Sprintf("S%02d", v) },
		OnChange:  func(v int) { r.onSlot(v) },
	})
	// Duplicate the current sequence into the next slot (was Ctrl-click on +).
	r.copyBtn = components.NewRackToggleIcon(theme.ContentCopyIcon(), deviceHwAccent, func() {
		r.copyCurrent()
		r.copyBtn.Flash()
	})

	// Sequence name is edited on Save, not shown inline.
	r.nameEntry = widget.NewEntry()
	r.nameEntry.SetPlaceHolder("name")
	r.saveBtn = components.NewRackToggle("SAVE", deviceHwAccent, func() {
		if r.onSave != nil {
			r.onSave()
		}
		r.saveBtn.Flash()
	})

	r.dockBtn = components.NewRackToggleIcon(theme.ViewRestoreIcon(), deviceHwAccent, r.toggleDock)
	r.dockBtn.SetOn(r.docked)

	// Everything on one compact control row. Copy + delete sit side by side.
	header := container.NewHBox(
		r.playBtn, widget.NewSeparator(),
		r.tracksStep.Object(), widget.NewSeparator(),
		r.slotStep.Object(), r.copyBtn, r.clearBtn, r.saveBtn,
		widget.NewSeparator(), r.dockBtn)
	r.header = header

	// Second row: mute + bar-length for the armed track. Tap a track's name to
	// arm it (it lights hardest), then these act on it. Greyed when none armed.
	r.armMuteBtn = components.NewRackToggleIcon(theme.VolumeUpIcon(), deviceHwAccent, r.toggleArmedMute)
	r.armBarsBtn = components.NewRackToggle("BARS", deviceHwAccent, r.cycleArmedBars)
	header2 := container.NewHBox(
		container.NewGridWrap(fyne.NewSize(44, 34), r.armMuteBtn),
		container.NewGridWrap(fyne.NewSize(80, 34), r.armBarsBtn),
	)
	r.header2 = header2

	trackObjs := make([]fyne.CanvasObject, 0, maxT+1)
	for t := range maxT {
		track := t
		seq.SetPad(t, t) // default: tracks -> pads A1, A2, ...

		// Track header: a backlit rack toggle showing the pad label. Tapping it
		// *arms* the track (lights hardest): the second-row mute/bars controls
		// then act on it, and the next selected pad becomes its sample (after
		// which it disarms, so an accidental pad hit can't change it).
		// Touch-friendly: no modifier keys needed (works on web/Android).
		tb := components.NewRackToggle(padLabelForID(t), bankNRGBAForID(t), func() { r.armTrack(track) })
		tb.SetOn(!seq.Muted(t)) // lit as a label; goes dark while the track is muted
		r.trackBtns[t] = tb

		headerCol := container.New(&fixedWidthFillLayout{width: 64}, tb)

		r.cells[t] = make([]*components.StepButton, maxB*spb)
		r.barRows[t] = make([]*fyne.Container, maxB)
		acc := bankColorForID(seq.Pad(t))
		for b := range maxB {
			cellObjs := make([]fyne.CanvasObject, spb)
			for c := range spb {
				idx := b*spb + c
				tt, ss := track, idx
				cell := components.NewStepButton(nil)
				cell.SetAccent(acc)
				cell.OnToggle = func() { seq.SetStep(tt, ss, cell.On()) }
				r.cells[t][idx] = cell
				cellObjs[c] = cell
			}
			row := container.NewGridWithColumns(spb, cellObjs...)
			if b != 0 {
				row.Hide() // only the first bar visible until bars increased
			}
			r.barRows[t][b] = row
		}
		barsCol := container.New(&fillRowsLayout{}, toObjs(r.barRows[t])...)

		block := container.NewBorder(nil, nil, headerCol, nil, barsCol)
		r.blocks[t] = block
		r.lastStep[t] = -1
		trackObjs = append(trackObjs, block)
	}

	// Trailing spacer so the last track isn't flush against the rack's bottom
	// frame.
	bottomGap := canvas.NewRectangle(color.Transparent)
	bottomGap.SetMinSize(fyne.NewSize(0, 10))
	trackObjs = append(trackObjs, bottomGap)

	// The track rows live in a vertical scroller so they stay reachable when
	// there are more tracks/bars than fit the available height (e.g. many tracks
	// docked as a side column). The transport + armed-track control rows stay
	// pinned above it. A modest min height keeps a few tracks visible even when
	// the rack is stacked in an unbounded VBox.
	tracks := container.New(&fillRowsLayout{fixedLast: true}, trackObjs...)
	scroll := container.NewVScroll(tracks)
	scroll.SetMinSize(fyne.NewSize(tracks.MinSize().Width, 240))
	r.trackBox = scroll

	// The object is composed by the app (ui.composeRack): from the layout file's
	// `rack seq` block if present, else r.defaultObject — so the sub-widgets are
	// parented into exactly one tree (never a throwaway; matters on mobile).
	r.applyTracks(defaultTracks)
	r.updateArmedControls() // nothing armed yet -> greyed
	return r
}

// defaultObject builds the rack's stock Go composition — the transport/knob row
// and armed-track control row pinned above the scrolling track grid. Used only
// when the layout file has no `rack seq` block (see ui.composeRack).
func (r *sequencerRack) defaultObject() fyne.CanvasObject {
	// A little breathing room between the controls and the grid.
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(fyne.NewSize(0, 12))
	top := container.NewVBox(r.header, r.header2, gap)
	return components.NewRackPanel(container.NewBorder(top, nil, nil, nil, r.trackBox))
}

func toObjs(rows []*fyne.Container) []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, len(rows))
	for i, r := range rows {
		objs[i] = r
	}
	return objs
}

// fillRowsLayout preserves every visible child's minimum height, then shares
// any extra height evenly. This lets the sequencer's active tracks and bars grow
// into a tall rack instead of leaving a large dead area beneath 34px step rows;
// when their minimums exceed the viewport the enclosing Scroll still scrolls.
// fixedLast keeps the trailing breathing-room object at its small minimum size.
type fillRowsLayout struct{ fixedLast bool }

func (l *fillRowsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	visible := visibleObjects(objects)
	var size fyne.Size
	for _, object := range visible {
		min := object.MinSize()
		size.Width = fyne.Max(size.Width, min.Width)
		size.Height += min.Height
	}
	if len(visible) > 1 {
		size.Height += float32(len(visible)-1) * theme.Padding()
	}
	return size
}

func (l *fillRowsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	visible := visibleObjects(objects)
	if len(visible) == 0 {
		return
	}
	padding := theme.Padding()
	available := size.Height - float32(len(visible)-1)*padding
	for _, object := range visible {
		available -= object.MinSize().Height
	}
	if available < 0 {
		available = 0
	}
	grow := len(visible)
	if l.fixedLast && grow > 1 {
		grow--
	}
	extra := float32(0)
	if grow > 0 {
		extra = available / float32(grow)
	}
	y := float32(0)
	for i, object := range visible {
		height := object.MinSize().Height
		if !l.fixedLast || i < len(visible)-1 {
			height += extra
		}
		object.Move(fyne.NewPos(0, y))
		object.Resize(fyne.NewSize(size.Width, height))
		y += height + padding
	}
}

func visibleObjects(objects []fyne.CanvasObject) []fyne.CanvasObject {
	visible := make([]fyne.CanvasObject, 0, len(objects))
	for _, object := range objects {
		if object != nil && object.Visible() {
			visible = append(visible, object)
		}
	}
	return visible
}

// fixedWidthFillLayout gives a single control a stable minimum width while
// allowing it to fill the row height. Track assignment keys therefore grow with
// their step rows instead of remaining as small caps at the top of each track.
type fixedWidthFillLayout struct{ width float32 }

func (l *fixedWidthFillLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, object := range visibleObjects(objects) {
		size = size.Max(object.MinSize())
	}
	size.Width = fyne.Max(size.Width, l.width)
	return size
}

func (*fixedWidthFillLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, object := range visibleObjects(objects) {
		object.Move(fyne.Position{})
		object.Resize(size)
	}
}

// Object returns the CanvasObject to place in a layout.
func (r *sequencerRack) Object() fyne.CanvasObject { return r.obj }

func (r *sequencerRack) toggleDock() {
	r.docked = !r.docked
	r.dockBtn.SetOn(r.docked)
	if r.onDock != nil {
		r.onDock(r.docked)
	}
}

func (r *sequencerRack) layoutChanged() {
	if r.onLayout != nil {
		r.onLayout()
	}
}

// SetSlot/SeqName/SetSeqName let the app reflect the current saved slot.
func (r *sequencerRack) SetSlot(slot int)    { r.slotStep.SetValueSilent(slot) }
func (r *sequencerRack) SeqName() string     { return r.nameEntry.Text }
func (r *sequencerRack) SetSeqName(s string) { r.nameEntry.SetText(s) }

// setSlotPending flashes (or stops flashing) the SEQ knob border to signal a
// queued slot change waiting for the next bar.
func (r *sequencerRack) setSlotPending(on bool) { r.slotStep.SetPending(on) }

// copyCurrent duplicates the current sequence into the next slot (the separate
// copy button; the slot knob has no +/Ctrl alt like the old stepper did).
func (r *sequencerRack) copyCurrent() {
	if r.onCopy == nil {
		return
	}
	// No slot after the last one to copy into; copying onto the current slot
	// would be a silent no-op (or a misleading "no free slot" error), so bail.
	if r.slotStep.Value() >= r.maxSlots {
		return
	}
	r.onCopy(r.slotStep.Value() + 1)
}

// syncFromEngine refreshes all widgets to match the engine (after a load).
func (r *sequencerRack) syncFromEngine() {
	r.tracksStep.SetValueSilent(r.seq.Tracks())
	changed := r.applyTracks(r.seq.Tracks())
	for t := 0; t < r.seq.MaxTracks(); t++ {
		r.trackBtns[t].SetLabel(padLabelForID(r.seq.Pad(t)))
		r.trackBtns[t].SetAccent(bankNRGBAForID(r.seq.Pad(t)))
		r.trackBtns[t].SetOn(!r.seq.Muted(t)) // lit as a label; dark while muted
		r.setTrackAccent(t)
		if r.applyBars(t, r.seq.Bars(t)) {
			changed = true
		}
	}
	r.disarm() // loading a sequence clears the armed track
	r.refreshCells()
	// A full relayout is only needed when the set of visible tracks/bars changed;
	// the per-cell/-track widgets refresh themselves, and pad labels are fixed
	// width, so a same-shape load (the common pak switch) skips the costly
	// window-tree refresh.
	if changed {
		r.layoutChanged()
	}
}

func (r *sequencerRack) applyTracks(n int) bool {
	r.seq.SetTracks(n)
	n = r.seq.Tracks()
	changed := false
	for t, block := range r.blocks {
		want := t < n
		if block.Visible() == want {
			continue
		}
		changed = true
		if want {
			block.Show()
		} else {
			block.Hide()
		}
	}
	return changed
}

// cycleArmedBars advances the armed track's bar count (wrapping at maxBars).
func (r *sequencerRack) cycleArmedBars() {
	if r.armedTrack < 0 {
		return
	}
	t := r.armedTrack
	r.applyBars(t, r.seq.Bars(t)%r.seq.MaxBars()+1)
	r.updateArmedControls()
	r.layoutChanged()
}

func (r *sequencerRack) applyBars(track, n int) bool {
	r.seq.SetBars(track, n)
	n = r.seq.Bars(track)
	changed := false
	for b, row := range r.barRows[track] {
		want := b < n
		if row.Visible() == want {
			continue
		}
		changed = true
		if want {
			row.Show()
		} else {
			row.Hide()
		}
	}
	return changed
}

func (r *sequencerRack) play() {
	r.seq.Start()
	r.playBtn.SetRunning(true)
}

func (r *sequencerRack) stop() {
	r.seq.Stop()
	r.playBtn.SetRunning(false)
	r.clearPlayhead()
	if r.onStop != nil {
		r.onStop()
	}
}

// armTrack arms a track: the second-row mute/bars controls act on it and the
// next selected pad becomes its sample. Tapping the armed track again cancels;
// tapping a different track moves the arm. The armed track lights hardest.
func (r *sequencerRack) armTrack(track int) {
	if r.armedTrack == track {
		r.disarm()
		return
	}
	r.disarm()
	r.armedTrack = track
	r.trackBtns[track].SetArmed(true)
	r.updateArmedControls()
}

// disarm clears any armed track.
func (r *sequencerRack) disarm() {
	if r.armedTrack >= 0 {
		r.trackBtns[r.armedTrack].SetArmed(false)
		r.armedTrack = -1
	}
	r.updateArmedControls()
}

// PadSelected is called by the app whenever a pad is selected/tapped. If a track
// is armed, the pad becomes that track's sample and the track disarms (so a
// later accidental pad hit can't change it). Returns whether the selection was
// consumed as a track assignment.
func (r *sequencerRack) PadSelected(pad int) bool {
	if r.armedTrack < 0 {
		return false
	}
	track := r.armedTrack
	r.disarm()
	r.assign(track, pad)
	return true
}

func (r *sequencerRack) assign(track, pad int) {
	if pad < 0 {
		return
	}
	r.seq.SetPad(track, pad)
	r.trackBtns[track].SetLabel(padLabelForID(pad))
	r.trackBtns[track].SetAccent(bankNRGBAForID(pad))
	r.setTrackAccent(track)
}

// setTrackAccent tints a track's step cells with its pad's bank color.
func (r *sequencerRack) setTrackAccent(track int) {
	acc := bankColorForID(r.seq.Pad(track))
	for _, cell := range r.cells[track] {
		cell.SetAccent(acc)
	}
}

// toggleArmedMute mutes/unmutes the armed track (second-row mute control).
func (r *sequencerRack) toggleArmedMute() {
	if r.armedTrack < 0 {
		return
	}
	r.seq.ToggleMuted(r.armedTrack)
	r.trackBtns[r.armedTrack].SetOn(!r.seq.Muted(r.armedTrack)) // dark while muted
	r.updateArmedControls()
}

// updateArmedControls reflects the armed track's mute + bar-length on the
// second-row controls. When no track is armed both controls grey out and do
// nothing.
func (r *sequencerRack) updateArmedControls() {
	t := r.armedTrack
	if t < 0 {
		r.armMuteBtn.SetAccent(deviceHwAccent)
		r.armMuteBtn.SetIcon(theme.VolumeUpIcon())
		r.armMuteBtn.SetOn(false)
		r.armBarsBtn.SetAccent(deviceHwAccent)
		r.armBarsBtn.SetLabel("BARS")
		r.armBarsBtn.SetOn(false)
		return
	}
	acc := bankNRGBAForID(r.seq.Pad(t))

	muted := r.seq.Muted(t)
	if muted {
		r.armMuteBtn.SetIcon(theme.VolumeMuteIcon())
	} else {
		r.armMuteBtn.SetIcon(theme.VolumeUpIcon())
	}
	r.armMuteBtn.SetAccent(acc)
	r.armMuteBtn.SetOn(!muted) // lit = active, greyed = muted

	r.armBarsBtn.SetAccent(acc)
	r.armBarsBtn.SetLabel(barsLabel(r.seq.Bars(t)))
	r.armBarsBtn.SetOn(true)
}

// barsLabel formats a bar count for the arm control ("1 BAR", "2 BARS", …).
func barsLabel(n int) string {
	if n == 1 {
		return "1 BAR"
	}
	return strconv.Itoa(n) + " BARS"
}

// setPlayhead updates each active track's playhead for the given global tick
// (tracks of different lengths advance independently). Runs on the main thread.
func (r *sequencerRack) setPlayhead(tick int) {
	for t := 0; t < r.seq.Tracks(); t++ {
		step := tick % r.seq.TrackLen(t)
		if r.lastStep[t] >= 0 && r.lastStep[t] < len(r.cells[t]) {
			r.cells[t][r.lastStep[t]].SetPlaying(false)
		}
		r.cells[t][step].SetPlaying(true)
		r.lastStep[t] = step
	}
}

func (r *sequencerRack) clearPlayhead() {
	for t := range r.cells {
		if r.lastStep[t] >= 0 && r.lastStep[t] < len(r.cells[t]) {
			r.cells[t][r.lastStep[t]].SetPlaying(false)
		}
		r.lastStep[t] = -1
	}
}

func (r *sequencerRack) refreshCells() {
	for t := range r.cells {
		for s := range r.cells[t] {
			r.cells[t][s].SetOn(r.seq.Step(t, s))
		}
	}
}
