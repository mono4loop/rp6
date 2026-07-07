// Package store persists sequencer patterns (and small metadata) keyed by an
// integer slot within a named profile. It is device- and UI-agnostic: it stores
// opaque byte blobs, so callers marshal their own state (e.g. JSON) into it.
//
// Every store is scoped to a profile (a caller-chosen string, e.g. "p6" for the
// hardware or "emu:/path/to/samples" for an emulator kit). All operations are
// confined to their profile, so sequences made against one backend never appear
// under another.
//
// The default backend is a pure-Go SQLite database on disk (see store.go). On
// the js/wasm target — where there is no filesystem and modernc.org/sqlite is
// unavailable — a browser localStorage-backed store is used instead
// (store_js.go). Both expose the identical API below.
package store

import "time"

// DefaultProfile is used when Open is given an empty profile. It is also the
// profile a plain (no-emulator) run uses, and the one legacy sequences are
// migrated into.
const DefaultProfile = "p6"

// Entry describes a saved slot (without its data blob).
type Entry struct {
	Slot    int
	Name    string
	Updated time.Time
}
