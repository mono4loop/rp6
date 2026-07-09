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
// New packs get an Install button (download + install + load); already-installed
// packs get a Select button (load). Network/disk work runs off the UI thread;
// updates are marshaled back through fyne.Do. Available on desktop and mobile
// (the web build stubs it out — see pak_stub.go).
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

// storeEntryCard renders one catalog entry: cover image, name, metadata
// (author • license • version • size), description, and an action — an Install
// button, or (when the pak is already installed at installedDir) a Select button
// that loads it into the emulator.
func (u *ui) storeEntryCard(e samplepak.CatalogEntry, dlg dialog.Dialog, installedDir string) fyne.CanvasObject {
	title := widget.NewLabelWithStyle(e.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	metaLbl := widget.NewLabel(strings.Join(nonEmpty(e.Author, e.License, e.Version, humanSize(e.Size)), "  •  "))
	desc := widget.NewLabel(e.Description)
	desc.Wrapping = fyne.TextWrapWord

	var action *widget.Button
	if installedDir != "" {
		action = widget.NewButtonWithIcon("Select", theme.ConfirmIcon(), func() {
			if dlg != nil {
				dlg.Hide()
			}
			u.setEmuSamples(installedDir)
		})
	} else {
		action = widget.NewButtonWithIcon("Install", theme.DownloadIcon(), func() {
			u.installFromStore(e, dlg)
		})
	}
	info := container.NewVBox(title, metaLbl, desc, container.NewHBox(action, layout.NewSpacer()))

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

// installFromStore downloads the entry's pak, installs it into the samples
// directory, and switches the emulator to it. Network + disk work runs off the
// UI thread. The download is staged in the samples directory (same volume as the
// install, and writable on Android).
func (u *ui) installFromStore(e samplepak.CatalogEntry, dlg dialog.Dialog) {
	u.setStatus("downloading " + e.Name + "…")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		dir, err := paksSamplesDir()
		if err != nil {
			fyne.Do(func() { u.setStatus("couldn't locate samples dir: " + err.Error()) })
			return
		}
		tmp, err := samplepak.DownloadTemp(ctx, e.DownloadURL, dir)
		if err != nil {
			fyne.Do(func() { u.setStatus("download failed: " + err.Error()) })
			return
		}
		defer os.Remove(tmp)
		installed, m, err := samplepak.Install(tmp, dir)
		if err != nil {
			fyne.Do(func() { u.setStatus("install failed: " + err.Error()) })
			return
		}
		fyne.Do(func() {
			u.setStatus("installed pak: " + m.Name)
			if dlg != nil {
				dlg.Hide()
			}
			u.setEmuSamples(installed)
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
