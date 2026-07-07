package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreSaveLoadListMeta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rp6.db")
	s, err := Open(path, "test")
	require.NoError(t, err)

	// Empty slot.
	_, _, ok, err := s.Load(1)
	require.NoError(t, err)
	assert.False(t, ok)

	// Save + load.
	require.NoError(t, s.Save(1, "beat", []byte(`{"v":1}`)))
	name, data, ok, err := s.Load(1)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "beat", name)
	assert.Equal(t, `{"v":1}`, string(data))

	// Upsert overwrites.
	require.NoError(t, s.Save(1, "beat2", []byte(`{"v":2}`)))
	name, data, _, _ = s.Load(1)
	assert.Equal(t, "beat2", name)
	assert.Equal(t, `{"v":2}`, string(data))

	// List.
	require.NoError(t, s.Save(3, "third", []byte("x")))
	list, err := s.List()
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, 1, list[0].Slot)
	assert.Equal(t, 3, list[1].Slot)

	// Meta.
	_, ok, err = s.Meta("last")
	require.NoError(t, err)
	assert.False(t, ok)
	require.NoError(t, s.SetMeta("last", "3"))
	v, ok, err := s.Meta("last")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "3", v)

	require.NoError(t, s.Close())

	// Reopen: data persists.
	s2, err := Open(path, "test")
	require.NoError(t, err)
	defer s2.Close()
	name, _, ok, _ = s2.Load(1)
	assert.True(t, ok)
	assert.Equal(t, "beat2", name)
	v, _, _ = s2.Meta("last")
	assert.Equal(t, "3", v)
}

func TestInsertGap(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "rp6.db"), "test")
	require.NoError(t, err)
	defer s.Close()

	nameAt := func(slot int) string {
		n, _, ok, err := s.Load(slot)
		require.NoError(t, err)
		if !ok {
			return ""
		}
		return n
	}

	// Occupy a contiguous run 1,2,3 and a separate slot 5.
	require.NoError(t, s.Save(1, "one", []byte("1")))
	require.NoError(t, s.Save(2, "two", []byte("2")))
	require.NoError(t, s.Save(3, "three", []byte("3")))
	require.NoError(t, s.Save(5, "five", []byte("5")))

	// Insert at 2: the contiguous run 2,3 shifts to 3,4; slot 5 is untouched
	// (the shift stops at the first gap, slot 4).
	ok, err := s.InsertGap(2, 16)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "one", nameAt(1))
	assert.Equal(t, "", nameAt(2), "slot 2 is now free")
	assert.Equal(t, "two", nameAt(3))
	assert.Equal(t, "three", nameAt(4))
	assert.Equal(t, "five", nameAt(5))

	// Inserting at an already-free slot is a no-op success.
	ok, err = s.InsertGap(2, 16)
	require.NoError(t, err)
	assert.True(t, ok)

	// A full range has no room: returns false, no changes.
	for i := 1; i <= 4; i++ {
		require.NoError(t, s.Save(i, "x", []byte("x")))
	}
	ok, err = s.InsertGap(1, 5) // slots 1..5 all occupied
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "five", nameAt(5), "no shift when full")
}

func TestDeleteSlot(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "rp6.db"), "test")
	require.NoError(t, err)
	defer s.Close()

	nameAt := func(slot int) string {
		n, _, ok, _ := s.Load(slot)
		if !ok {
			return ""
		}
		return n
	}

	// Contiguous run 1,2,3 and a separate slot 5.
	require.NoError(t, s.Save(1, "one", []byte("1")))
	require.NoError(t, s.Save(2, "two", []byte("2")))
	require.NoError(t, s.Save(3, "three", []byte("3")))
	require.NoError(t, s.Save(5, "five", []byte("5")))

	// Delete slot 2: 3 shifts down into 2; the gap at 4 stops the shift, so 5
	// stays put.
	require.NoError(t, s.DeleteSlot(2, 16))
	assert.Equal(t, "one", nameAt(1))
	assert.Equal(t, "three", nameAt(2), "slot 3 shifted down to 2")
	assert.Equal(t, "", nameAt(3), "slot 3 now free")
	assert.Equal(t, "five", nameAt(5), "slot past the gap untouched")

	// Deleting an empty slot pulls the next contiguous run down.
	require.NoError(t, s.DeleteSlot(4, 16)) // 4 empty, 5 occupied -> 5 moves to 4
	assert.Equal(t, "five", nameAt(4))
	assert.Equal(t, "", nameAt(5))
}

func TestProfilesAreIsolated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rp6.db")

	p6, err := Open(path, "p6")
	require.NoError(t, err)
	emu, err := Open(path, "emu:/kits/demo")
	require.NoError(t, err)
	t.Cleanup(func() { _ = p6.Close(); _ = emu.Close() })

	// Same slot number, two profiles: independent content.
	require.NoError(t, p6.Save(1, "hardware beat", []byte("hw")))
	require.NoError(t, emu.Save(1, "demo beat", []byte("emu")))

	name, data, ok, err := p6.Load(1)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "hardware beat", name)
	assert.Equal(t, "hw", string(data))

	name, data, ok, err = emu.Load(1)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "demo beat", name)
	assert.Equal(t, "emu", string(data))

	// List and meta are scoped too.
	require.NoError(t, p6.Save(2, "hw2", []byte("x")))
	pl, _ := p6.List()
	el, _ := emu.List()
	assert.Len(t, pl, 2)
	assert.Len(t, el, 1)

	require.NoError(t, p6.SetMeta("last", "2"))
	require.NoError(t, emu.SetMeta("last", "1"))
	pv, _, _ := p6.Meta("last")
	ev, _, _ := emu.Meta("last")
	assert.Equal(t, "2", pv)
	assert.Equal(t, "1", ev)

	// A third profile sees nothing.
	other, err := Open(path, "emu:/kits/other")
	require.NoError(t, err)
	defer other.Close()
	_, _, ok, _ = other.Load(1)
	assert.False(t, ok)
}

// TestLegacyMigration seeds a pre-profile database (slot/key primary keys) and
// verifies Open migrates the rows into the DefaultProfile without loss.
func TestLegacyMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Build the old schema by hand and insert a couple of rows.
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE sequences (
		slot INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT '',
		data TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO sequences(slot,name,data,updated_at) VALUES(1,'old','blob','2020-01-01T00:00:00Z')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO meta(key,value) VALUES('last','1')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// Opening migrates the legacy rows into the "p6" profile.
	s, err := Open(path, DefaultProfile)
	require.NoError(t, err)
	defer s.Close()

	name, data, ok, err := s.Load(1)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "old", name)
	assert.Equal(t, "blob", string(data))
	v, ok, _ := s.Meta("last")
	assert.True(t, ok)
	assert.Equal(t, "1", v)

	// The legacy content is not visible under a different profile.
	emu, err := Open(path, "emu:/kits/demo")
	require.NoError(t, err)
	defer emu.Close()
	_, _, ok, _ = emu.Load(1)
	assert.False(t, ok)

	// Reopening is idempotent (no double migration).
	s2, err := Open(path, DefaultProfile)
	require.NoError(t, err)
	defer s2.Close()
	_, _, ok, _ = s2.Load(1)
	assert.True(t, ok)
}
