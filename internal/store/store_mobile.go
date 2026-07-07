//go:build android || ios

// Mobile store: modernc.org/sqlite crashes under Android's seccomp filter, so on
// mobile sequences persist as a JSON file in the app's private storage instead.
// It mirrors the SQLite store's API and profile scoping exactly (see
// store_common.go). The caller supplies a writable path (the app's storage dir).
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type seqRow struct {
	Name    string `json:"name"`
	Data    string `json:"data"`
	Updated string `json:"updated"`
}

type profileData struct {
	Seqs map[int]seqRow    `json:"seqs"`
	Meta map[string]string `json:"meta"`
}

type dbData struct {
	Profiles map[string]*profileData `json:"profiles"`
}

// Store is a JSON-file-backed, profile-scoped slot store.
type Store struct {
	mu      sync.Mutex
	profile string
	path    string
	db      *dbData
}

// DefaultPath is a last-resort location; the app overrides Open's path with its
// private storage directory (there is no standard user data dir on mobile).
func DefaultPath() (string, error) {
	return filepath.Join(os.TempDir(), "rp6", "rp6.json"), nil
}

// Open loads (creating if needed) the JSON store at path and scopes operations
// to profile.
func Open(path, profile string) (*Store, error) {
	if profile == "" {
		profile = DefaultProfile
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	d := &dbData{Profiles: map[string]*profileData{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, d)
	}
	if d.Profiles == nil {
		d.Profiles = map[string]*profileData{}
	}
	return &Store{profile: profile, path: path, db: d}, nil
}

func (s *Store) flush() error {
	b, err := json.MarshalIndent(s.db, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) prof() *profileData {
	p := s.db.Profiles[s.profile]
	if p == nil {
		p = &profileData{}
		s.db.Profiles[s.profile] = p
	}
	if p.Seqs == nil {
		p.Seqs = map[int]seqRow{}
	}
	if p.Meta == nil {
		p.Meta = map[string]string{}
	}
	return p
}

// Profile returns the profile this store is scoped to.
func (s *Store) Profile() string { return s.profile }

// Close persists any pending changes.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flush()
}

// Save upserts a sequence blob into a slot within the current profile.
func (s *Store) Save(slot int, name string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prof().Seqs[slot] = seqRow{Name: name, Data: string(data), Updated: time.Now().UTC().Format(time.RFC3339)}
	return s.flush()
}

// Load returns the name and data for a slot; ok is false if the slot is empty.
func (s *Store) Load(slot int) (name string, data []byte, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.prof().Seqs[slot]
	if !ok {
		return "", nil, false, nil
	}
	return r.Name, []byte(r.Data), true, nil
}

// List returns the occupied slots in the current profile, ordered by slot.
func (s *Store) List() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.prof()
	slots := make([]int, 0, len(p.Seqs))
	for sl := range p.Seqs {
		slots = append(slots, sl)
	}
	sort.Ints(slots)
	out := make([]Entry, 0, len(slots))
	for _, sl := range slots {
		r := p.Seqs[sl]
		up, _ := time.Parse(time.RFC3339, r.Updated)
		out = append(out, Entry{Slot: sl, Name: r.Name, Updated: up})
	}
	return out, nil
}

// InsertGap opens a free slot at `slot` by shifting the contiguous run of
// occupied slots starting there up by one (mirrors the SQLite store).
func (s *Store) InsertGap(slot, max int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.prof()
	occupied := func(i int) bool { _, ok := p.Seqs[i]; return ok }
	if !occupied(slot) {
		return true, nil
	}
	gap := -1
	for i := slot; i <= max; i++ {
		if !occupied(i) {
			gap = i
			break
		}
	}
	if gap == -1 {
		return false, nil
	}
	for i := gap - 1; i >= slot; i-- {
		p.Seqs[i+1] = p.Seqs[i]
		delete(p.Seqs, i)
	}
	return true, s.flush()
}

// DeleteSlot removes the sequence at `slot` and pulls the contiguous run above
// it down by one to close the gap (mirrors the SQLite store).
func (s *Store) DeleteSlot(slot, max int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.prof()
	delete(p.Seqs, slot)
	occupied := func(i int) bool { _, ok := p.Seqs[i]; return ok }
	for i := slot + 1; i <= max && occupied(i); i++ {
		p.Seqs[i-1] = p.Seqs[i]
		delete(p.Seqs, i)
	}
	return s.flush()
}

// SetMeta stores a small key/value string within the current profile.
func (s *Store) SetMeta(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prof().Meta[key] = value
	return s.flush()
}

// Meta returns a stored value in the current profile; ok is false if absent.
func (s *Store) Meta(key string) (value string, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok = s.prof().Meta[key]
	return value, ok, nil
}
