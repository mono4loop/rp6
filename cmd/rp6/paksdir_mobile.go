//go:build android || ios

package main

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
)

// paksSamplesDir is the mobile implementation of the sample-pak store's
// platform seam (see paksdir_desktop.go): it returns the base directory packs
// install into — a "samples" folder inside the app's private storage. That's a
// real filesystem path (unlike the SAF content:// trees the folder picker
// returns), so the emulator loads it with os.DirFS and samplepak extracts into
// it with ordinary os calls.
func paksSamplesDir() (string, error) {
	app := fyne.CurrentApp()
	if app == nil || app.Storage().RootURI() == nil {
		return "", fmt.Errorf("no writable app storage")
	}
	return filepath.Join(app.Storage().RootURI().Path(), "samples"), nil
}
