package samplepak

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CatalogEntry is one downloadable pak advertised by a store catalog. The store
// UI shows Name/Description/Author/License plus the Cover image, and downloads
// DownloadURL when the user installs.
type CatalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	License     string `json:"license,omitempty"`
	Version     string `json:"version,omitempty"`
	// CoverURL and DownloadURL may be absolute or relative to the catalog URL;
	// FetchCatalog resolves them to absolute URLs before returning.
	CoverURL    string `json:"cover_url,omitempty"`
	DownloadURL string `json:"download_url"`
	// Size is the download size in bytes (0 if unknown), for display.
	Size int64 `json:"size,omitempty"`
}

// Catalog is the JSON document a store server returns: the list of paks it
// offers, plus an optional human-readable store name.
type Catalog struct {
	Name  string         `json:"name,omitempty"`
	Packs []CatalogEntry `json:"packs"`
}

// httpClient is the shared client for catalog/download requests.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// FetchCatalog GETs the store catalog at catalogURL and decodes it. Relative
// CoverURL/DownloadURL values are resolved against catalogURL so callers can use
// them directly.
func FetchCatalog(ctx context.Context, catalogURL string) (*Catalog, error) {
	base, err := url.Parse(catalogURL)
	if err != nil {
		return nil, fmt.Errorf("samplepak: bad catalog url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("samplepak: fetching catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("samplepak: catalog returned %s", resp.Status)
	}
	var cat Catalog
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&cat); err != nil {
		return nil, fmt.Errorf("samplepak: decoding catalog: %w", err)
	}
	for i := range cat.Packs {
		cat.Packs[i].CoverURL = resolveURL(base, cat.Packs[i].CoverURL)
		cat.Packs[i].DownloadURL = resolveURL(base, cat.Packs[i].DownloadURL)
	}
	return &cat, nil
}

// resolveURL resolves ref against base, returning ref unchanged if it's empty or
// unparseable.
func resolveURL(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

// FetchBytes GETs rawURL and returns the body (capped at 16 MiB), for cover
// images and other small assets.
func FetchBytes(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("samplepak: %s returned %s", rawURL, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// DownloadTemp downloads the pak at rawURL to a temporary .rp6sp file inside dir
// (or the OS temp dir when dir is empty) and returns its path. The caller is
// responsible for removing the file (typically after Install). The download is
// capped at 256 MiB. Passing the eventual install directory as dir keeps the
// download on the same (writable) volume — important on Android, where the OS
// temp dir may not be writable.
func DownloadTemp(ctx context.Context, rawURL, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("samplepak: downloading pak: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("samplepak: download returned %s", resp.Status)
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	f, err := os.CreateTemp(dir, "rp6pak-*"+Ext)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 256<<20))
	cerr := f.Close()
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if cerr != nil {
		os.Remove(f.Name())
		return "", cerr
	}
	return f.Name(), nil
}

// ReadCover returns the cover image bytes from a pak archive (per its manifest's
// Cover field) plus the image's lowercased extension (".png"/".jpg"). It returns
// ok=false when the pak declares no cover or the file is missing.
func ReadCover(archivePath string) (data []byte, ext string, ok bool, err error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, "", false, fmt.Errorf("samplepak: opening %s: %w", archivePath, err)
	}
	defer zr.Close()
	m, err := manifestFromZip(&zr.Reader)
	if err != nil {
		return nil, "", false, err
	}
	if m.Cover == "" {
		return nil, "", false, nil
	}
	f, err := zr.Open(m.Cover)
	if err != nil {
		return nil, "", false, nil // declared but missing — treat as no cover
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 16<<20))
	if err != nil {
		return nil, "", false, err
	}
	return b, strings.ToLower(filepath.Ext(m.Cover)), true, nil
}
