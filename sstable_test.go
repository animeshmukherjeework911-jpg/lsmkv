package lsmkv

import (
	"fmt"
	"path/filepath"
	"testing"
)

// buildTestMemtable returns a memtable with n sequentially-keyed records
// (key-000, key-001, ...) so sort order matches numeric order.
func buildTestMemtable(n int) *Memtable {
	m := NewMemtable()
	for i := 0; i < n; i++ {
		m.Put(Record{
			Key:   []byte(fmt.Sprintf("key-%03d", i)),
			Value: []byte(fmt.Sprintf("val-%03d", i)),
			Kind:  RecordSet,
		})
	}
	return m
}

func mustGet(t *testing.T, sst *SSTable, key string) Record {
	t.Helper()
	rec, ok, err := sst.Get([]byte(key))
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", key, err)
	}
	if !ok {
		t.Fatalf("Get(%q) = not found, want found", key)
	}
	return rec
}

func mustNotFind(t *testing.T, sst *SSTable, key string) {
	t.Helper()
	_, ok, err := sst.Get([]byte(key))
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", key, err)
	}
	if ok {
		t.Fatalf("Get(%q) = found, want not found", key)
	}
}

// GATE M4 — sparse index: keys that land exactly on a sparse boundary must
// resolve directly; keys in between must be found by scanning forward from
// the preceding boundary.
func TestSparseIndexBoundaryAndMidBlockKeys(t *testing.T) {
	const n = 50 // spans multiple sparseIndexInterval (16) blocks, with a partial final block
	m := buildTestMemtable(n)
	path := filepath.Join(t.TempDir(), "sstable-0.sst")

	sst, err := FlushMemtable(m, path)
	if err != nil {
		t.Fatal(err)
	}

	// Boundary keys: index 0, 16, 32, 48 land exactly on sparse entries
	// given sparseIndexInterval == 16.
	boundaries := []int{0, 16, 32, 48}
	for _, i := range boundaries {
		key := fmt.Sprintf("key-%03d", i)
		rec := mustGet(t, sst, key)
		want := fmt.Sprintf("val-%03d", i)
		if string(rec.Value) != want {
			t.Errorf("boundary key %q: got value %q, want %q", key, rec.Value, want)
		}
	}

	// Mid-block keys: not themselves in the sparse index, must be found by
	// scanning forward from the nearest preceding boundary.
	midBlock := []int{5, 20, 45, 49}
	for _, i := range midBlock {
		key := fmt.Sprintf("key-%03d", i)
		rec := mustGet(t, sst, key)
		want := fmt.Sprintf("val-%03d", i)
		if string(rec.Value) != want {
			t.Errorf("mid-block key %q: got value %q, want %q", key, rec.Value, want)
		}
	}
}

// GATE M4 — a key that was never written must come back not-found, whether
// it sorts before, inside, or after the key range, and whether or not the
// bloom filter's early-out fires.
func TestSparseIndexAbsentKeys(t *testing.T) {
	m := buildTestMemtable(50)
	path := filepath.Join(t.TempDir(), "sstable-0.sst")

	sst, err := FlushMemtable(m, path)
	if err != nil {
		t.Fatal(err)
	}

	mustNotFind(t, sst, "aaa-before-range")
	mustNotFind(t, sst, "key-025x") // sorts inside the range, never inserted
	mustNotFind(t, sst, "zzz-after-range")
}

// GATE M4 — sparse index + bloom filter state must be reconstructed
// correctly after a close/reopen cycle (OpenSSTable), not just right after
// FlushMemtable.
func TestSparseIndexSurvivesReopen(t *testing.T) {
	m := buildTestMemtable(50)
	path := filepath.Join(t.TempDir(), "sstable-0.sst")

	if _, err := FlushMemtable(m, path); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSSTable(path)
	if err != nil {
		t.Fatal(err)
	}

	rec := mustGet(t, reopened, "key-020")
	if string(rec.Value) != "val-020" {
		t.Errorf("got value %q, want %q", rec.Value, "val-020")
	}
	mustNotFind(t, reopened, "key-999")
}
