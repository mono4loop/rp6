//go:build !js

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mono4loop/rp6/internal/samplepak"
)

// defaultStoreURL is the sample-pak store the app queries when RP6_STORE_URL is
// unset — the public RP6 sample store.
const defaultStoreURL = "https://rp6-store.rbel.co/"

// storeURL returns the configured store catalog URL (RP6_STORE_URL) or the
// built-in default.
func storeURL() string {
	if v := strings.TrimSpace(os.Getenv("RP6_STORE_URL")); v != "" {
		return v
	}
	return defaultStoreURL
}

// openSampleStore opens the sample-pak store: it fetches the catalog from
// storeURL() and lists each pak with its cover, name, description and license.
// New packs get an Install button (download + install; the button then flips to
// Select); already-installed packs get a Select button (load). The store stays
// open across installs. Network/disk work runs off the UI thread; updates are
// marshaled back through fyne.Do. Available on desktop and mobile (the web build
// stubs it out — see pak_stub.go).
func (u *ui) openSampleStore() {
	list := container.NewVBox(widget.NewLabel("Loading catalog…"))
	scroll := container.NewVScroll(list)
	scroll.SetMinSize(fyne.NewSize(470, 430))
	dlg := dialog.NewCustom("Sample Pak Store", "Close", scroll, u.win)
	dlg.Resize(fyne.NewSize(540, 540))
	dlg.Show()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cat, err := samplepak.FetchCatalog(ctx, storeURL())
		installed := installedPaks() // id -> on-disk dir for paks already installed
		fyne.Do(func() {
			list.Objects = nil
			switch {
			case err != nil:
				msg := widget.NewLabel("Couldn't reach the store at " + storeURL() + "\n\n" + err.Error())
				msg.Wrapping = fyne.TextWrapWord
				list.Add(msg)
			case len(cat.Packs) == 0:
				list.Add(widget.NewLabel("No sample paks available."))
			default:
				for _, e := range cat.Packs {
					list.Add(u.storeEntryCard(e, dlg, installed[e.ID]))
				}
			}
			list.Refresh()
		})
	}()
}

// installedPaks maps each installed pak's ID to its on-disk directory (empty on
// any error).
func installedPaks() map[string]string {
	dirs := map[string]string{}
	dir, err := paksSamplesDir()
	if err != nil {
		return dirs
	}
	list, err := samplepak.List(dir)
	if err != nil {
		return dirs
	}
	for _, in := range list {
		dirs[in.Manifest.ID] = in.Dir
	}
	return dirs
}

// installedPakItems lists the installed sample paks for the paks rack (name +
// directory), sorted by name. Empty on any error or when none are installed.
func (u *ui) installedPakItems() []pakItem {
	dir, err := paksSamplesDir()
	if err != nil {
		return nil
	}
	list, err := samplepak.List(dir)
	if err != nil {
		return nil
	}
	items := make([]pakItem, 0, len(list))
	for _, in := range list {
		items = append(items, pakItem{ID: in.Manifest.ID, Name: in.Manifest.Name, Dir: in.Dir})
	}
	return items
}

// storeEntryCard renders one catalog entry: cover image, name, metadata
// (author • license • version • size), description, and an action button that
// toggles between Install (download + install, which then flips the button to
// Select) and Select (load the installed pak). The store stays open across
// installs so several packs can be installed in a row.
func (u *ui) storeEntryCard(e samplepak.CatalogEntry, dlg dialog.Dialog, installedDir string) fyne.CanvasObject {
	title := widget.NewLabelWithStyle(e.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	metaLbl := widget.NewLabel(strings.Join(nonEmpty(e.Author, e.License, e.Version, humanSize(e.Size)), "  •  "))
	desc := widget.NewLabel(e.Description)
	desc.Wrapping = fyne.TextWrapWord

	actionRow := container.NewHBox()
	var setAction func(installed string)
	setAction = func(installed string) {
		var b *widget.Button
		if installed != "" {
			b = widget.NewButtonWithIcon("Select", theme.ConfirmIcon(), func() {
				if dlg != nil {
					dlg.Hide()
				}
				u.setEmuSamples(installed)
			})
		} else {
			b = widget.NewButtonWithIcon("Install", theme.DownloadIcon(), func() {
				b.SetText("Installing…")
				b.Disable()
				u.installFromStore(e, func(dir string) {
					if dir == "" { // failed — restore the Install button
						b.SetText("Install")
						b.Enable()
						return
					}
					setAction(dir) // installed — becomes Select
				})
			})
		}
		actionRow.Objects = []fyne.CanvasObject{b, layout.NewSpacer()}
		actionRow.Refresh()
	}
	setAction(installedDir)
	info := container.NewVBox(title, metaLbl, desc, actionRow)

	cover := canvas.NewImageFromResource(theme.FileImageIcon())
	cover.FillMode = canvas.ImageFillContain
	coverBox := container.NewGridWrap(fyne.NewSize(84, 84), cover)
	if e.CoverURL != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if b, err := samplepak.FetchBytes(ctx, e.CoverURL); err == nil && len(b) > 0 {
				fyne.Do(func() {
					cover.Resource = fyne.NewStaticResource("cover-"+e.ID, b)
					cover.Refresh()
				})
			}
		}()
	}
	return container.NewVBox(
		container.NewBorder(nil, nil, coverBox, nil, info),
		widget.NewSeparator(),
	)
}

// installFromStore downloads the entry's pak and installs it into the samples
// directory, without closing the store or loading it (the user presses Select to
// load). onDone is called on the UI thread with the installed directory on
// success, or "" on failure. Network + disk work runs off the UI thread; the
// download is staged in the samples directory (same volume as the install, and
// writable on Android).
func (u *ui) installFromStore(e samplepak.CatalogEntry, onDone func(installedDir string)) {
	u.setStatus("downloading " + e.Name + "…")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		fail := func(msg string) {
			fyne.Do(func() {
				u.setStatus(msg)
				onDone("")
			})
		}
		dir, err := paksSamplesDir()
		if err != nil {
			fail("couldn't locate samples dir: " + err.Error())
			return
		}
		tmp, err := samplepak.DownloadTemp(ctx, e.DownloadURL, dir)
		if err != nil {
			fail("download failed: " + err.Error())
			return
		}
		defer os.Remove(tmp)
		installed, m, err := samplepak.Install(tmp, dir)
		if err != nil {
			fail("install failed: " + err.Error())
			return
		}
		fyne.Do(func() {
			u.setStatus("installed pak: " + m.Name + " — press Select to load it")
			u.refreshPaksRack() // the new pak now appears in the paks rack
			onDone(installed)
		})
	}()
}

// nonEmpty returns the non-empty strings among its arguments (for joining
// metadata lines without stray separators).
func nonEmpty(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

// humanSize formats a byte count for display, or "" when unknown (0).
func humanSize(n int64) string {
	if n <= 0 {
		return ""
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
