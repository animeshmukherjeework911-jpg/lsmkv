package lsmkv

import (
	"bytes"
	"fmt"
	"testing"
)

func entriesEqual(a, b []sparseIndexEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i].Key, b[i].Key) || a[i].Offset != b[i].Offset {
			return false
		}
	}
	return true
}

func TestSparseIndexEncodeDecodeRoundTrip(t *testing.T) {
	si := &sparseIndex{entries: []sparseIndexEntry{
		{Key: []byte("apple"), Offset: 0},
		{Key: []byte("cherry"), Offset: 46},
		{Key: []byte("fig"), Offset: 90},
	}}

	encoded := si.EncodeSparseIndex()

	decoded, err := DecodeSparseIndex(encoded)
	if err != nil {
		t.Fatalf("DecodeSparseIndex returned error: %v", err)
	}

	if !entriesEqual(decoded.entries, si.entries) {
		t.Errorf("entries mismatch after round trip: got %+v, want %+v", decoded.entries, si.entries)
	}
}

func TestSparseIndexEncodeDecodeEmpty(t *testing.T) {
	si := &sparseIndex{entries: []sparseIndexEntry{}}

	encoded := si.EncodeSparseIndex()
	if len(encoded) != 4 {
		t.Fatalf("encoded empty index length = %d, want 4 (just the entry count header)", len(encoded))
	}

	decoded, err := DecodeSparseIndex(encoded)
	if err != nil {
		t.Fatalf("DecodeSparseIndex returned error: %v", err)
	}
	if len(decoded.entries) != 0 {
		t.Errorf("decoded entries = %+v, want empty", decoded.entries)
	}
}

func TestSparseIndexEncodeDecodeManyEntries(t *testing.T) {
	const n = 200
	entries := make([]sparseIndexEntry, n)
	offset := int64(0)
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%04d", i))
		entries[i] = sparseIndexEntry{Key: key, Offset: offset}
		offset += int64(len(key)) + 37 // arbitrary stand-in record size
	}
	si := &sparseIndex{entries: entries}

	encoded := si.EncodeSparseIndex()
	decoded, err := DecodeSparseIndex(encoded)
	if err != nil {
		t.Fatalf("DecodeSparseIndex returned error: %v", err)
	}

	if !entriesEqual(decoded.entries, si.entries) {
		t.Errorf("entries mismatch after round trip for %d entries", n)
	}
}

func TestSparseIndexEncodeDecodeZeroLengthKey(t *testing.T) {
	// The wire format allows keyLen == 0 structurally, even though real usage
	// never produces an empty key. Decode must not choke on it.
	si := &sparseIndex{entries: []sparseIndexEntry{
		{Key: []byte(""), Offset: 5},
		{Key: []byte("nonempty"), Offset: 10},
	}}

	encoded := si.EncodeSparseIndex()
	decoded, err := DecodeSparseIndex(encoded)
	if err != nil {
		t.Fatalf("DecodeSparseIndex returned error: %v", err)
	}
	if !entriesEqual(decoded.entries, si.entries) {
		t.Errorf("entries mismatch: got %+v, want %+v", decoded.entries, si.entries)
	}
}

func TestSparseIndexDecodeShortBuffer(t *testing.T) {
	cases := []struct {
		name string
		buf  []byte
	}{
		{"empty", nil},
		{"three bytes, less than the count header", []byte{1, 2, 3}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeSparseIndex(c.buf)
			if err == nil {
				t.Errorf("DecodeSparseIndex(%d bytes) = nil error, want an error", len(c.buf))
			}
		})
	}
}

func TestSparseIndexDecodeTruncatedEntry(t *testing.T) {
	si := &sparseIndex{entries: []sparseIndexEntry{
		{Key: []byte("apple"), Offset: 0},
		{Key: []byte("banana"), Offset: 21},
	}}
	encoded := si.EncodeSparseIndex()

	// Truncate partway through the second entry: the count header still
	// claims 2 entries, but the bytes for the second one are incomplete.
	truncated := encoded[:len(encoded)-3]

	_, err := DecodeSparseIndex(truncated)
	if err == nil {
		t.Fatal("DecodeSparseIndex on truncated buffer = nil error, want an error")
	}
}

func TestSparseIndexDecodeTruncatedKeyLenField(t *testing.T) {
	si := &sparseIndex{entries: []sparseIndexEntry{
		{Key: []byte("apple"), Offset: 0},
		{Key: []byte("banana"), Offset: 21},
	}}
	encoded := si.EncodeSparseIndex()

	// Truncate right after the first entry, so the second entry's keyLen
	// field itself is incomplete (fewer than 4 bytes remain).
	firstEntrySize := 4 + len("apple") + 8
	truncated := encoded[:4+firstEntrySize+2]

	_, err := DecodeSparseIndex(truncated)
	if err == nil {
		t.Fatal("DecodeSparseIndex on buffer truncated mid-keyLen = nil error, want an error")
	}
}
