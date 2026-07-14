package main

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mono4loop/rp6/internal/audiofx"
	"github.com/mono4loop/rp6/internal/recorder"
	"github.com/mono4loop/rp6/internal/ui/components"
)

var recorderAccent = color.NRGBA{R: 0xE8, G: 0x45, B: 0x45, A: 0xFF}

// defaultRecorderTracks is how many recorder track rows the rack shows out of the
// engine's recorder.TrackCount capacity when a layout variant doesn't override it
// with a `rec(tracks: N)` property. Kept low so the looper stays compact by
// default; roomy variants (the console) raise it via the layout.
const defaultRecorderTracks = 4

// recorderRack is the host audio recorder (up to recorder.TrackCount tracks; the
// rack shows a configurable subset, defaultRecorderTracks by default). Track rows
// carry live actions; the selected track's mixer/effect macros share one compact
// strip.
type recorderRack struct {
	rec      *recorder.Engine
	onStatus func(string)
	onExport func(track int)
	selected int
	// visibleTracks is how many track rows are shown (<= recorder.TrackCount); set
	// by SetTrackCount from the layout `rec(tracks: N)` property (default
	// defaultRecorderTracks). The engine keeps its full capacity regardless.
	visibleTracks int

	playAll *components.TransportButton
	quant   *components.Knob
	export  *components.RackToggle

	selectBtns []*components.RackToggle
	recordBtns []*components.RackToggle
	playBtns   []*components.RackToggle
	muteBtns   []*components.RackToggle
	soloBtns   []*components.RackToggle
	loopBtns   []*components.RackToggle
	durations  []*widget.Label
	rows       []fyne.CanvasObject // per-track row containers, shown/hidden by SetTrackCount

	level  *components.Knob
	pan    *components.Knob
	tone   *components.Knob
	comp   *components.Knob
	chorus *components.Knob
	delay  *components.Knob
	reverb *components.Knob

	header   *fyne.Container
	controls *container.Scroll
	trackBox *container.Scroll
	obj      fyne.CanvasObject
}

func newRecorderRack(rec *recorder.Engine, onStatus func(string), onExport func(int)) *recorderRack {
	r := &recorderRack{rec: rec, onStatus: onStatus, onExport: onExport}
	r.selectBtns = make([]*components.RackToggle, recorder.TrackCount)
	r.recordBtns = make([]*components.RackToggle, recorder.TrackCount)
	r.playBtns = make([]*components.RackToggle, recorder.TrackCount)
	r.muteBtns = make([]*components.RackToggle, recorder.TrackCount)
	r.soloBtns = make([]*components.RackToggle, recorder.TrackCount)
	r.loopBtns = make([]*components.RackToggle, recorder.TrackCount)
	r.durations = make([]*widget.Label, recorder.TrackCount)

	r.playAll = components.NewTransportToggle(func(running bool) {
		if running {
			rec.PlayAll()
			r.status("all tracks queued")
		} else {
			rec.StopAll()
			r.status("all tracks stopping")
		}
		r.syncAll()
	})
	r.quant = components.NewKnob(components.KnobConfig{
		Label: "QUANT", Value: int(rec.Quantization()), Min: 0, Max: 2, Step: 1, Width: 116,
		Accent: deviceHwAccent,
		Format: func(v int) string {
			return [...]string{"OFF", "BEAT", "BAR"}[v]
		},
		OnChange: func(v int) {
			rec.SetQuantization(recorder.Quantization(v))
			r.status("recorder quantization " + [...]string{"off", "beat", "bar"}[v])
		},
	})
	r.export = components.NewRackToggle("EXPORT", deviceHwAccent, func() {
		if r.onExport != nil {
			r.onExport(r.selected)
		}
	})
	r.header = container.NewHBox(r.playAll, widget.NewSeparator(), r.quant.Object(), r.export)

	r.level = r.knob("LEVEL", 0, 100, func(v int, s *audiofx.Settings) { _ = rec.SetLevel(r.selected, float32(v)/100) })
	r.pan = r.knob("PAN", -100, 100, func(v int, s *audiofx.Settings) { _ = rec.SetPan(r.selected, float32(v)/100) })
	r.tone = r.knob("TONE", -100, 100, func(v int, s *audiofx.Settings) { s.Tone = float32(v) / 100 })
	r.comp = r.knob("COMP", 0, 100, func(v int, s *audiofx.Settings) { s.Comp = float32(v) / 100 })
	r.chorus = r.knob("CHORUS", 0, 100, func(v int, s *audiofx.Settings) { s.Chorus = float32(v) / 100 })
	r.delay = r.knob("DELAY", 0, 100, func(v int, s *audiofx.Settings) { s.Delay = float32(v) / 100 })
	r.reverb = r.knob("REVERB", 0, 100, func(v int, s *audiofx.Settings) { s.Reverb = float32(v) / 100 })
	controlRow := container.NewHBox(
		r.level.Object(), r.pan.Object(), widget.NewSeparator(), r.tone.Object(), r.comp.Object(),
		r.chorus.Object(), r.delay.Object(), r.reverb.Object())
	r.controls = container.NewHScroll(controlRow)
	r.controls.SetMinSize(fyne.NewSize(0, controlRow.MinSize().Height))

	r.rows = make([]fyne.CanvasObject, recorder.TrackCount)
	for i := range recorder.TrackCount {
		track := i
		r.selectBtns[i] = components.NewRackToggle(fmt.Sprintf("T%d", i+1), deviceHwAccent, func() { r.selectTrack(track) })
		r.recordBtns[i] = components.NewRackToggleIcon(theme.MediaRecordIcon(), recorderAccent, func() { r.toggleRecord(track) })
		r.playBtns[i] = components.NewRackToggleIcon(theme.MediaPlayIcon(), deviceHwAccent, func() { r.togglePlay(track) })
		r.muteBtns[i] = components.NewRackToggle("M", deviceHwAccent, func() {
			_ = rec.SetMuted(track, !rec.Muted(track))
			r.syncTrack(track)
		})
		r.soloBtns[i] = components.NewRackToggle("S", deviceHwAccent, func() {
			_ = rec.SetSolo(track, !rec.Solo(track))
			r.syncAll()
		})
		r.loopBtns[i] = components.NewRackToggle("LOOP", deviceHwAccent, func() {
			_ = rec.SetLoop(track, !rec.Loop(track))
			r.syncTrack(track)
		})
		r.durations[i] = widget.NewLabel("EMPTY")
		r.rows[i] = container.NewHBox(
			container.NewGridWrap(fyne.NewSize(54, 34), r.selectBtns[i]),
			container.NewGridWrap(fyne.NewSize(42, 34), r.recordBtns[i]),
			container.NewGridWrap(fyne.NewSize(42, 34), r.playBtns[i]),
			container.NewGridWrap(fyne.NewSize(42, 34), r.muteBtns[i]),
			container.NewGridWrap(fyne.NewSize(42, 34), r.soloBtns[i]),
			container.NewGridWrap(fyne.NewSize(68, 34), r.loopBtns[i]),
			r.durations[i],
		)
	}
	r.trackBox = container.NewVScroll(container.NewVBox(r.rows...))
	r.selectTrack(0)
	r.SetTrackCount(defaultRecorderTracks) // compact by default; a variant may raise it
	r.syncAll()
	return r
}

// SetTrackCount sets how many track rows the rack shows (clamped to
// 1..recorder.TrackCount). The recorder engine keeps its full capacity; this only
// governs the visible rows, so a compact variant shows fewer and a roomy one
// more. Used by the layout `rec(tracks: N)` property (variant-entry only, via
// applyDefaultRecorderTracks) and the constructor's default. The track scroller's
// minimum height follows the visible rows (capped) so a small count doesn't leave
// an empty scroll area, while a large count still expands to fill a tall pane.
func (r *recorderRack) SetTrackCount(n int) {
	n = max(1, min(n, recorder.TrackCount))
	r.visibleTracks = n
	for i, row := range r.rows {
		if row == nil {
			continue
		}
		if want := i < n; row.Visible() != want {
			if want {
				row.Show()
			} else {
				row.Hide()
			}
		}
	}
	// Keep the selection (which drives the mixer strip) on a visible track.
	if r.selected >= n {
		r.selectTrack(n - 1)
	}
	if r.trackBox != nil {
		// Size the scroller to the visible rows, capped so eight tracks still
		// scroll within a reasonable height (a taller pane expands past the min).
		h := min(r.trackBox.Content.MinSize().Height, 210)
		r.trackBox.SetMinSize(fyne.NewSize(340, h))
	}
}

func (r *recorderRack) knob(label string, min, max int, update func(int, *audiofx.Settings)) *components.Knob {
	return components.NewKnob(components.KnobConfig{
		Label: label, Min: min, Max: max, Step: 5, Width: 104, Accent: deviceHwAccent,
		Format: func(v int) string {
			if min < 0 && v > 0 {
				return fmt.Sprintf("+%d", v)
			}
			return fmt.Sprintf("%d", v)
		},
		OnChange: func(v int) {
			settings := r.rec.Effects(r.selected)
			update(v, &settings)
			_ = r.rec.SetEffects(r.selected, settings)
		},
	})
}

func (r *recorderRack) status(s string) {
	if r.onStatus != nil {
		r.onStatus(s)
	}
}

func (r *recorderRack) Object() fyne.CanvasObject { return r.obj }

func (r *recorderRack) defaultObject() fyne.CanvasObject {
	return components.NewRackPanel(container.NewBorder(
		container.NewVBox(r.header, r.controls), nil, nil, nil, r.trackBox))
}

func (r *recorderRack) selectTrack(track int) {
	if track < 0 || track >= recorder.TrackCount {
		return
	}
	r.selected = track
	for i, btn := range r.selectBtns {
		btn.SetArmed(i == track)
	}
	r.syncControls()
}

func (r *recorderRack) toggleRecord(track int) {
	switch {
	case r.rec.RecordingTrack() == track || r.rec.RecordPendingTrack() == track:
		r.rec.StopRecording()
	case r.rec.ArmedTrack() == track:
		r.rec.CancelRecording()
	default:
		if err := r.rec.ArmRecord(track); err != nil {
			r.status(err.Error())
		} else {
			r.status(fmt.Sprintf("track %d armed: press an RP6 pad", track+1))
		}
	}
	r.syncAll()
}

func (r *recorderRack) togglePlay(track int) {
	if r.rec.Playing(track) || r.rec.PlayPending(track) {
		_ = r.rec.Stop(track)
	} else if r.rec.HasClip(track) {
		_ = r.rec.Play(track)
	} else {
		r.status(fmt.Sprintf("track %d is empty", track+1))
	}
	r.syncTrack(track)
}

func (r *recorderRack) syncAll() {
	anyPlaying := false
	for i := range recorder.TrackCount {
		r.syncTrack(i)
		anyPlaying = anyPlaying || r.rec.Playing(i) || r.rec.PlayPending(i)
	}
	r.playAll.SetRunning(anyPlaying)
	r.quant.SetValueSilent(int(r.rec.Quantization()))
	if r.selectBtns != nil {
		r.syncControls()
	}
}

func (r *recorderRack) syncTrack(track int) {
	if track < 0 || track >= len(r.playBtns) || r.playBtns[track] == nil {
		return
	}
	recording := r.rec.RecordingTrack() == track
	recordPending := r.rec.RecordPendingTrack() == track
	armed := r.rec.ArmedTrack() == track
	r.recordBtns[track].SetArmed(recording || recordPending || armed)
	r.recordBtns[track].SetOn(recording)
	playing := r.rec.Playing(track) || r.rec.PlayPending(track)
	r.playBtns[track].SetOn(playing)
	if playing {
		r.playBtns[track].SetIcon(theme.MediaStopIcon())
	} else {
		r.playBtns[track].SetIcon(theme.MediaPlayIcon())
	}
	r.muteBtns[track].SetOn(r.rec.Muted(track))
	r.soloBtns[track].SetOn(r.rec.Solo(track))
	r.loopBtns[track].SetOn(r.rec.Loop(track))
	frames := r.rec.DurationFrames(track)
	if frames == 0 {
		r.durations[track].SetText("EMPTY")
	} else {
		d := time.Duration(float64(frames) / float64(r.rec.SampleRate()) * float64(time.Second))
		label := fmt.Sprintf("%.1fs", d.Seconds())
		if r.rec.Truncated(track) {
			label += " MAX"
		}
		r.durations[track].SetText(label)
	}
}

func (r *recorderRack) syncControls() {
	track := r.selected
	r.level.SetValueSilent(int(r.rec.Level(track) * 100))
	r.pan.SetValueSilent(int(r.rec.Pan(track) * 100))
	settings := r.rec.Effects(track)
	r.tone.SetValueSilent(int(settings.Tone * 100))
	r.comp.SetValueSilent(int(settings.Comp * 100))
	r.chorus.SetValueSilent(int(settings.Chorus * 100))
	r.delay.SetValueSilent(int(settings.Delay * 100))
	r.reverb.SetValueSilent(int(settings.Reverb * 100))
}
