# lsmkv — a log-structured key-value store, built end to end

This is a scaffold, not a solution. Every real file compiles but does nothing —
it returns `ErrNotImplemented` or panics with a `TODO`. Your job is to make the
red tests green, one milestone at a time. When `make todo` reports zero, you have
built a storage engine and you understand it, because you wrote every byte of it.

The design is a small LSM (log-structured merge) engine: a durable write-ahead
log in front of an in-memory sorted buffer that periodically flushes to immutable
sorted files on disk, which are occasionally merged. It is the same shape as
LevelDB, RocksDB, and the storage layer under QuestDB — just stripped to the idea.

---

## How to work this repo (read this once, then don't argue with it)

1. **Goals are build outcomes, never pages read.** "Today the WAL replays a write
   after a simulated crash" — not "today I read the recovery chapter." If your
   goal can be satisfied by reading, it's the wrong goal.
2. **One milestone at a time, in order.** Each depends on the one before it. Don't
   start M3 because M1 got hard. Finish M1.
3. **Read just-in-time.** Open the book for the piece you're building this week,
   not front to back. The reading pairings below tell you exactly what to open.
4. **Keep the daily unit small.** This is a deliberate, sustainable build, not a
   sprint. A good day moves one test from red to green. That's enough.
5. **Never edit a test to pass it.** The test encodes the contract. Satisfy it.

Progress check any time: `make todo`.

---

## The build-and-fuel loop

Weeks are a guide, not a deadline. **Weeks 1–2 are M0–M2** — that's your concrete
starting slice; everything after is the map for when you get there.

| # | Build outcome (the gate test that goes green) | Prove it / break it | Read (just-in-time) | Watch |
|---|---|---|---|---|
| **M0** | Repo builds, tests run and fail cleanly. `make build` ok, `make test` red. | — | DDIA ch.3 *once*, for the vocabulary (WAL, memtable, SST, compaction). | CMU 15-445 *Database Storage* lecture(s). |
| **M1** | `TestCrashRecovery_WALReplay` green. Append + fsync + replay. | Kill the process (the test abandons the handle); reopen; the write is still there. Then un-skip the torn-write test. | Petrov *Database Internals*, Part 1: the log / recovery material. | CMU 15-445 *Logging & Recovery*. |
| **M2** | `TestPutGet` + `TestDeleteTombstone` green. Memtable + tombstones + read path over memtable. | Delete a key; confirm it stays dead. | Petrov, Part 1: memtable / LSM intro. | CMU 15-445 *Log-Structured Storage*. |
| **M3** | `TestPersistenceAcrossReopen` green. Flush memtable → SSTable; truncate WAL; read across mem + files. | Reopen after a clean close; data is on disk, WAL is empty. | Petrov, Part 1: SSTable / on-disk file format. | CMU 15-445 *Storage Models / file layout*. |
| **M4** | Reads stay fast with many SST files. Sparse index + bloom filter; newest-wins across files. | Add a bloom-negative timing check; a missing key should skip files, not scan them. | Bloom filter primer; Petrov index material. | CMU 15-445 *Index / hash* material. |
| **M5** | `Compact` merges files, drops shadowed keys and dead tombstones. | Write, overwrite, delete the same key across flushes; after compaction only the latest survives. | Petrov: compaction strategies (size-tiered vs leveled). | CMU 15-445 *Log-Structured / compaction*. |
| **M6** | Benchmarks run; you can explain the bottleneck. | `make bench`; write down which layer caps throughput and why. | DDIA ch.3 (re-read — it lands differently now). | — |

---

## After it works — the mirrors (not before)

Only once your engine is correct, read a real one to see what production adds that
you punted on (concurrency, corruption handling, format versioning):

- **boltdb** (Go, B-tree/page world) — small and famously readable.
- **LevelDB** (C++, the LSM world) — the canonical version of what you just built.

Read them to answer a specific question you now have, not cover to cover.

## The finale

Then take the QuestDB contribution (issue #5041) — it's already cloned in your home
directory. After building this, its storage and interval code will read like a
language you speak — and that merge is your code, running in a database other
people run in production. Permanently.

---

## Commands

```
make build      # compile
make test       # all gate tests (red until you build them)
make recovery   # just the crash tests, verbose
make bench      # M6
make todo       # how much is left
```

Requires Go 1.21+. Nothing else — no dependencies, on purpose.
