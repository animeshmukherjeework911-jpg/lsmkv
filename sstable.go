package lsmkv

import (
	"os"
)

const (
	sparseIndexInterval    = 16
	bloomFalsePositiveRate = 0.01
)

// SSTable is an immutable, sorted, on-disk table produced by flushing a memtable.
// Immutability is the trick that makes the rest of the engine simple: a file is
// never edited once written — only created, and later deleted by compaction (M5).
//
// File layout:
//
//	[ sorted data blocks ] [ sparse index ] [ bloom filter ] [ footer ]
//
//   - sparse index: one key per block, so Get binary-searches to a block instead
//     of scanning the whole file.
//   - bloom filter: lets Get skip this file entirely when the key is absent.
//     A read-heavy store lives or dies on this early-out.
type SSTable struct {
	path  string
	file  *os.File
	index *sparseIndex
}

// FlushMemtable writes m's sorted records to a brand-new SSTable file at path,
// followed by a sparse index, a bloom filter, and a footer pointing at both.
func FlushMemtable(m *Memtable, path string) (*SSTable, error) {

	index := newSparseIndex(sparseIndexInterval)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	bloom := NewBloomFilter(len(m.records), bloomFalsePositiveRate)
	index.attachBloom(bloom)
	index.attachReader(file)

	offset := int64(0)
	for _, record := range m.Sorted() {
		encodedRecord, err := Encode(record)
		if err != nil {
			return nil, err
		}

		index.add(record.Key, offset)
		offset += int64(len(encodedRecord))

		if _, err := file.Write(encodedRecord); err != nil {
			return nil, err
		}
	}

	index.attachSize(offset)

	sparseIndexOffset := offset
	sparseIndexBytes := index.EncodeSparseIndex()
	if _, err := file.Write(sparseIndexBytes); err != nil {
		return nil, err
	}
	offset += int64(len(sparseIndexBytes))

	bloomFilterOffset := offset
	bloomBytes := EncodeBloomFilter(bloom)
	if _, err := file.Write(bloomBytes); err != nil {
		return nil, err
	}
	offset += int64(len(bloomBytes))

	footer := &Footer{
		sparseIndexOffset: uint64(sparseIndexOffset),
		sparseIndexLength: uint64(len(sparseIndexBytes)),
		bloomFilterOffset: uint64(bloomFilterOffset),
		bloomFilterLength: uint64(len(bloomBytes)),
	}
	if _, err := file.Write(footer.EncodeFooter()); err != nil {
		return nil, err
	}

	if err := file.Sync(); err != nil {
		return nil, err
	}
	return &SSTable{path: path, file: file, index: index}, nil
}

// OpenSSTable reopens an existing table, reading the footer to load the
// sparse index and bloom filter directly, without decoding the data blocks.
func OpenSSTable(path string) (*SSTable, error) {
	index := newSparseIndex(sparseIndexInterval)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	footerBuf := make([]byte, footerSize)
	if _, err := file.ReadAt(footerBuf, info.Size()-footerSize); err != nil {
		return nil, err
	}
	footer, err := DecodeFooter(footerBuf)
	if err != nil {
		return nil, err
	}

	sparseIndexBuf := make([]byte, footer.sparseIndexLength)
	if _, err := file.ReadAt(sparseIndexBuf, int64(footer.sparseIndexOffset)); err != nil {
		return nil, err
	}
	decodedIndex, err := DecodeSparseIndex(sparseIndexBuf)
	if err != nil {
		return nil, err
	}

	bloomBuf := make([]byte, footer.bloomFilterLength)
	if _, err := file.ReadAt(bloomBuf, int64(footer.bloomFilterOffset)); err != nil {
		return nil, err
	}
	bloom, err := DecodeBloomFilter(bloomBuf)
	if err != nil {
		return nil, err
	}

	index.attachEntries(decodedIndex.entries)
	index.attachBloom(&bloom)
	index.attachReader(file)
	index.attachSize(int64(footer.sparseIndexOffset))

	return &SSTable{path: path, file: file, index: index}, nil
}

// Get returns (record, found, error). It checks the bloom filter first and
// returns early on a negative — that early return is the entire reason the
// filter exists.
func (s *SSTable) Get(key []byte) (Record, bool, error) {

	info, err := s.file.Stat()
	if err != nil {
		return Record{}, false, err
	}

	offset, ok := s.index.lookup(key)
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
