package lsmkv

import "os"

// SSTable is an immutable, sorted, on-disk table produced by flushing a memtable.
// Immutability is the trick that makes the rest of the engine simple: a file is
// never edited once written — only created, and later deleted by compaction (M5).
//
// A reasonable file layout to design in M3:
//
//	[ sorted data blocks ] [ sparse index ] [ bloom filter ] [ footer ]
//
//   - sparse index: one key per block, so Get binary-searches to a block instead
//     of scanning the whole file.
//   - bloom filter (M4): lets Get skip this file entirely when the key is absent.
//     A read-heavy store lives or dies on this early-out.
type SSTable struct {
	// TODO(M3): path, file handle, in-memory sparse index, and (M4) bloom filter.
	path string
	file *os.File
	index map[string]int64
}

// FlushMemtable writes m's sorted records to a brand-new SSTable file at path.
func FlushMemtable(m *Memtable, path string) (*SSTable, error) {
	return nil, ErrNotImplemented // TODO(M3)
}

// OpenSSTable reopens an existing table, loading its index (and bloom) into memory.
func OpenSSTable(path string) (*SSTable, error) {
	return nil, ErrNotImplemented // TODO(M4)
}

// Get returns (record, found, error). From M4 it must check the bloom filter first
// and return early on a negative — that early return is the entire reason the
// filter exists.
func (s *SSTable) Get(key []byte) (Record, bool, error) {
	return Record{}, false, ErrNotImplemented // TODO(M3, then M4)
}

// Path returns the file this table is backed by (used by compaction to delete it).
func (s *SSTable) Path() string {
	panic("TODO(M3)")
}
