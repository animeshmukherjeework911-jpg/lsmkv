package lsmkv

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

// ErrNotImplemented is returned by every stub in this repo.
// Use it as your progress bar: when `grep -rn ErrNotImplemented .` and
// `grep -rn "TODO(" .` both come back empty, the engine is finished.
var ErrNotImplemented = errors.New("not implemented — see README milestone")

// ErrShortRecord means b did not contain a complete, valid record — either
// there weren't enough bytes, or the crc didn't match. Both cases mean the
// same thing to a WAL replay loop: stop here, this is the torn tail, not
// a real error.
var ErrShortRecord = errors.New("short or corrupt record")

// RecordKind distinguishes a normal write from a delete.
type RecordKind uint8

const (
	RecordSet    RecordKind = 0
	RecordDelete RecordKind = 1 // a "tombstone" — a delete is a WRITE, never an erase
)

// A Record is a single logical operation. The exact same struct travels through
// the WAL and the SSTable, which is what keeps the whole design honest.
//
// On-disk layout you will implement in M1 (a boring, explicit starting point):
//
//	+--------+--------+--------+-----------+--------------+----------------+
//	| crc32  | keyLen | valLen | kind (1B) | key (keyLen) | value (valLen) |
//	| uint32 | uint32 | uint32 | 0=set 1=del                              |
//	+--------+--------+--------+-----------+--------------+----------------+
//
// The crc is the durability tripwire. A torn write — process killed mid-append —
// leaves a trailing record whose crc will not match. Replay MUST treat that as a
// clean end-of-log, not a crash. That one rule is ~80% of "crash safe".
type Record struct {
	Key   []byte
	Value []byte
	Kind  RecordKind
}

func (r Record) GetRecordLen() int {
	return len(r.Key) + len(r.Value)
}

// Encode serialises a record to the layout documented above.
func Encode(r Record) ([]byte, error) {
	keyLen := len(r.Key)
	valLen := len(r.Value)

	payload := make([]byte, 4+4+1+keyLen+valLen)
	binary.LittleEndian.PutUint32(payload[0:4], uint32(keyLen))
	binary.LittleEndian.PutUint32(payload[4:8], uint32(valLen))
	payload[8] = byte(r.Kind)
	copy(payload[9:9+keyLen], r.Key)
	copy(payload[9+keyLen:], r.Value)

	crc := crc32.ChecksumIEEE(payload)

	buf := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], crc)
	copy(buf[4:], payload)

	return buf, nil
}

// Decode reads one record from the front of b and reports how many bytes it
// consumed. On a crc mismatch or a short buffer it must return an error the WAL
// replay loop can recognise as "stop here, cleanly".
func Decode(b []byte) (rec Record, n int, err error) {
	const headerLen = 4 + 4 + 4 + 1 // crc + keyLen + valLen + kind

	if len(b) < headerLen {
		return Record{}, 0, ErrShortRecord
	}

	storedCrc := binary.LittleEndian.Uint32(b[0:4])
	keyLen := binary.LittleEndian.Uint32(b[4:8])
	valLen := binary.LittleEndian.Uint32(b[8:12])
	kind := b[12]

	total := headerLen + int(keyLen) + int(valLen)
	if len(b) < total {
		return Record{}, 0, ErrShortRecord
	}

	payload := b[4:total]
	if crc32.ChecksumIEEE(payload) != storedCrc {
		return Record{}, 0, ErrShortRecord
	}

	key := make([]byte, keyLen)
	copy(key, b[headerLen:headerLen+int(keyLen)])
	value := make([]byte, valLen)
	copy(value, b[headerLen+int(keyLen):total])

	rec = Record{
		Key:   key,
		Value: value,
		Kind:  RecordKind(kind),
	}
	return rec, total, nil
}
