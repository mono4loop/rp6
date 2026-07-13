//go:build !js && !android && !ios

package main

import (
	"path/filepath"

	"github.com/mono4loop/rp6/internal/store"
)

func recorderBaseDir() (string, bool) {
	path, err := store.DefaultPath()
	if err != nil {
		return "", false
	}
	return filepath.Join(filepath.Dir(path), "recordings"), true
}
