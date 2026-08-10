package lsmkv

import (
	"bytes"
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
