package lsmkv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

type sparseIndexEntry struct {
	Key    []byte
	Offset int64
}

type sparseIndex struct {
	entries  []sparseIndexEntry
	interval int
	count    int
	bloom    *BloomFilter
	reader   io.ReaderAt
	size     int64
}

var ErrShortSparseIndex = errors.New("short or corrupt sparse index")
var ErrCorruptedSparseIndex = errors.New("truncated sparse index, missing key length or missing offset")

func (si *sparseIndex) EncodeSparseIndex() []byte {

	totalSize := 4
	for _, entry := range si.entries {
		totalSize += 4 + len(entry.Key) + 8
	}

	buf := make([]byte, totalSize)
	pos := 0

	binary.LittleEndian.PutUint32(buf[pos:pos+4], uint32(len(si.entries)))
	pos += 4

	for _, entry := range si.entries {
		binary.LittleEndian.PutUint32(buf[pos:pos+4], uint32(len(entry.Key)))
		pos += 4

		copy(buf[pos:pos+len(entry.Key)], entry.Key)
		pos += len(entry.Key)

		binary.LittleEndian.PutUint64(buf[pos:pos+8], uint64(entry.Offset))
		pos += 8
	}

	return buf

}

func DecodeSparseIndex(buf []byte) (*sparseIndex, error) {
	if len(buf) < 4 {
		return nil, ErrShortSparseIndex
	}

	entryCount := binary.LittleEndian.Uint32(buf[0:4])
	pos := 4

	entries := make([]sparseIndexEntry, 0, entryCount)

	for i := uint32(0); i < entryCount; i++ {
		if len(buf)-pos < 4 {
			return nil, ErrCorruptedSparseIndex
		}
		keyLen := binary.LittleEndian.Uint32(buf[pos : pos+4])
		pos += 4

		if len(buf)-pos < int(keyLen+8) {
			return nil, ErrCorruptedSparseIndex
		}

		key := make([]byte, keyLen)
		copy(key, buf[pos:pos+int(keyLen)])
		pos += int(keyLen)

		offset := int64(binary.LittleEndian.Uint64(buf[pos : pos+8]))
		pos += 8

		entries = append(entries, sparseIndexEntry{Key: key, Offset: offset})
	}

	return &sparseIndex{entries: entries}, nil

}

func newSparseIndex(interval int) *sparseIndex {
	if interval < 1 {
		interval = 1
	}
	return &sparseIndex{entries: make([]sparseIndexEntry, 0), interval: interval, count: 0}
}

func (si *sparseIndex) attachBloom(bloomFilter *BloomFilter) {
	si.bloom = bloomFilter
}

func (si *sparseIndex) attachReader(reader io.ReaderAt) {
	si.reader = reader
}

func (si *sparseIndex) attachSize(size int64) {
	si.size = size
}

func (si *sparseIndex) attachEntries(entries []sparseIndexEntry) {
	si.entries = entries
}

func (si *sparseIndex) add(key []byte, offset int64) {
	_ = si.bloom.Add(key)
	if (si.count % si.interval) == 0 {
		si.entries = append(si.entries, sparseIndexEntry{Key: key, Offset: offset})
	}
	si.count += 1

}

func (si *sparseIndex) lookup(key []byte) (int64, bool) {

	if ok, _ := si.bloom.MayContain(key); !ok {
		return 0, false
	}

	offset, ok := si.findStartOffset(key)
	if !ok {
		return 0, false
	}

	buf := make([]byte, si.size-offset)
	if _, err := si.reader.ReadAt(buf, offset); err != nil && err != io.EOF {
		return 0, false
	}
	for len(buf) > 0 {
		rec, n, err := Decode(buf)
		if err != nil {
			return 0, false
		}
		cmp := bytes.Compare(rec.Key, key)
		if cmp == 0 {
			return offset, true
		}
		if cmp > 0 {
			return 0, false
		}
		offset += int64(n)
		buf = buf[n:]
	}
	return 0, false
}

func (si *sparseIndex) findStartOffset(key []byte) (int64, bool) {
	lo, hi := 0, len(si.entries)

	for lo < hi {
		mid := (lo + hi) / 2
		if bytes.Compare(si.entries[mid].Key, key) > 0 {
			hi = mid
		} else {
			lo = mid + 1
		}
	}

	if lo == 0 {
		return 0, false
	}

	return si.entries[lo-1].Offset, true
}

func (si *sparseIndex) dataSize() int64 {
	return si.size
}
