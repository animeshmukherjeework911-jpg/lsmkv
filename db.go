package lsmkv

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	sstablePrefix                 = "sstable-"
	sstableSuffix                 = ".sst"
	sstableSeqWidth               = 8
	defaultMemtableFlushThreshold = 4 << 20
	defaultIndexMode              = sparseIndexMode
)

// DB is the public storage engine. Keep the two paths below taped to your monitor;
// every method here is just one step along one of them.
//
// Write path:
//
//	Put -> WAL.Append (durable)          // survives a crash from here on
//	    -> Memtable.Put (visible)        // now readable
//	    -> if Memtable too big:
//	         FlushMemtable -> new SSTable
//	         WAL.Truncate                // the log's job is done for that data
//
// Read path (newest wins):
//
//	Get -> Memtable
//	    -> SSTables, newest -> oldest (bloom-skipped)
//	    -> not found
type DB struct {
	dir  string
	wal  *WAL
	mem  *Memtable
	ssts []*SSTable // decide the ordering, then document it and never second-guess it
}

func sstableFilename(seq int) string {
	return fmt.Sprintf("%s%0*d%s", sstablePrefix, sstableSeqWidth, seq, sstableSuffix)
}

func parseSSTableSeq(path string) (int, error) {
	base := filepath.Base(path)
	numStr := strings.TrimSuffix(strings.TrimPrefix(base, sstablePrefix), sstableSuffix)
	return strconv.Atoi(numStr)
}

func nextSSTablePath(dir string) (string, error) {
	_, seq, found, err := latestSSTable(dir)
	if err != nil {
		return "", err
	}

	if !found {
		seq = -1
	}
	return filepath.Join(dir, sstableFilename(seq+1)), nil
}

func latestSSTable(dir string) (string, int, bool, error) {

	files, err := filepath.Glob(filepath.Join(dir, sstablePrefix+"*"+sstableSuffix))

	if err != nil {
		return "", 0, false, err
	}

	bestPath := ""
	bestSeq := -1

	for _, f := range files {
		seq, err := parseSSTableSeq(f)
		if err != nil {
			return "", 0, false, err
		}
		if seq > bestSeq {
			bestSeq = seq
			bestPath = f
		}
	}

	return bestPath, bestSeq, bestPath != "", nil

}

func loadSSTables(dir string) ([]*SSTable, error) {
	files, err := filepath.Glob(filepath.Join(dir, sstablePrefix+"*"+sstableSuffix))
	if err != nil {
		return nil, err
	}

	type entry struct {
		seq  int
		path string
	}

	entries := make([]entry, 0, len(files))

	for _, f := range files {
		seq, err := parseSSTableSeq(f)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{seq, f})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].seq < entries[j].seq })

	ssts := make([]*SSTable, 0, len(entries))
	for _, e := range entries {
		sst, err := OpenSSTable(e.path, defaultIndexMode)
		if err != nil {
			return nil, err
		}
		ssts = append(ssts, sst)
	}
	return ssts, nil
}

// Open recovers an existing database directory or creates a new one. Recovery is:
// load the existing SSTables, then WAL.Replay into a fresh memtable. The contract
// is strict: if Open returns a nil error, the database is in a consistent state
// and every acknowledged write is present.
func Open(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	ssts, err := loadSSTables(dir)
	if err != nil {
		return nil, err
	}

	wal, err := OpenWAL(filepath.Join(dir, "wal.log"))
	if err != nil {
		return nil, err
	}

	mem := NewMemtable()
	if err := wal.Replay(func(r Record) error {
		mem.Put(r)
		return nil
	}); err != nil {
		return nil, err
	}
	return &DB{dir: dir, wal: wal, mem: mem, ssts: ssts}, nil
	// return nil, ErrNotImplemented // TODO(M1 gives it WAL replay; it grows through M4)
}

// Put stores key=value durably. See the write path above.
func (db *DB) Put(key, value []byte) error {
	r := Record{
		Key:   key,
		Value: value,
		Kind:  RecordSet,
	}
	if err := db.wal.Append(r); err != nil {
		return err
	}

	db.mem.Put(r)
	if db.mem.SizeBytes() > defaultMemtableFlushThreshold {
		nextSSTablePathStr, err := nextSSTablePath(db.dir)
		if err != nil {
			return err
		}
		sst, err := FlushMemtable(db.mem, nextSSTablePathStr, defaultIndexMode)
		if err != nil {
			return err
		}
		db.ssts = append(db.ssts, sst)

		if err := db.wal.Truncate(); err != nil {
			return err
		}

		db.mem = NewMemtable()
	}
	return nil

}

// Get returns (value, found, error). See the read path above.
func (db *DB) Get(key []byte) ([]byte, bool, error) {

	if r, ok := db.mem.Get(key); ok {
		if r.Kind == RecordDelete {
			return nil, false, nil
		}
		return r.Value, true, nil
	}

	for i := len(db.ssts) - 1; i >= 0; i-- {
		r, ok, err := db.ssts[i].Get(key)
		if err != nil {
			return nil, false, err
		}

		if !ok {
			continue
		}

		if r.Kind == RecordDelete {
			return nil, false, nil
		}

		return r.Value, true, nil
	}

	return nil, false, nil
}

// Delete writes a tombstone for key. It is a Put, not an erase.
func (db *DB) Delete(key []byte) error {

	r := Record{Key: key, Kind: RecordDelete}
	if err := db.wal.Append(r); err != nil {
		return err
	}
	db.mem.Put(r)
	return nil
}

// Close flushes the active memtable and closes all files cleanly. After a clean
// Close, the next Open should have nothing to replay.
func (db *DB) Close() error {

	if db.mem.SizeBytes() > 0 {
		path, err := nextSSTablePath(db.dir)
		if err != nil {
			return err
		}

		sst, err := FlushMemtable(db.mem, path, defaultIndexMode)
		if err != nil {
			return err
		}

		db.ssts = append(db.ssts, sst)
		if err := db.wal.Truncate(); err != nil {
			return err
		}
		db.mem = NewMemtable()
	}

	return db.wal.Close()
}
