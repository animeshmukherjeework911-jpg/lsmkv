package lsmkv

import (
	"bytes"
	"sort"
)

// Memtable is the in-memory write buffer. Reads consult it first; it must stay
// sorted by key so it can be streamed into a sorted SSTable in a single pass (M3).
//
// Start with the simplest correct thing — a map plus a sort at flush time is fine
// for M2. Only reach for a skiplist later, and only if the M6 benchmark tells you
// the sort is actually your bottleneck. Do not optimise on a hunch.
type Memtable struct {
	records map[string]Record
	size    int
}

// NewMemtable returns an empty memtable.
func NewMemtable() *Memtable {
	return &Memtable{records: make(map[string]Record)}
}

// Put inserts or overwrites a record. A delete is Put with a RecordDelete kind,
// never a map removal — the tombstone has to be visible to shadow older SSTables.
func (m *Memtable) Put(r Record) {

	key := string(r.Key)
	if res, ok := m.records[key]; ok {
		m.size -= res.GetRecordLen()
	}

	m.records[key] = r
	m.size += r.GetRecordLen()

}

// Get returns (record, found). A tombstone counts as FOUND: the caller must stop
// and not fall through to older SSTables. Get this wrong and deleted keys crawl
// back out of old files — the canonical LSM bug.
func (m *Memtable) Get(key []byte) (Record, bool) {
	r, ok := m.records[string(key)]
	return r, ok
}

// SizeBytes drives the flush threshold in M3.
func (m *Memtable) SizeBytes() int {
	return m.size
}

// Sorted returns every record in ascending key order, ready to write to an SSTable.
func (m *Memtable) Sorted() []Record {
	records := make([]Record, 0, len(m.records))
	for _, val := range m.records {
		records = append(records, val)
	}
	sort.Slice(records, func(i, j int) bool {
		return bytes.Compare(records[i].Key, records[j].Key) < 0
	})
	return records
}
