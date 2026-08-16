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

// GATE M4 — OpenSSTable must not decode the data blocks at all: it reads
// the footer, then only the sparse-index and bloom-filter sections that
// follow it. Proven here by corrupting one specific record deep inside the
// data blocks and showing three things at once: (1) OpenSSTable itself
// still succeeds, because opening never reads that region; (2) a Get for
// an unrelated key, whose scan never reaches the corrupted bytes, still
// works correctly; (3) a Get for the corrupted record's own key does fail,
// proving the corruption is real and that reading it is what surfaces the
// problem — not that the byte flip was silently inert.
func TestOpenSSTableDoesNotDecodeDataBlocksOnOpen(t *testing.T) {
	m := buildTestMemtable(50)
	dir := t.TempDir()
	path := filepath.Join(dir, "sstable-0.sst")

	if _, err := FlushMemtable(m, path); err != nil {
		t.Fatal(err)
	}

	// Every record here is a fixed 27 bytes (13-byte header + 7-byte key +
	// 7-byte value), so record 5 ("key-005") starts at offset 5*27 = 135.
	// Byte 155 falls inside its value, well past the header, so flipping it
	// breaks record 5's crc without touching any other record.
	const corruptedRecordOffset = 135
	const corruptOffset = 155
	flipByteAt(t, path, corruptOffset)

	reopened, err := OpenSSTable(path)
	if err != nil {
		t.Fatalf("OpenSSTable failed on data-block corruption it should never read on open: %v", err)
	}

	// key-020 starts scanning from the sparse boundary at key-016 and never
	// reaches record 5's bytes.
	rec := mustGet(t, reopened, "key-020")
	if string(rec.Value) != "val-020" {
		t.Errorf("unrelated key affected by unrelated corruption: got %q, want %q", rec.Value, "val-020")
	}

	// key-005 itself requires decoding the corrupted record, so it must not
	// come back as a successful, correct read (a decode failure surfaces
	// here as "not found" rather than an error — see sparseIndex.lookup).
	corruptRec, corruptOk, _ := reopened.Get([]byte("key-005"))
	if corruptOk && string(corruptRec.Value) == "val-005" {
		t.Fatalf("Get(%q) returned the correct value despite corrupting offset %d (record starts at %d); "+
			"corruption did not take effect as expected", "key-005", corruptOffset, corruptedRecordOffset)
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

	if _, err := FlushMemtable(m, path); err != nil {
		t.Fatal(err)
	}

	sst, err := OpenSSTable(path)
	if err != nil {
		t.Fatal(err)
	}

	si := sst.index
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
	oldSst, err := FlushMemtable(older, filepath.Join(dir, "manual-old.sst"))
	if err != nil {
		t.Fatal(err)
	}

	newer := NewMemtable()
	newer.Put(Record{Key: []byte("k"), Value: []byte("new"), Kind: RecordSet})
	newSst, err := FlushMemtable(newer, filepath.Join(dir, "manual-new.sst"))
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
	tombSst, err := FlushMemtable(tombstoned, filepath.Join(dir, "manual-tomb.sst"))
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
