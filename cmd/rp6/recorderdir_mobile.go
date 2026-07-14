//go:build android || ios

package main

import "path/filepath"

func recorderBaseDir() (string, bool) {
	path, ok := mobileStorePath()
	if !ok {
		return "", false
	}
	return filepath.Join(filepath.Dir(path), "recordings"), true
}
