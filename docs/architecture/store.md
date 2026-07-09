# Sample paks & the store

Developer notes on RP6's **sample paks** (`.rp6sp`) and the **sample-pak store**:
the on-disk/archive format, the store catalog protocol, how packs are created,
served, downloaded and installed, and where each piece lives.

If you're just *using* it: tap the blue **store** button in the pad rack to
browse and install packs; or run `rp6 pak create|install|list` from the CLI.

---

## 1. What it is

A **sample pak** is a self-contained kit of P-6-style pad samples plus metadata,
distributed as a single `.rp6sp` file. Packs can be:

- **created** from a folder of samples (`rp6 pak create`),
- **installed** locally (`rp6 pak install`, the emulator settings dialog, or a
  `-pak file.rp6sp` launch flag),
- **published** to a store and **installed over the network** from the in-app
  store browser.

Once installed, a pak is just a directory the emulator loads like any other
samples folder — so nothing in the audio path is pak-specific.

**Platform support.** The in-app **store** (browse + download + install) runs on
**desktop and mobile (Android/iOS)**; the **web (wasm)** build has no local
samples filesystem, so it stubs the store out. The **pak-authoring CLI**
(`rp6 pak …`), the `-pak` launch flag and the settings-dialog file-picker install
are **desktop-only** (no argv / desktop pickers). The one thing that differs per
platform is *where paks install* — abstracted behind a single seam,
`paksSamplesDir()` (see §7).

---

## 2. Design principles

Mirroring the rest of the codebase (see the repo's `AGENTS.md`):

- **The core is generic and pure.** `internal/samplepak` imports no Fyne and no
  `p6`. It only manipulates ZIP archives, JSON and the filesystem, plus a small
  stdlib `net/http` catalog client — so it's fully unit-testable (hermetic
  `httptest` for the client, `t.TempDir()` for install/create).
- **An installed pak is just a samples directory.** The emulator already loads
  a directory of `A1..H6` samples (`emu.Open`); installing a pak means extracting
  it into a per-pak directory and pointing the emulator there. No new audio code.
- **The store is a dumb catalog.** The server (or static host) returns a JSON
  list; the client downloads a `.rp6sp` and installs it with the same code path
  as a local install. No server-side accounts, sessions or state.

---

## 3. The `.rp6sp` format

A `.rp6sp` file is a **ZIP archive** containing a manifest, the pad samples, and
optional extras (credits, cover image):

```
mypak.rp6sp  (zip)
├── manifest.json          (required)
├── cover.png              (optional; PNG/JPEG)
├── CREDITS.txt            (optional; long-form attribution)
├── A1.wav  A2.flac  …  H6.wav      (pad samples, ≥1 required)
└── …
```

- The samples live at the archive **root** (not under a subdirectory), because
  the emulator's flat `A1.wav`..`H6.wav` scan reads the root. The per-bank
  (`A/1.wav`) and P-6 export (`BANK_A/PAD_1/*.wav`) layouts are also honored, as
  they are for a plain samples folder. Samples may be **WAV or FLAC** (FLAC is
  emulator-only; the P-6 hardware imports WAV).
- `manifest.json` and `CREDITS.txt`/`cover.png` are non-sample files the pad
  scanner simply ignores, so they coexist at the root.

### manifest.json

```json
{
  "format": 1,
  "id": "acme.modular-hits",
  "name": "Modular Hits",
  "version": "1.0.0",
  "author": "Someone",
  "description": "24 one-shot modular hits across banks A-D.",
  "license": "CC0-1.0",
  "credits": "Full attribution text…",
  "cover": "cover.png"
}
```

| field         | required | notes |
|---------------|----------|-------|
| `format`      | yes      | schema version (`samplepak.FormatVersion`, currently `1`) |
| `id`          | yes      | stable, filesystem-safe identifier; **becomes the install directory name** and the persistence-profile key |
| `name`        | yes      | display name |
| `version`     | no       | free-form pak version |
| `author`      | no       | shown in the store |
| `description` | no       | shown in the store |
| `license`     | no       | e.g. `CC0-1.0`; shown in the store |
| `credits`     | no       | long-form attribution |
| `cover`       | no       | filename (within the pak) of a cover image |

**`id` is load-bearing.** It's used verbatim as the installed directory name
(`<samples>/<id>/`), so re-installing the same id **replaces** that directory,
and the emulator's per-directory persistence profile (`emu:<abs-dir>`) stays
stable across re-installs. IDs are validated as a single safe path segment (no
separators, no `..`).

### Creating a pak

`samplepak.Create(srcDir, outPath, manifest)`:

- collects `*.wav`/`*.flac` (recursively), a `CREDITS.txt`, and a cover
  (`cover.png`/`.jpg`) if present;
- auto-fills `manifest.Cover` from the detected cover when unset;
- requires ≥1 sample and a valid `id`.

CLI: `rp6 pak create -id acme.kit -name "ACME Kit" -license CC0-1.0 -o kit.rp6sp ./samples`.

---

## 4. Installing

`samplepak.Install(archivePath, samplesDir)`:

1. validates the manifest and that the archive has ≥1 sample;
2. extracts into a **staging** temp dir under `samplesDir`, guarding every entry
   against zip-slip (absolute paths / `..` are rejected);
3. atomically swaps it into `samplesDir/<id>` (removing any prior install first),
   so a failed/partial extract never clobbers a good install.

`samplesDir` comes from `paksSamplesDir()`, the **platform seam** (§7): on the
desktop it's `store.SamplesDir()` → `$XDG_DATA_HOME/rp6/samples` (a sibling of
the sequence database); on mobile it's a `samples/` folder in the app's private
storage. Downloads are staged in that same directory so the install rename stays
on one (writable) volume — important on Android.

`samplepak.List(samplesDir)` enumerates installed packs (dirs with a valid
`manifest.json`), used by the store to mark packs already on disk.

After extracting, the app calls `setEmuSamples(dir)` to switch the emulator to
the pak: it re-scopes persistence to the pak's `emu:<dir>` profile and reloads
that profile's sequence. So each pak keeps its own sequences.

---

## 5. The store catalog

The store client fetches a **catalog JSON** from a base URL and lists the packs.
The catalog is intentionally simple so it can be served by a real server *or* a
plain static file host.

### Catalog JSON

```json
{
  "name": "Example Store",
  "packs": [
    {
      "id": "acme.kit",
      "name": "ACME Kit",
      "description": "…",
      "author": "…",
      "license": "CC0-1.0",
      "version": "1.0.0",
      "cover_url": "covers/acme.kit.png",
      "download_url": "paks/acme-kit.rp6sp",
      "size": 1916621
    }
  ]
}
```

- `download_url` is required; `cover_url` is optional (packs without one show a
  placeholder).
- **`cover_url` and `download_url` may be relative or absolute.** Relative URLs
  are resolved against the catalog URL, so a flat static layout works: put the
  `.rp6sp` files, cover images and `catalog.json` in one directory and use
  filenames as the URLs. They can also point at a CDN / other host.
- `size` (bytes) is optional, shown for information.

### Client (`internal/samplepak/catalog.go`)

- `FetchCatalog(ctx, catalogURL)` — GET + decode, then resolve every entry's
  `CoverURL`/`DownloadURL` to absolute against `catalogURL`.
- `FetchBytes(ctx, url)` — cover images (capped).
- `DownloadTemp(ctx, url)` — download a `.rp6sp` to a temp file (capped); the
  caller installs it then removes the temp.

All requests use a shared `http.Client` with a timeout; body sizes are capped to
avoid unbounded reads.

---

## 6. Serving a store

The catalog is deliberately static-host friendly. Serve it with **any file
host** (e.g. Caddy `file_server`): author a `catalog.json`, place it next to the
`.rp6sp` files and cover images, and serve the catalog at the URL RP6 queries.
Because `cover_url`/`download_url` resolve relative to the catalog URL, a single
flat directory works — e.g.:

```
/srv/store/
├── catalog.json
├── mykit.rp6sp
└── mykit.png
```

```bash
RP6_STORE_URL=https://store.example.com/ rp6
```

Any server that returns the catalog JSON and serves the pak/cover files over
HTTP works; there's nothing RP6-specific on the server side.

---

## 7. App wiring

- **Pad-rack store button** (`cmd/rp6/main.go`, `buildPadRack`): a
  `components.RackToggle` with a blue accent (`storeAccent`), just left of the
  device badge. Its tap calls `u.openSampleStore()`.
- **Store dialog + install** (`cmd/rp6/store.go`, `!js` — desktop **and**
  mobile): fetches the catalog off the UI thread, lists each entry (cover, name,
  metadata, description) and shows either **Install** (download + install, which
  then flips that entry's button to **Select**) or, for packs already on disk,
  **Select** (load that installed pak). The store stays open across installs, so
  several packs can be installed in a row. All network/disk work runs in
  goroutines; UI updates go through `fyne.Do`.
- **The platform seam** is `paksSamplesDir() (string, error)` — the *only*
  platform-specific piece. `store.go` and the CLI use it opaquely:
  - `cmd/rp6/paksdir_desktop.go` (`!js && !android && !ios`) → `store.SamplesDir()`.
  - `cmd/rp6/paksdir_mobile.go` (`android || ios`) → a `samples/` dir under the
    app's private storage (`app.Storage().RootURI().Path()`), a real filesystem
    path the emulator/`samplepak` use with ordinary `os` calls (unlike the SAF
    `content://` trees the folder picker returns).
- **CLI + authoring** (`cmd/rp6/pak.go`, desktop only): `pak create/install/list`,
  the `-pak` launch flag, and the emu-settings file-picker install.
- **Stubs**: `cmd/rp6/pak_stub.go` (`js`) no-ops everything incl. the store;
  `cmd/rp6/pak_mobile_stub.go` (`android || ios`) no-ops only the desktop-only
  bits (`maybeRunPakCLI`, `installAndSelectPak`, `emuSettingsExtra`) — the store
  itself is real on mobile.
- **Store URL**: a built-in default, overridable with the **`RP6_STORE_URL`**
  environment variable (`storeURL()` in `store.go`).

---

## 8. Packages

```
internal/samplepak/          pure logic (NO Fyne, NO p6) — ZIP + JSON + http client
  samplepak.go               Manifest, Create, Install (zip-slip-guarded, atomic), List, ReadManifest
  catalog.go                 Catalog/CatalogEntry, FetchCatalog (URL resolution), DownloadTemp, FetchBytes, ReadCover
  *_test.go                  create/install/list/zip-slip; httptest-based catalog + download tests
cmd/rp6/store.go             (desktop + mobile) the store dialog: fetch catalog, list, download+install/select
cmd/rp6/paksdir_desktop.go   (desktop) paksSamplesDir seam -> store.SamplesDir()
cmd/rp6/paksdir_mobile.go    (android/ios) paksSamplesDir seam -> app private storage
cmd/rp6/pak.go               (desktop) CLI (pak create/install/list), -pak flag, emu-settings install button
cmd/rp6/pak_stub.go          (web) no-op store + authoring
cmd/rp6/pak_mobile_stub.go   (android/ios) no-op authoring only (store is real)
internal/store/              SamplesDir() lives here (alongside the sequence DB path)
```

---

## 9. Configuration & paths

| variable         | meaning |
|------------------|---------|
| `RP6_STORE_URL`  | store catalog URL; falls back to the built-in default |
| `RP6_EMU_SAMPLES`| samples dir for `-emu` (unrelated to the store, but the same emulator loader) |

- Installed packs: `$XDG_DATA_HOME/rp6/samples/<id>/` (fallback
  `~/.local/share/rp6/samples/<id>/`).
- Sequence DB (per-pak profiles): `$XDG_DATA_HOME/rp6/rp6.db`, profile
  `emu:<abs-samples-dir>` (see `docs/architecture` / the store notes in AGENTS.md).

---

## 10. Testing

All tests run without hardware or network access to the real store:

- `internal/samplepak` (plain `go test`): create → read → install round-trip,
  re-install replaces, `List`, bad-id and no-samples errors, missing manifest,
  and a **zip-slip** rejection. The catalog client is tested against an
  in-process `httptest` server (catalog fetch + relative-URL resolution,
  download+install, cover bytes, error status) — no external store.
- The `.rp6sp` install path is exercised end-to-end (download from the httptest
  server → `Install` → assert files + cover on disk).

---

## 11. Ideas / not built

- **Signatures / checksums** for downloads (verify integrity/authenticity).
- **Update awareness**: compare installed `version` vs. catalog to offer updates
  (today an already-installed id shows **Select**, not **Update**).
- **Search / categories / paging** in the catalog for larger stores.
- **Mobile/web install**: the store runs on Android/iOS (installing into
  app-private storage); the **web (wasm)** build still lacks a local samples
  filesystem, so it stays stubbed. Mobile pak *authoring* (the CLI) is also still
  desktop-only.
- **Uninstall** from the store/settings UI (today: delete the pak's directory).
