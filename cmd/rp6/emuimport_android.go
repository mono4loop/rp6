//go:build android

package main

import (
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/storage"
)

// maxImportDepth caps how deep the sample import recurses. The deepest supported
// layout is the P-6 export BANK_x/PAD_n/file.wav (3 levels below the root).
const maxImportDepth = 4

// resolveEmuSamples copies the WAV files from the picked folder into the app's
// private storage and returns that real directory, which the emulator can load
// with os.DirFS. On Android the folder picker returns a Storage Access Framework
// content:// tree (e.g. content://com.android.externalstorage.documents/tree/
// primary%3AMusic%2FSamples%2FKit) whose uri.Path() ("/tree/primary%3A…") is NOT
// a filesystem path — os.DirFS on it fails with "no such file or directory",
// which is why picking a folder used to turn the emulator off with no sound.
// Reading through the SAF grant (Fyne's storage repository → ContentResolver) is
// the only way in, so we copy the WAVs out into storage we can os.Open.
func (u *ui) resolveEmuSamples(uri fyne.ListableURI) (string, error) {
	app := fyne.CurrentApp()
	if app == nil || app.Storage().RootURI() == nil {
		return "", fmt.Errorf("no writable app storage")
	}
	dest := filepath.Join(app.Storage().RootURI().Path(), "emu-samples")
	_ = os.RemoveAll(dest) // replace any previous import
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	n, err := copyEmuTree(uri, dest, 0)
	log.Printf("rp6emu: imported %d wav(s) from %q -> %q (err=%v)", n, uri.String(), dest, err)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("no .wav files in the selected folder")
	}
	return dest, nil
}

// copyEmuTree recursively copies .wav files (preserving subdirectories) from a
// listable URI into dest, returning how many WAVs were copied.
func copyEmuTree(dir fyne.ListableURI, dest string, depth int) (int, error) {
	if depth > maxImportDepth {
		return 0, nil
	}
	children, err := dir.List()
	if err != nil {
		return 0, fmt.Errorf("listing %s: %w", dir.Name(), err)
	}
	count := 0
	for _, c := range children {
		name := leafName(c)
		if lister, err := storage.ListerForURI(c); err == nil { // a subdirectory
			sub := filepath.Join(dest, name)
			if err := os.MkdirAll(sub, 0o755); err != nil {
				return count, err
			}
			n, err := copyEmuTree(lister, sub, depth+1)
			if err != nil {
				return count, err
			}
			count += n
			continue
		}
		if !strings.EqualFold(filepath.Ext(name), ".wav") {
			continue
		}
		if err := copyEmuFile(c, filepath.Join(dest, name)); err != nil {
			return count, fmt.Errorf("copying %s: %w", name, err)
		}
		count++
	}
	return count, nil
}

// leafName returns the final path component of a URI's name. On Android the SAF
// URI.Name() is the full URL-encoded document id (e.g.
// "primary%3AMusic%2FSamples%2FKit%2FBANK_A"), not the leaf — so we decode it and
// take the part after the last '/' (or ':'), yielding "BANK_A"/"kick.wav". This
// is what emu's scanSamples matches on (BANK_x / PAD_n / A1.wav …).
func leafName(u fyne.URI) string {
	n := u.Name()
	if dec, err := url.QueryUnescape(n); err == nil {
		n = dec
	}
	n = strings.TrimRight(n, "/")
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:]
	} else if i := strings.LastIndex(n, ":"); i >= 0 {
		n = n[i+1:]
	}
	return n
}

func copyEmuFile(src fyne.URI, destPath string) error {
	r, err := storage.Reader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}
