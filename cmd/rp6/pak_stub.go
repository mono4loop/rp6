//go:build js

package main

import "fyne.io/fyne/v2"

// Sample-pak install/authoring and the store need a local filesystem samples
// directory, which the web (wasm) build doesn't have. These no-op stand-ins keep
// the call sites in main.go valid on the web build.

func maybeRunPakCLI() {}

func (u *ui) installAndSelectPak(path string) {}

func (u *ui) emuSettingsExtra(onInstalled func()) []fyne.CanvasObject { return nil }

// openSampleStore reports that the store isn't available on the web build.
func (u *ui) openSampleStore() { u.setStatus("the sample-pak store needs the desktop or mobile app") }
