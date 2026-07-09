//go:build !js && !android && !ios

// SQLite-backed store (desktop). modernc.org/sqlite's libc makes syscalls that
// Android's seccomp filter blocks (and there's no js/wasm build), so the browser
// (store_js.go) and mobile (store_mobile.go) targets use lighter file/localStorage
// backends with the same API. See store_common.go for the shared API doc/types.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)
)

// Store is a SQLite-backed, profile-scoped slot store.
type Store struct {
	db      *sql.DB
	profile string
}

// Table definitions (without CREATE, so they can be reused for both fresh
// creation and legacy migration). The primary key includes the profile so each
// profile has an independent set of slots/keys.
const (
	sequencesDef = `sequences (
		profile    TEXT    NOT NULL DEFAULT '',
		slot       INTEGER NOT NULL,
		name       TEXT    NOT NULL DEFAULT '',
		data       TEXT    NOT NULL,
		updated_at TEXT    NOT NULL,
		PRIMARY KEY (profile, slot)
	)`
	metaDef = `meta (
		profile TEXT NOT NULL,
		key     TEXT NOT NULL,
		value   TEXT NOT NULL,
		PRIMARY KEY (profile, key)
	)`
)

// dataDir returns the rp6 data directory: $XDG_DATA_HOME/rp6 (falling back to
// ~/.local/share/rp6).
func dataDir() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "rp6"), nil
}

// DefaultPath returns $XDG_DATA_HOME/rp6/rp6.db (falling back to
// ~/.local/share/rp6/rp6.db).
func DefaultPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "rp6.db"), nil
}

// SamplesDir returns $XDG_DATA_HOME/rp6/samples (falling back to
// ~/.local/share/rp6/samples), the base directory where installed sample paks
// (.rp6sp) live, each in its own subdirectory.
func SamplesDir() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "samples"), nil
}

// Open opens (creating if needed) the database at path, migrates any legacy
// schema, ensures the current schema, and scopes all operations to profile. An
// empty profile defaults to DefaultProfile.
func Open(path, profile string) (*Store, error) {
	if profile == "" {
		profile = DefaultProfile
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := migrateLegacy(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, profile: profile}, nil
}

// ensureSchema creates the current tables if they don't already exist.
func ensureSchema(db *sql.DB) error {
	for _, def := range []string{sequencesDef, metaDef} {
		if _, err := db.Exec("CREATE TABLE IF NOT EXISTS " + def); err != nil {
			return err
		}
	}
	return nil
}

// migrateLegacy upgrades pre-profile tables (keyed by slot/key alone) to the
// profile-scoped schema, assigning their existing rows to DefaultProfile. It is
// a no-op for fresh databases and for already-migrated ones.
func migrateLegacy(db *sql.DB) error {
	migrations := []struct {
		table string
		def   string
		copy  string
	}{
		{"sequences", sequencesDef,
			`INSERT INTO sequences(profile, slot, name, data, updated_at)
			 SELECT ?, slot, name, data, updated_at FROM sequences_old`},
		{"meta", metaDef,
			`INSERT INTO meta(profile, key, value) SELECT ?, key, value FROM meta_old`},
	}
	for _, m := range migrations {
		cols, err := tableColumns(db, m.table)
		if err != nil {
			return err
		}
		if len(cols) == 0 || slices.Contains(cols, "profile") {
			continue // fresh (created by ensureSchema) or already migrated
		}
		if err := migrateTable(db, m.table, m.def, m.copy); err != nil {
			return err
		}
	}
	return nil
}

// migrateTable renames the legacy table aside, creates the new one, copies the
// rows in (tagged with DefaultProfile), and drops the old table — atomically.
func migrateTable(db *sql.DB, table, def, copyStmt string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, s := range []string{
		"ALTER TABLE " + table + " RENAME TO " + table + "_old",
		"CREATE TABLE " + def,
	} {
		if _, err := tx.Exec(s); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(copyStmt, DefaultProfile); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec("DROP TABLE " + table + "_old"); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// tableColumns returns the column names of a table, or nil if it doesn't exist.
func tableColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

// Profile returns the profile this store is scoped to.
func (s *Store) Profile() string { return s.profile }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Save upserts a sequence blob into a slot within the current profile.
func (s *Store) Save(slot int, name string, data []byte) error {
	_, err := s.db.Exec(
		`INSERT INTO sequences(profile, slot, name, data, updated_at) VALUES(?, ?, ?, ?, ?)
		 ON CONFLICT(profile, slot) DO UPDATE SET name=excluded.name, data=excluded.data, updated_at=excluded.updated_at`,
		s.profile, slot, name, string(data), time.Now().UTC().Format(time.RFC3339))
	return err
}

// Load returns the name and data for a slot in the current profile; ok is false
// if the slot is empty.
func (s *Store) Load(slot int) (name string, data []byte, ok bool, err error) {
	var d string
	err = s.db.QueryRow(`SELECT name, data FROM sequences WHERE profile=? AND slot=?`, s.profile, slot).Scan(&name, &d)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	return name, []byte(d), true, nil
}

// List returns the occupied slots in the current profile, ordered by slot.
func (s *Store) List() ([]Entry, error) {
	rows, err := s.db.Query(`SELECT slot, name, updated_at FROM sequences WHERE profile=? ORDER BY slot`, s.profile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		var ts string
		if err := rows.Scan(&e.Slot, &e.Name, &ts); err != nil {
			return nil, err
		}
		e.Updated, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			return nil, fmt.Errorf("store: invalid updated_at %q for slot %d: %w", ts, e.Slot, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// InsertGap opens a free slot at `slot` by shifting every occupied slot in
// [slot, max] up by one (so an insert doesn't overwrite existing sequences).
// It shifts only the contiguous run of occupied slots starting at `slot`, up to
// the first free slot. Returns false without changes if slot..max are all
// occupied (no room); slots above max are never touched. Operates within the
// current profile only.
func (s *Store) InsertGap(slot, max int) (bool, error) {
	occupied, err := s.occupiedIn(slot, max)
	if err != nil {
		return false, err
	}

	if !occupied[slot] {
		return true, nil // already free, nothing to shift
	}
	gap := -1
	for i := slot; i <= max; i++ {
		if !occupied[i] {
			gap = i
			break
		}
	}
	if gap == -1 {
		return false, nil // full: no room to insert
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	// Move each occupied slot up by one, from the gap downward so the target is
	// always free (avoids a PRIMARY KEY conflict).
	for i := gap - 1; i >= slot; i-- {
		if _, err := tx.Exec(`UPDATE sequences SET slot=? WHERE profile=? AND slot=?`, i+1, s.profile, i); err != nil {
			_ = tx.Rollback()
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteSlot removes the sequence at `slot` and closes the gap by shifting the
// contiguous run of occupied slots above it (slot+1..max) down by one, so the
// numbered list stays compact. Slots above the first gap, and above max, are
// untouched. Operates within the current profile only.
func (s *Store) DeleteSlot(slot, max int) error {
	occupied, err := s.occupiedIn(slot, max)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sequences WHERE profile=? AND slot=?`, s.profile, slot); err != nil {
		_ = tx.Rollback()
		return err
	}
	// Pull the contiguous run above the deleted slot down by one.
	for i := slot + 1; i <= max && occupied[i]; i++ {
		if _, err := tx.Exec(`UPDATE sequences SET slot=? WHERE profile=? AND slot=?`, i-1, s.profile, i); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// occupiedIn returns the set of occupied slots in [lo, hi] for the current
// profile.
func (s *Store) occupiedIn(lo, hi int) (map[int]bool, error) {
	rows, err := s.db.Query(`SELECT slot FROM sequences WHERE profile=? AND slot BETWEEN ? AND ? ORDER BY slot`, s.profile, lo, hi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	occupied := map[int]bool{}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		occupied[n] = true
	}
	return occupied, rows.Err()
}

// SetMeta stores a small key/value string within the current profile.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO meta(profile, key, value) VALUES(?, ?, ?)
		 ON CONFLICT(profile, key) DO UPDATE SET value=excluded.value`, s.profile, key, value)
	return err
}

// Meta returns a stored value in the current profile; ok is false if absent.
func (s *Store) Meta(key string) (value string, ok bool, err error) {
	err = s.db.QueryRow(`SELECT value FROM meta WHERE profile=? AND key=?`, s.profile, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}
