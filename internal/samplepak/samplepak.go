// Package samplepak reads, writes and installs .rp6sp sample paks: ZIP archives
// that carry a set of P-6-style pad samples (WAV or FLAC, laid out A1..H6 —
// see internal/emu) plus a manifest.json describing the pak. Paks are installed
// into a per-pak subdirectory of the rp6 samples directory
// (see internal/store.SamplesDir) so the emulator can load one like any other
// samples folder, and a future store can download and install them.
//
// This package is pure logic (no Fyne, no p6, no emu): it only manipulates ZIP
// archives and the filesystem, so it is fully unit-testable.
package samplepak

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Ext is the sample-pak file extension (including the dot).
const Ext = ".rp6sp"

// ManifestName is the manifest file's name at the archive/pak-dir root.
const ManifestName = "manifest.json"

// FormatVersion is the current manifest format version written by Create.
const FormatVersion = 1

// Manifest describes a sample pak. It is stored as manifest.json at the root of
// the archive (and of the installed directory).
type Manifest struct {
	// Format is the manifest schema version (FormatVersion).
	Format int `json:"format"`
	// ID is a stable, filesystem-safe identifier (e.g. "acme.modular-hits").
	// It is used as the installed directory name, so re-installing replaces the
	// same directory and the emulator's persistence profile stays stable.
	ID string `json:"id"`
	// Name is the human-readable pak name shown in the UI.
	Name string `json:"name"`
	// Version is the pak's own version string (free-form, e.g. "1.0.0").
	Version string `json:"version,omitempty"`
	// Author / Description / License are optional metadata for the store + UI.
	Author      string `json:"author,omitempty"`
	Description string `json:"description,omitempty"`
	License     string `json:"license,omitempty"`
	// Credits is optional long-form attribution text (shown in the info dialog).
	Credits string `json:"credits,omitempty"`
}

var (
	// ErrNoManifest is returned when a pak archive has no manifest.json.
	ErrNoManifest = errors.New("samplepak: no manifest.json in pak")
	// ErrNoSamples is returned when a pak archive contains no pad samples.
	ErrNoSamples = errors.New("samplepak: no pad samples (.wav/.flac) in pak")
	// ErrBadID is returned when a manifest ID is empty or unsafe.
	ErrBadID = errors.New("samplepak: invalid pak id")
)

// Installed pairs an installed pak's on-disk directory with its manifest.
type Installed struct {
	Dir      string
	Manifest Manifest
}

// validID reports whether id is a safe single path segment (no separators,
// no traversal, non-empty). IDs become directory names.
func validID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, `/\`) {
		return false
	}
	if strings.ContainsRune(id, os.PathSeparator) {
		return false
	}
	return true
}

// isSample reports whether a filename is a pad sample (WAV or FLAC).
func isSample(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".wav", ".flac":
		return true
	}
	return false
}

// ReadManifest opens the pak archive at archivePath and returns its parsed
// manifest, without extracting anything. Useful for listing/store previews.
func ReadManifest(archivePath string) (*Manifest, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("samplepak: opening %s: %w", archivePath, err)
	}
	defer zr.Close()
	return manifestFromZip(&zr.Reader)
}

// manifestFromZip reads and parses manifest.json from a zip reader.
func manifestFromZip(zr *zip.Reader) (*Manifest, error) {
	f, err := zr.Open(ManifestName)
	if err != nil {
		return nil, ErrNoManifest
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("samplepak: reading manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("samplepak: parsing manifest: %w", err)
	}
	if !validID(m.ID) {
		return nil, fmt.Errorf("%w: %q", ErrBadID, m.ID)
	}
	return &m, nil
}

// Install extracts the pak archive at archivePath into samplesDir/<manifest.ID>
// and returns the installed directory and manifest. An existing installation
// with the same ID is replaced. samplesDir is created if needed. The archive is
// validated first (well-formed manifest, at least one pad sample, no unsafe
// paths), so a failed install never leaves a partial directory in place.
func Install(archivePath, samplesDir string) (string, *Manifest, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", nil, fmt.Errorf("samplepak: opening %s: %w", archivePath, err)
	}
	defer zr.Close()

	m, err := manifestFromZip(&zr.Reader)
	if err != nil {
		return "", nil, err
	}
	if !hasSample(&zr.Reader) {
		return "", nil, ErrNoSamples
	}

	// Extract into a temporary staging dir next to the destination, then swap it
	// into place, so a partial/failed extract never clobbers a good install.
	if err := os.MkdirAll(samplesDir, 0o755); err != nil {
		return "", nil, err
	}
	dest := filepath.Join(samplesDir, m.ID)
	staging, err := os.MkdirTemp(samplesDir, ".stage-"+m.ID+"-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(staging) // no-op once renamed away

	for _, f := range zr.File {
		if err := extractFile(f, staging); err != nil {
			return "", nil, err
		}
	}

	// Swap: remove any prior install, then move staging into place.
	if err := os.RemoveAll(dest); err != nil {
		return "", nil, err
	}
	if err := os.Rename(staging, dest); err != nil {
		return "", nil, err
	}
	return dest, m, nil
}

// hasSample reports whether the archive contains at least one pad sample file.
func hasSample(zr *zip.Reader) bool {
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && isSample(f.Name) {
			return true
		}
	}
	return false
}

// extractFile writes one zip entry into destDir, guarding against zip-slip
// (entries that escape destDir via "../" or absolute paths).
func extractFile(f *zip.File, destDir string) error {
	if unsafePath(f.Name) {
		return fmt.Errorf("samplepak: unsafe path in pak: %q", f.Name)
	}
	name := path.Clean(f.Name)
	if name == "." {
		return nil
	}
	target := filepath.Join(destDir, filepath.FromSlash(name))
	if !within(destDir, target) {
		return fmt.Errorf("samplepak: unsafe path in pak: %q", f.Name)
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, rc); err != nil {
		return err
	}
	return out.Close()
}

// unsafePath reports whether a zip entry name is absolute or contains a ".."
// component (either of which could escape the extraction root).
func unsafePath(name string) bool {
	if name == "" {
		return true
	}
	if path.IsAbs(name) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return true
	}
	// Windows-style absolute (drive letter) or backslash separators.
	if len(name) >= 2 && name[1] == ':' {
		return true
	}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

// within reports whether target is inside base (defends against zip-slip).
func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// List returns the installed paks under samplesDir (each a subdirectory with a
// manifest.json), sorted by name. Directories without a valid manifest are
// skipped. A missing samplesDir yields an empty list.
func List(samplesDir string) ([]Installed, error) {
	entries, err := os.ReadDir(samplesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Installed
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(samplesDir, e.Name())
		m, err := readDirManifest(dir)
		if err != nil {
			continue // not a pak (or unreadable) — skip
		}
		out = append(out, Installed{Dir: dir, Manifest: *m})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.Name < out[j].Manifest.Name })
	return out, nil
}

// readDirManifest reads manifest.json from an installed pak directory.
func readDirManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if !validID(m.ID) {
		return nil, ErrBadID
	}
	return &m, nil
}

// Create builds a .rp6sp archive at outPath from the sample files in srcDir
// (recursively, WAV/FLAC only) plus the given manifest. The manifest's Format
// is set to FormatVersion. srcDir must contain at least one pad sample. A
// CREDITS.txt in srcDir, if present, is included.
func Create(srcDir, outPath string, m Manifest) error {
	if !validID(m.ID) {
		return fmt.Errorf("%w: %q", ErrBadID, m.ID)
	}
	samples, extras, err := collectFiles(srcDir)
	if err != nil {
		return err
	}
	if len(samples) == 0 {
		return ErrNoSamples
	}

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)

	m.Format = FormatVersion
	manifestJSON, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := writeZipEntry(zw, ManifestName, manifestJSON); err != nil {
		return err
	}
	for _, rel := range append(samples, extras...) {
		data, err := os.ReadFile(filepath.Join(srcDir, rel))
		if err != nil {
			return err
		}
		if err := writeZipEntry(zw, filepath.ToSlash(rel), data); err != nil {
			return err
		}
	}
	return zw.Close()
}

// collectFiles walks srcDir returning relative paths of sample files and of
// extra bundled files (currently CREDITS.txt), each sorted.
func collectFiles(srcDir string) (samples, extras []string, err error) {
	err = filepath.WalkDir(srcDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		switch {
		case isSample(rel):
			samples = append(samples, rel)
		case strings.EqualFold(d.Name(), "CREDITS.txt"):
			extras = append(extras, rel)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(samples)
	sort.Strings(extras)
	return samples, extras, nil
}

// writeZipEntry writes a single file entry into zw.
func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
