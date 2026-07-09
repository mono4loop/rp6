package samplepak

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storeFixture spins up an in-process store: a catalog at "/" plus a pak
// download and a cover. It returns the server.
func storeFixture(t *testing.T) *httptest.Server {
	t.Helper()
	// Build a real pak (with a cover) to serve.
	src := t.TempDir()
	for _, n := range []string{"A1.wav", "B2.flac"} {
		require.NoError(t, os.WriteFile(filepath.Join(src, n), []byte("audio"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(src, "cover.png"), []byte("\x89PNGfake"), 0o644))
	pakPath := filepath.Join(t.TempDir(), "kit"+Ext)
	require.NoError(t, Create(src, pakPath, Manifest{ID: "acme.kit", Name: "ACME Kit", Description: "d", License: "CC0-1.0"}))

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Relative URLs, to exercise FetchCatalog's resolution.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Test","packs":[{"id":"acme.kit","name":"ACME Kit","description":"d","license":"CC0-1.0","cover_url":"cover/acme.kit","download_url":"paks/acme.kit"}]}`))
	})
	mux.HandleFunc("/paks/", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, pakPath) })
	mux.HandleFunc("/cover/", func(w http.ResponseWriter, r *http.Request) {
		data, _, ok, err := ReadCover(pakPath)
		require.NoError(t, err)
		require.True(t, ok)
		_, _ = w.Write(data)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchCatalogResolvesURLs(t *testing.T) {
	srv := storeFixture(t)
	cat, err := FetchCatalog(context.Background(), srv.URL+"/")
	require.NoError(t, err)
	assert.Equal(t, "Test", cat.Name)
	require.Len(t, cat.Packs, 1)
	e := cat.Packs[0]
	assert.Equal(t, "acme.kit", e.ID)
	// Relative URLs from the catalog are resolved to absolute.
	assert.Equal(t, srv.URL+"/cover/acme.kit", e.CoverURL)
	assert.Equal(t, srv.URL+"/paks/acme.kit", e.DownloadURL)
}

func TestDownloadAndInstallFromStore(t *testing.T) {
	srv := storeFixture(t)
	cat, err := FetchCatalog(context.Background(), srv.URL+"/")
	require.NoError(t, err)

	tmp, err := DownloadTemp(context.Background(), cat.Packs[0].DownloadURL, "")
	require.NoError(t, err)
	defer os.Remove(tmp)

	dir, m, err := Install(tmp, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "acme.kit", m.ID)
	assert.Equal(t, "cover.png", m.Cover)
	assert.FileExists(t, filepath.Join(dir, "cover.png"))
}

func TestFetchCoverBytes(t *testing.T) {
	srv := storeFixture(t)
	cat, err := FetchCatalog(context.Background(), srv.URL+"/")
	require.NoError(t, err)
	b, err := FetchBytes(context.Background(), cat.Packs[0].CoverURL)
	require.NoError(t, err)
	assert.NotEmpty(t, b)
}

func TestFetchCatalogError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := FetchCatalog(context.Background(), srv.URL+"/")
	assert.Error(t, err)
}
