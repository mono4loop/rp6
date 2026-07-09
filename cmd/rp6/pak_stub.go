//go:build js || android || ios

package main

import "fyne.io/fyne/v2"

// Sample-pak install/authoring is a desktop feature (it needs a real samples
// directory on the local filesystem). These no-op stand-ins keep the call sites
// in main.go valid on web/mobile builds.

func maybeRunPakCLI() {}

func (u *ui) installAndSelectPak(path string) {}

func (u *ui) emuSettingsExtra(onInstalled func()) []fyne.CanvasObject { return nil }
