//go:build js

// Browser (wasm) store: modernc.org/sqlite is unavailable on js/wasm and there
// is no filesystem, so sequences persist in the page's localStorage as a single
// JSON blob. It mirrors the SQLite store's API and profile scoping exactly (see
// store_common.go). If localStorage is unavailable the store still works for the
// session, just without persistence across reloads.
package store

import (
	"encoding/json"
	"sort"
	"syscall/js"
	"time"
)

// localStorageKey is the single key holding the whole (all-profiles) JSON blob.
const localStorageKey = "rp6.store"

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

// Store is a localStorage-backed, profile-scoped slot store.
type Store struct {
	profile string
	db      *dbData
	ls      js.Value // localStorage, or undefined when unavailable
}

// DefaultPath names the backing store (there is no real path in a browser).
func DefaultPath() (string, error) { return "browser localStorage", nil }

// Open loads the shared blob from localStorage and scopes operations to profile.
func Open(_, profile string) (*Store, error) {
	if profile == "" {
		profile = DefaultProfile
	}
	ls := js.Global().Get("localStorage")
	s := &Store{profile: profile, ls: ls, db: loadDB(ls)}
	return s, nil
}

func loadDB(ls js.Value) *dbData {
	d := &dbData{Profiles: map[string]*profileData{}}
	if ls.Truthy() {
		if v := ls.Call("getItem", localStorageKey); v.Type() == js.TypeString {
			_ = json.Unmarshal([]byte(v.String()), d)
		}
	}
	if d.Profiles == nil {
		d.Profiles = map[string]*profileData{}
	}
	return d
}

// flush writes the whole blob back to localStorage (no-op if unavailable).
func (s *Store) flush() error {
	if !s.ls.Truthy() {
		return nil
	}
	b, err := json.Marshal(s.db)
	if err != nil {
		return err
	}
	s.ls.Call("setItem", localStorageKey, string(b))
	return nil
}

// prof returns (creating if needed) the current profile's data.
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
func (s *Store) Close() error { return s.flush() }

// Save upserts a sequence blob into a slot within the current profile.
func (s *Store) Save(slot int, name string, data []byte) error {
	s.prof().Seqs[slot] = seqRow{Name: name, Data: string(data), Updated: time.Now().UTC().Format(time.RFC3339)}
	return s.flush()
}

// Load returns the name and data for a slot; ok is false if the slot is empty.
func (s *Store) Load(slot int) (name string, data []byte, ok bool, err error) {
	r, ok := s.prof().Seqs[slot]
	if !ok {
		return "", nil, false, nil
	}
	return r.Name, []byte(r.Data), true, nil
}

// List returns the occupied slots in the current profile, ordered by slot.
func (s *Store) List() ([]Entry, error) {
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
		return false, nil // full
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
	s.prof().Meta[key] = value
	return s.flush()
}

// Meta returns a stored value in the current profile; ok is false if absent.
func (s *Store) Meta(key string) (value string, ok bool, err error) {
	value, ok = s.prof().Meta[key]
	return value, ok, nil
}
