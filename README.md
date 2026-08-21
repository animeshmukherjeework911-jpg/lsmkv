# lsmkv

A log-structured merge (LSM) key-value store in Go, built from scratch: durable
write-ahead log, sorted in-memory memtable, immutable on-disk SSTables with a
sparse index and bloom filter, and a heap-based compactor that is itself
crash-safe. Same shape as LevelDB, RocksDB, and the storage layer under QuestDB —
stripped to the idea, with no dependencies beyond the Go standard library.

## Architecture

```
Put(k, v) / Delete(k)
        │
        ▼
   WAL.Append ── fsync ──▶ durable the instant Put returns
        │
        ▼
  Memtable.Put ──▶ visible to Get immediately (sorted map, in memory)
        │
        │  size > threshold
        ▼
  FlushMemtable ──▶ new immutable SSTable on disk, WAL truncated
        │
        │  SSTable count > threshold
        ▼
     Compact ──▶ heap-merges every SSTable into one, drops shadowed
                  keys and dead tombstones, deletes the old files

Get(k):  Memtable  →  SSTables, newest → oldest (bloom-filter early-out)  →  miss
```

- **Durability boundary is the WAL.** `Put` and `Delete` append + fsync before
  returning — a `kill -9` immediately after either call still leaves the write
  recoverable via WAL replay on the next `Open`.
- **Deletes are writes.** A `Delete` appends a tombstone record; the key isn't
  actually erased until compaction runs and confirms no older version of it
  survives the merge. Get treats a tombstone as a hard "not found," so a deleted
  key never resurrects by falling through to an older SSTable.
- **SSTables are immutable.** Once flushed, a file is only ever read or deleted
  in full — never edited in place. That invariant is what keeps concurrent-free
  correctness simple everywhere else in the engine.
- **Compaction is crash-safe end to end.** The merged output is written to a
  `.tmp` file, fsynced, and atomically renamed into place — the rename is the
  single commit point. Old input files are only unlinked after that rename
  succeeds, and the containing directory is fsynced after both the rename and
  the deletions, since POSIX doesn't guarantee a directory entry change is
  durable until the directory itself is synced. `Open` scans for and removes
  any orphaned `.tmp` file left behind by a crash that landed before the
  rename ever committed. See `TestCompact_SurvivesCrashBeforeInputDeletion`
  and `TestCompact_OpenCleansUpOrphanedTempFile` in `compaction_test.go`.

## On-disk format

Every record — in the WAL and inside an SSTable — uses the same layout:

```
+--------+--------+--------+-----------+--------------+----------------+
| crc32  | keyLen | valLen | kind (1B) | key (keyLen) | value (valLen) |
| uint32 | uint32 | uint32 | 0=set 1=del                              |
+--------+--------+--------+-----------+--------------+----------------+
```

The CRC is the crash-safety tripwire: a torn write (process killed mid-append)
leaves a trailing record whose checksum won't match, and WAL replay treats that
as a clean end-of-log rather than a fatal error — that one rule does most of the
work of "crash safe."

An SSTable file is:

```
[ sorted data blocks ] [ sparse index ] [ bloom filter ] [ footer ]
```

- **Sparse index** — one key per `sparseIndexInterval` records, so `Get`
  binary-searches to the nearest preceding block instead of scanning the whole
  file.
- **Bloom filter** — lets `Get` skip a file entirely when the key is
  definitely absent, without touching disk at all. This is the single biggest
  lever on read latency in a multi-SSTable store; see `BENCHMARKS.md`.
- **Footer** — fixed-size trailer pointing at the sparse index and bloom
  filter offsets, so both can be reconstructed on `Open` without decoding the
  data blocks.

## Usage

```go
db, err := lsmkv.Open("/path/to/db-dir")
if err != nil {
    log.Fatal(err)
}
defer db.Close()

db.Put([]byte("lang"), []byte("go"))
v, ok, err := db.Get([]byte("lang")) // v == "go", ok == true

db.Delete([]byte("lang"))
_, ok, _ = db.Get([]byte("lang")) // ok == false
```

`Compact` is triggered automatically once the SSTable count crosses
`sstableCompactionThreshold` (see `DB.maybeCompact` in `db.go`) — no manual
call needed in normal operation. It's also exported directly if you want to
drive it yourself: `lsmkv.Compact(inputs []*SSTable, outDir string) ([]*SSTable, error)`.

## Performance

`make bench` runs `BenchmarkPut` (sequential vs. random keys) and
`BenchmarkGet` (memtable hit, SSTable hit, bloom-filter miss). Full numbers,
CPU-profile evidence, and analysis are in **`BENCHMARKS.md`** — short version:
`Put` is fsync-bound (not memtable-bound: sequential and random keys cost the
same), and comparing a real SSTable hit against a bloom-filter miss surfaced a
genuine read-amplification bug in `SSTable.Get`, documented there with the fix.

## Status

All planned stages are complete:

| Stage | What it delivers |
|---|---|
| 0–2 | WAL with fsync + crash-safe replay; memtable; tombstoned deletes |
| 3–4 | SSTable flush/reopen; sparse index + bloom filter for fast multi-file reads |
| 5 | Heap-based k-way compaction; newest-wins merge; dead tombstones dropped for real |
| 5b | Crash-safe compaction (temp file + atomic rename + orphan cleanup) |
| 6 | Benchmarked, profiled, bottleneck identified and documented |

Run `make todo` to confirm: zero `TODO`s, zero `ErrNotImplemented`.

## Commands

```
make build      # compile
make test       # full test suite
make recovery   # just the crash-recovery tests, verbose
make bench      # benchmarks (see BENCHMARKS.md)
make todo       # progress check — should read zero
```

Requires Go 1.21+. No dependencies outside the standard library, on purpose.

## Deliberate scope cuts

Single-threaded, no goroutines or locking anywhere — a reasonable cut for a
learning-scale engine, made honest by taking crash-safety seriously everywhere
single-threadedness doesn't already give it to you for free (that's the whole
point of Stage 5b). Also out of scope by design: leveled/tiered compaction
strategies (this engine does flat "merge everything past N tables"), on-disk
format versioning, and multi-writer concurrency. A real engine like LevelDB or
the storage layer under QuestDB adds exactly these on top of the same core
shape implemented here.
