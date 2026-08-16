package lsmkv

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// flipByteAt corrupts a single byte at the given file offset in place.
func flipByteAt(t *testing.T, path string, offset int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open for corruption: %v", err)
	}
	defer f.Close()

	buf := make([]byte, 1)
	if _, err := f.ReadAt(buf, offset); err != nil {
		t.Fatalf("read byte to corrupt: %v", err)
	}
	buf[0] ^= 0xFF
	if _, err := f.WriteAt(buf, offset); err != nil {
		t.Fatalf("write corrupted byte: %v", err)
	}
}

// GATE M4 — OpenSSTable in sparse mode must not decode the data blocks at
// all: it reads the footer, then only the sparse-index and bloom-filter
// sections. Corrupting a byte deep inside the data blocks (but outside the
// persisted metadata) must not prevent a sparse-mode reopen from succeeding.
// Full mode, which still decodes every record on open, must fail on the
// same corruption — the contrast is the proof that sparse mode skips the
// data blocks entirely on open.
func TestOpenSSTableSparseModeDoesNotDecodeDataBlocks(t *testing.T) {
	m := buildTestMemtable(50)
	dir := t.TempDir()
	sparsePath := filepath.Join(dir, "sparse.sst")
	fullPath := filepath.Join(dir, "full.sst")

	if _, err := FlushMemtable(m, sparsePath, sparseIndexMode); err != nil {
		t.Fatal(err)
	}
	if _, err := FlushMemtable(m, fullPath, fullIndexMode); err != nil {
		t.Fatal(err)
	}

	// Offset 50 lands well inside the data blocks for 50 records of this
	// size, and well before any footer/index/bloom section.
	const corruptOffset = 50
	flipByteAt(t, sparsePath, corruptOffset)
	flipByteAt(t, fullPath, corruptOffset)

	if _, err := OpenSSTable(sparsePath, sparseIndexMode); err != nil {
		t.Errorf("OpenSSTable(sparseIndexMode) failed on data-block corruption it should never read: %v", err)
	}

	if _, err := OpenSSTable(fullPath, fullIndexMode); err == nil {
		t.Errorf("OpenSSTable(fullIndexMode) succeeded despite data-block corruption; " +
			"full mode decodes every record on open and should have failed")
	}
}

// countingReaderAt wraps an io.ReaderAt and counts how many times ReadAt is
// called, so a test can assert a code path never touched the underlying
// file.
type countingReaderAt struct {
	inner io.ReaderAt
	calls int
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.calls++
	return c.inner.ReadAt(p, off)
}

// GATE M4 — a Get for a key the bloom filter rejects must skip the sparse
// index scan (and therefore the file read) entirely; a Get for a present
// key must actually read the data blocks. This is the timing/scan-avoidance
// gate STAGES.md asks for, made deterministic via a read counter instead of
// a flaky wall-clock comparison.
func TestBloomFilterSkipsScanForAbsentKey(t *testing.T) {
	m := buildTestMemtable(50)
	dir := t.TempDir()
	path := filepath.Join(dir, "sstable.sst")

	if _, err := FlushMemtable(m, path, sparseIndexMode); err != nil {
		t.Fatal(err)
	}

	sst, err := OpenSSTable(path, sparseIndexMode)
	if err != nil {
		t.Fatal(err)
	}

	si, ok := sst.index.(*sparseIndex)
	if !ok {
		t.Fatal("expected sst.index to be *sparseIndex")
	}

	counter := &countingReaderAt{inner: si.reader}
	si.attachReader(counter)

	_, found, err := sst.Get([]byte("definitely-absent-key"))
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected absent key to be reported not found")
	}
	if counter.calls != 0 {
		t.Errorf("bloom filter did not short-circuit the scan: expected 0 ReadAt calls for an absent key, got %d", counter.calls)
	}

	counter.calls = 0
	_, found, err = sst.Get([]byte("key-005"))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected present key to be found")
	}
	if counter.calls == 0 {
		t.Errorf("expected at least one ReadAt call for a present key, got 0")
	}
}

// GATE M4 — newest version of a key wins across multiple real, persisted
// SSTables, not just memtable-vs-one-file. Covers both an overwrite and a
// tombstone shadowing an older value.
func TestNewestWinsAcrossMultipleSSTables(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	older := NewMemtable()
	older.Put(Record{Key: []byte("k"), Value: []byte("old"), Kind: RecordSet})
	older.Put(Record{Key: []byte("only-in-old"), Value: []byte("still-here"), Kind: RecordSet})
	oldSst, err := FlushMemtable(older, filepath.Join(dir, "manual-old.sst"), sparseIndexMode)
	if err != nil {
		t.Fatal(err)
	}

	newer := NewMemtable()
	newer.Put(Record{Key: []byte("k"), Value: []byte("new"), Kind: RecordSet})
	newSst, err := FlushMemtable(newer, filepath.Join(dir, "manual-new.sst"), sparseIndexMode)
	if err != nil {
		t.Fatal(err)
	}

	// db.ssts is newest-last, matching the order DB.Get walks backward from.
	db.ssts = append(db.ssts, oldSst, newSst)

	v, ok, err := db.Get([]byte("k"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(v) != "new" {
		t.Fatalf("newest-wins violated: got %q/%v, want %q/true", v, ok, "new")
	}

	// A key only present in the older table must still be found by falling through.
	v, ok, err = db.Get([]byte("only-in-old"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(v) != "still-here" {
		t.Fatalf("fell through incorrectly: got %q/%v, want %q/true", v, ok, "still-here")
	}

	// A tombstone in the newer table must shadow the older table's value.
	tombstoned := NewMemtable()
	tombstoned.Put(Record{Key: []byte("k"), Kind: RecordDelete})
	tombSst, err := FlushMemtable(tombstoned, filepath.Join(dir, "manual-tomb.sst"), sparseIndexMode)
	if err != nil {
		t.Fatal(err)
	}
	db.ssts = append(db.ssts, tombSst)

	_, ok, err = db.Get([]byte("k"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("newest tombstone did not shadow older value")
	}
}
