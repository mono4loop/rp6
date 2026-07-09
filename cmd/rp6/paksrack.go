package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mono4loop/rp6/internal/ui/components"
)

// pakItem is one installed sample pak shown in the paks rack (name + its on-disk
// directory, which the emulator loads when the pak is selected).
type pakItem struct {
	ID, Name, Dir string
}

// paksRack is the "kit selector": a scrolling column of backlit keys, one per
// installed sample pak, with the loaded pak lit. Tapping a key loads that pak
// into the emulator (like the store's Select). A lit store key in the header
// opens the online sample-pak store to install more, and a filter field narrows
// the list by name for when many paks are installed. The list is populated from
// the paks installed via the store (see ui.installedPakItems).
type paksRack struct {
	lister   func() []pakItem // installed paks (rescanned each refresh)
	onSelect func(dir string) // load a pak into the emulator
	onStore  func()           // open the sample-pak store

	header  fyne.CanvasObject
	search  *widget.Entry
	listBox *fyne.Container
	scroll  *container.Scroll
	empty   *widget.Label
	obj     fyne.CanvasObject

	filter    string // current filter text (case-insensitive name substring)
	activeDir string // samples dir of the loaded pak ("" = none), kept across rebuilds
}

func newPaksRack(lister func() []pakItem, onSelect func(dir string), onStore func()) *paksRack {
	r := &paksRack{lister: lister, onSelect: onSelect, onStore: onStore}

	// Header: a "SAMPLE PAKS" caption with a lit store key on the right that
	// opens the store — kin to the pad rack's store toggle (same blue accent) —
	// above a filter field that narrows the list by name.
	title := widget.NewLabelWithStyle("SAMPLE PAKS", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	store := components.NewRackToggleIcon(theme.DownloadIcon(), storeAccent, r.onStore)
	store.SetOn(true)
	titleRow := container.NewBorder(nil, nil, nil, store, title)

	// The filter field is kept always visible (not hidden/shown), per the app's
	// focus/first-show guidance — see AGENTS.md's Fyne notes.
	r.search = widget.NewEntry()
	r.search.PlaceHolder = "Filter paks…"
	r.search.OnChanged = func(s string) {
		r.filter = s
		r.rebuild()
	}
	searchRow := container.NewBorder(nil, nil, widget.NewIcon(theme.SearchIcon()), nil, r.search)
	r.header = container.NewVBox(titleRow, searchRow)

	r.empty = widget.NewLabel("No sample paks yet.\nTap the store key to browse and install some.")
	r.empty.Wrapping = fyne.TextWrapWord

	r.listBox = container.NewVBox()
	sc := container.NewVScroll(r.listBox)
	sc.SetMinSize(fyne.NewSize(150, 150)) // usable height when stacked; grows to fill a rail
	r.scroll = sc
	return r
}

// Object returns the CanvasObject to place in a layout.
func (r *paksRack) Object() fyne.CanvasObject { return r.obj }

// refresh records which pak is loaded (activeDir; empty = none, e.g. the built-in
// kit or the P-6 backend) and rebuilds the key list. Call on the UI thread.
func (r *paksRack) refresh(activeDir string) {
	r.activeDir = activeDir
	r.rebuild()
}

// rebuild repopulates the key list from the installed paks, applying the current
// filter and lighting the loaded pak. Called on refresh and on every filter edit.
func (r *paksRack) rebuild() {
	if r.listBox == nil {
		return
	}
	items := r.lister()
	r.listBox.RemoveAll()
	if len(items) == 0 {
		r.listBox.Add(r.empty)
		r.listBox.Refresh()
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.filter))
	shown := 0
	for _, it := range items {
		if q != "" && !strings.Contains(strings.ToLower(it.Name), q) {
			continue
		}
		dir := it.Dir
		key := components.NewRackToggle(pakKeyLabel(it.Name), storeAccent, func() { r.onSelect(dir) })
		key.SetOn(r.activeDir != "" && dir == r.activeDir) // the loaded pak lights up
		r.listBox.Add(key)
		shown++
	}
	if shown == 0 { // paks installed, but none match the filter
		hint := widget.NewLabel("No paks match “" + strings.TrimSpace(r.filter) + "”.")
		hint.Wrapping = fyne.TextWrapWord
		r.listBox.Add(hint)
	}
	r.listBox.Refresh()
}

// defaultObject builds the rack's stock Go composition — used only when the
// layout file has no `rack paks` block (see ui.composeRack): the header on top,
// the scrolling pak list filling the rest.
func (r *paksRack) defaultObject() fyne.CanvasObject {
	return components.NewRackPanel(container.NewBorder(r.header, nil, nil, nil, r.scroll))
}

// pakKeyLabel trims a pak name to a single, key-friendly line.
func pakKeyLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "(unnamed)"
	}
	const max = 22
	if len([]rune(name)) > max {
		return string([]rune(name)[:max-1]) + "…"
	}
	return name
}
