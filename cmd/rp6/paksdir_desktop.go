//go:build !js && !android && !ios

package main

import "github.com/mono4loop/rp6/internal/store"

// paksSamplesDir is the platform seam for the sample-pak store: it returns the
// base directory packs install into, which the store logic (store.go) and the
// CLI (pak.go) treat opaquely. On the desktop that's the shared samples
// directory next to the sequence database. The mobile implementation lives in
// paksdir_mobile.go; the web build has no store (see pak_stub.go), so it needs
// no implementation here.
func paksSamplesDir() (string, error) { return store.SamplesDir() }
