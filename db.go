package lsmkv

import (
	"os"
	"path/filepath"
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

// Open recovers an existing database directory or creates a new one. Recovery is:
// load the existing SSTables, then WAL.Replay into a fresh memtable. The contract
// is strict: if Open returns a nil error, the database is in a consistent state
// and every acknowledged write is present.
func Open(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
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
	return &DB{dir: dir, wal: wal, mem: mem}, nil
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
	return nil

}

// Get returns (value, found, error). See the read path above.
func (db *DB) Get(key []byte) ([]byte, bool, error) {
	r, ok := db.mem.Get(key)
	if !ok {
		return nil, false, nil
	}
	if r.Kind == RecordDelete {
		return nil, false, nil
	}

	return r.Value, true, nil
	// return nil, false, ErrNotImplemented // TODO(M2; grows through M4)
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
	if err := db.wal.Close(); err != nil {
		return err
	}

	return nil
}
