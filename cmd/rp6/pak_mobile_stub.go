//go:build android || ios

package main

import "fyne.io/fyne/v2"

// The sample-pak CLI and file-picker install are desktop-only (no argv on
// mobile, and the settings folder/file pickers are wired for the desktop). The
// in-app store (openSampleStore, store.go) IS available on mobile. These no-op
// stand-ins keep the desktop-only call sites in main.go valid on mobile builds.

func maybeRunPakCLI() {}

func (u *ui) installAndSelectPak(path string) {}

func (u *ui) emuSettingsExtra(onInstalled func()) []fyne.CanvasObject { return nil }
