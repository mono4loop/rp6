package samplepak

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildBareZip writes a zip with the exact entry names given (no path cleaning),
// so tests can craft manifests-less or zip-slip archives.
func buildBareZip(t *testing.T, outPath string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(outPath)
	require.NoError(t, err)
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
}

// makePakDir builds a source directory with a few WAV pad files and a manifest,
// returning its path.
func makePakDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"A1.wav", "B2.flac", "H6.wav"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("fake-audio"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CREDITS.txt"), []byte("by me"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("ignored"), 0o644))
	return dir
}

func TestCreateReadInstallRoundTrip(t *testing.T) {
	src := makePakDir(t)
	out := filepath.Join(t.TempDir(), "test"+Ext)

	err := Create(src, out, Manifest{ID: "acme.kit", Name: "ACME Kit", Version: "1.0.0", Author: "me"})
	require.NoError(t, err)

	// ReadManifest works without extracting.
	m, err := ReadManifest(out)
	require.NoError(t, err)
	assert.Equal(t, "acme.kit", m.ID)
	assert.Equal(t, "ACME Kit", m.Name)
	assert.Equal(t, FormatVersion, m.Format)

	// Install extracts into samplesDir/<id>.
	samples := t.TempDir()
	dir, m2, err := Install(out, samples)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(samples, "acme.kit"), dir)
	assert.Equal(t, "acme.kit", m2.ID)

	// Sample files + manifest + credits present; the .md was not bundled.
	for _, name := range []string{"A1.wav", "B2.flac", "H6.wav", ManifestName, "CREDITS.txt"} {
		assert.FileExists(t, filepath.Join(dir, name))
	}
	assert.NoFileExists(t, filepath.Join(dir, "notes.md"))
}

func TestInstallReplacesExisting(t *testing.T) {
	src := makePakDir(t)
	out := filepath.Join(t.TempDir(), "p"+Ext)
	require.NoError(t, Create(src, out, Manifest{ID: "acme.kit", Name: "Kit"}))

	samples := t.TempDir()
	dir, _, err := Install(out, samples)
	require.NoError(t, err)
	// Drop a stale file into the install, then re-install; it must be gone.
	stale := filepath.Join(dir, "stale.wav")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o644))
	_, _, err = Install(out, samples)
	require.NoError(t, err)
	assert.NoFileExists(t, stale)
	assert.FileExists(t, filepath.Join(dir, "A1.wav"))
}

func TestList(t *testing.T) {
	samples := t.TempDir()
	// Two installed paks.
	for _, id := range []string{"acme.one", "acme.two"} {
		src := makePakDir(t)
		out := filepath.Join(t.TempDir(), id+Ext)
		require.NoError(t, Create(src, out, Manifest{ID: id, Name: id}))
		_, _, err := Install(out, samples)
		require.NoError(t, err)
	}
	// A non-pak directory (no manifest) is ignored.
	require.NoError(t, os.MkdirAll(filepath.Join(samples, "loose"), 0o755))

	list, err := List(samples)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "acme.one", list[0].Manifest.ID)
	assert.Equal(t, "acme.two", list[1].Manifest.ID)
}

func TestListMissingDir(t *testing.T) {
	list, err := List(filepath.Join(t.TempDir(), "does-not-exist"))
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestCreateNoSamples(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "readme.txt"), []byte("hi"), 0o644))
	err := Create(src, filepath.Join(t.TempDir(), "x"+Ext), Manifest{ID: "x", Name: "x"})
	assert.ErrorIs(t, err, ErrNoSamples)
}

func TestCreateBadID(t *testing.T) {
	src := makePakDir(t)
	for _, id := range []string{"", "..", "a/b", "a\\b"} {
		err := Create(src, filepath.Join(t.TempDir(), "x"+Ext), Manifest{ID: id, Name: "x"})
		assert.ErrorIs(t, err, ErrBadID, "id %q", id)
	}
}

func TestReadManifestMissing(t *testing.T) {
	// A zip without a manifest.
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "A1.wav"), []byte("a"), 0o644))
	// Build a zip manually via Create with a manifest, then a plain zip without.
	out := filepath.Join(t.TempDir(), "nomani"+Ext)
	buildBareZip(t, out, map[string]string{"A1.wav": "a"})
	_, err := ReadManifest(out)
	assert.ErrorIs(t, err, ErrNoManifest)
}

func TestInstallRejectsZipSlip(t *testing.T) {
	out := filepath.Join(t.TempDir(), "evil"+Ext)
	buildBareZip(t, out, map[string]string{
		ManifestName:    `{"format":1,"id":"evil","name":"Evil"}`,
		"A1.wav":        "a",
		"../escape.wav": "pwned",
	})
	_, _, err := Install(out, t.TempDir())
	assert.Error(t, err)
}
