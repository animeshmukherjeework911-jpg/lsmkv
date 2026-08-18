package lsmkv

import (
	"fmt"
	"path/filepath"
	"testing"
)

// GATE M5 prerequisite — SSTable.Records must return every record that was
// flushed, in ascending key order, without touching the sparse
// index/bloom/footer sections. Compaction depends entirely on this being
// correct.
func TestSSTableRecordsRoundTrip(t *testing.T) {
	const n = 50
	m := buildTestMemtable(n)
	path := filepath.Join(t.TempDir(), "sstable-0.sst")

	sst, err := FlushMemtable(m, path)
	if err != nil {
		t.Fatal(err)
	}

	records, err := sst.Records()
	if err != nil {
		t.Fatalf("Records() returned error: %v", err)
	}

	if len(records) != n {
		t.Fatalf("Records() returned %d records, want %d", len(records), n)
	}

	for i, rec := range records {
		wantKey := fmt.Sprintf("key-%03d", i)
		wantValue := fmt.Sprintf("val-%03d", i)
		if string(rec.Key) != wantKey {
			t.Errorf("record %d: key = %q, want %q", i, rec.Key, wantKey)
		}
		if string(rec.Value) != wantValue {
			t.Errorf("record %d: value = %q, want %q", i, rec.Value, wantValue)
		}
		if rec.Kind != RecordSet {
			t.Errorf("record %d: kind = %v, want RecordSet", i, rec.Kind)
		}
	}
}

// Records() must also work correctly after a close/reopen cycle, not just
// immediately after FlushMemtable — the data-block boundary comes from a
// different source (the footer) in that path.
func TestSSTableRecordsAfterReopen(t *testing.T) {
	const n = 50
	m := buildTestMemtable(n)
	path := filepath.Join(t.TempDir(), "sstable-0.sst")

	if _, err := FlushMemtable(m, path); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSSTable(path)
	if err != nil {
		t.Fatal(err)
	}

	records, err := reopened.Records()
	if err != nil {
		t.Fatalf("Records() returned error: %v", err)
	}

	if len(records) != n {
		t.Fatalf("Records() returned %d records, want %d", len(records), n)
	}
	if string(records[0].Key) != "key-000" || string(records[n-1].Key) != fmt.Sprintf("key-%03d", n-1) {
		t.Errorf("unexpected boundary records: first=%q last=%q", records[0].Key, records[n-1].Key)
	}
}

// A table with a single record (or none) must not confuse the loop bounds.
func TestSSTableRecordsSmallTable(t *testing.T) {
	m := NewMemtable()
	m.Put(Record{Key: []byte("only"), Value: []byte("one"), Kind: RecordSet})
	path := filepath.Join(t.TempDir(), "sstable-0.sst")

	sst, err := FlushMemtable(m, path)
	if err != nil {
		t.Fatal(err)
	}

	records, err := sst.Records()
	if err != nil {
		t.Fatalf("Records() returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Records() returned %d records, want 1", len(records))
	}
	if string(records[0].Key) != "only" || string(records[0].Value) != "one" {
		t.Errorf("got %q/%q, want %q/%q", records[0].Key, records[0].Value, "only", "one")
	}
}
