package lsmkv

import (
	"io"
	"os"
)

// SSTable is an immutable, sorted, on-disk table produced by flushing a memtable.
// Immutability is the trick that makes the rest of the engine simple: a file is
// never edited once written — only created, and later deleted by compaction (M5).
//
// A reasonable file layout to design in M3:
//
//		[ sorted data blocks ] [ sparse index ] [ bloom filter ] [ footer ]
//
//	  - sparse index: one key per block, so Get binary-searches to a block instead
//	    of scanning the whole file.
//	  - bloom filter (M4): lets Get skip this file entirely when the key is absent.
//	    A read-heavy store lives or dies on this early-out.
type SSTable struct {
	// TODO(M3): path, file handle, in-memory sparse index, and (M4) bloom filter.
	path  string
	file  *os.File
	index map[string]int64
}

// FlushMemtable writes m's sorted records to a brand-new SSTable file at path.
func FlushMemtable(m *Memtable, path string) (*SSTable, error) {

	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)

	if err != nil {
		return nil, err
	}

	index := make(map[string]int64)
	offset := int64(0)

	for _, record := range m.Sorted() {
		encodedRecord, err := Encode(record)
		if err != nil {
			return nil, err
		}

		index[string(record.Key)] = offset
		offset += int64(len(encodedRecord))

		if _, err := file.Write(encodedRecord); err != nil {
			return nil, err
		}
	}

	if err := file.Sync(); err != nil {
		return nil, err
	}
	return &SSTable{path: path, file: file, index: index}, nil

}

// OpenSSTable reopens an existing table, loading its index (and bloom) into memory.
func OpenSSTable(path string) (*SSTable, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	index := make(map[string]int64)
	offset := int64(0)
	for offset < int64(len(data)) {
		rec, n, err := Decode(data[offset:])
		if err != nil {
			return nil, err
		}
		index[string(rec.Key)] = offset
		offset += int64(n)
	}

	return &SSTable{path: path, file: file, index: index}, nil
}

// Get returns (record, found, error). From M4 it must check the bloom filter first
// and return early on a negative — that early return is the entire reason the
// filter exists.
func (s *SSTable) Get(key []byte) (Record, bool, error) {

	info, err := s.file.Stat()
	if err != nil {
		return Record{}, false, err
	}

	offset, ok := s.index[string(key)]
	if !ok {
		return Record{}, false, nil
	}

	buf := make([]byte, info.Size()-offset)
	if _, err := s.file.ReadAt(buf, offset); err != nil {
		return Record{}, false, err
	}

	record, _, err := Decode(buf)
	if err != nil {
		return Record{}, false, err
	}
	return record, true, nil
}

// Path returns the file this table is backed by (used by compaction to delete it).
func (s *SSTable) Path() string {
	return s.path
}
