package lsmkv

import (
	"io"
	"os"
)

// WAL is the write-ahead log. Every mutation is appended here and fsync'd
// BEFORE Put returns to the caller. This single ordering is the entire reason
// the database can promise durability.
//
// Your accounting-engine instinct maps straight onto the invariant here:
//
//	If Put returned nil, the write survives a `kill -9` one nanosecond later.
//
// Build this first. Everything else is optimisation on top of it.
type WAL struct {
	// TODO(M1): an *os.File opened O_APPEND, plus whatever you need for Sync.
	file *os.File
}

// OpenWAL opens or creates the log at path, ready for Append and Replay.
func OpenWAL(path string) (*WAL, error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	} else {
		return &WAL{file: file}, nil
		// return nil, ErrNotImplemented // TODO(M1)
	}

}

// Append writes one record and fsyncs it to disk. Do NOT batch or buffer yet —
// correctness in M1, throughput in M6. Returning nil is a durability promise you
// are not allowed to break.
func (w *WAL) Append(r Record) error {
	buf, err := Encode(r)
	if err != nil {
		return err
	}
	if _, err := w.file.Write(buf); err != nil {
		return err
	}

	return w.file.Sync()
	// return ErrNotImplemented // TODO(M1)
}

// Replay reads the log front-to-back, calling apply for each intact record, and
// stops cleanly at the first torn or short record (see the crc note in record.go).
// This runs inside Open and is the mechanism that makes recovery real.
func (w *WAL) Replay(apply func(Record) error) error {
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(w.file)
	if err != nil {
		return err
	}

	offset := 0
	for offset < len(data) {
		record, n, err := Decode(data[offset:])
		if err != nil {
			break
		}
		if err := apply(record); err != nil {
			return err
		}
		offset += n
	}

	return nil
}

// Truncate resets the log after a memtable flush (M3): once the data is safely in
// an SSTable, the log entries that produced it are redundant and can be dropped.
func (w *WAL) Truncate() error {
	if err := w.file.Truncate(0); err != nil {
		return err
	}
	return w.file.Sync()
}

// Close flushes and closes the underlying file.
func (w *WAL) Close() error {
	if err := w.file.Sync(); err != nil {
		return err
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	return nil
}
