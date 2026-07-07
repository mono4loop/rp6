//go:build !android

package main

import "fyne.io/fyne/v2"

// resolveEmuSamples turns a folder picked in the settings dialog into a real
// filesystem directory the emulator can load with os.DirFS. On the desktop the
// picked URI is already a local directory. (Android returns a Storage Access
// Framework content:// tree that isn't a filesystem path — see
// emuimport_android.go.)
func (u *ui) resolveEmuSamples(uri fyne.ListableURI) (string, error) {
	return uri.Path(), nil
}
