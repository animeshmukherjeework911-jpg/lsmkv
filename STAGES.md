# lsmkv — Stages & Artifacts

Each stage ends in a **shippable artifact** — something that demonstrably works and
that you can point to. Stages are checkpoints, not just steps: you can stop after
Stage 3 and still have a real, persistent key-value store worth talking about.

Rule of the road (same as the README): the gate is a passing test or a runnable
demo, never "I read the chapter." Effort estimates assume ~1 focused hour a day
while working full-time. They are guidance, not deadlines.

---

## Stage 0 — Setup  ·  ~1 hour
**Finish:** `make build` compiles, `make test` runs and fails cleanly.
**Artifact:** A compiling project with a red test suite — i.e. a *spec you can see*.
**Portfolio value:** none (this is the start line).
- [ ] `make build` green, `make test` red, `make todo` shows the work left.

---

## Stage 1 — Durable log  ·  ~1 week  ·  (milestone M1)
**Finish:** WAL append + fsync + replay. `TestCrashRecovery_WALReplay` green, then
un-skip and pass `TestCrashRecovery_TornWrite`.
**Artifact:** **A write-ahead log that survives `kill -9`.** A record that reached
the log is still there after a simulated crash; a half-written (torn) record is
ignored on replay.
**Portfolio value:** HIGH. "I built a durable WAL with crash recovery, including
torn-write detection via CRC." This is your correctness home turf and the single
most credible thing in the whole project.
- [x] `TestCrashRecovery_WALReplay` green
- [ ] `TestCrashRecovery_TornWrite` green (still skipped)
- [ ] Can explain, out loud, why fsync-before-ack is the whole durability promise.

**Implementation checklist:**
- [x] `WAL.file` populated in `OpenWAL` (`wal.go`)
- [x] `WAL.Append` — encode, write, fsync (`wal.go`)
- [x] `WAL.Replay` — read, decode loop, stop clean on torn tail (`wal.go`)
- [x] `WAL.Close` (`wal.go`)
- [x] `Memtable.Put` / `Memtable.Get` — map-backed, tracks size via `Record.GetRecordLen` (`memtable.go`) — used as `Replay`'s `apply` target
- [x] `DB.Open` — create/open `dir`, open WAL, build a fresh `Memtable`, `Replay` into it (`db.go`)
- [x] `DB.Put` — `wal.Append` then `mem.Put`, per the write-path doc comment (`db.go`)
- [x] `DB.Get` — reads from `mem`, tombstone-aware (`db.go`)
- [x] `DB.Delete` — tombstone via `wal.Append` + `mem.Put`, done ahead of schedule (belongs to Stage 2 but already correct) (`db.go`)
- [ ] Un-skip `TestCrashRecovery_TornWrite`, hand-truncate a WAL file mid-record in the test, assert `Open` still succeeds and only the intact records survive — **last item blocking Stage 1**

---

## Stage 2 — Working key-value store  ·  ~3–5 days  ·  (M2)
**Finish:** Memtable + tombstones + the read path over the memtable.
`TestPutGet` and `TestDeleteTombstone` green.
**Artifact:** **A usable in-memory KV store, backed by the durable log.** You can
put, get, and delete keys, and a deleted key stays dead. It's not yet on disk, but
it works and it's crash-durable via Stage 1.
**Portfolio value:** medium. The store now *does something*.
- [ ] `TestPutGet` green
- [ ] `TestDeleteTombstone` green
- [ ] A 10-line demo (or test) that puts, gets, deletes, and prints results.

**Implementation checklist:**
- [ ] `Memtable` backing storage — a `map[string]Record` is fine to start (`memtable.go`)
- [ ] `Memtable.Put` — insert/overwrite, tombstone is just `RecordKind = RecordDelete`, never a map delete (`memtable.go`)
- [ ] `Memtable.Get` — a tombstone counts as found; caller must not fall through (`memtable.go`)
- [ ] `Memtable.SizeBytes` — running byte count, drives the M3 flush threshold (`memtable.go`)
- [ ] `DB.Delete` — writes a `RecordDelete` through the same `wal.Append` → `mem.Put` path as `Put` (`db.go`)
- [ ] `DB.Get` — return `(nil, false, nil)` on a tombstone hit, not the tombstone's value

---

## Stage 3 — Persistent store  ·  ~1–2 weeks  ·  (M3)  ·  ⭐ MINIMUM REAL ARTIFACT
**Finish:** Flush the memtable to an immutable SSTable file; truncate the WAL after
flush; read across memtable + on-disk files. `TestPersistenceAcrossReopen` green.
**Artifact:** **A real persistent key-value store.** Data outlives the process — it
lives in SSTable files on disk and comes back after a clean restart. This is the
"it's actually a database now" moment.
**Portfolio value:** HIGH. This is the honest floor you can stop at and still say
"I built a persistent LSM key-value store from scratch" and mean it.
- [ ] `TestPersistenceAcrossReopen` green
- [ ] Write keys, restart the process, read them back — on disk, no WAL replay needed.
- [ ] You can describe your SSTable file format on a whiteboard.

**Implementation checklist:**
- [ ] `Memtable.Sorted` — ascending key order, ready to stream to disk (`memtable.go`)
- [ ] `SSTable` fields — path, file handle, in-memory sparse index (`sstable.go`)
- [ ] `FlushMemtable` — write sorted records + sparse index + footer to a new file (`sstable.go`)
- [ ] `SSTable.Get` — binary-search the sparse index to a block, scan within it (`sstable.go`)
- [ ] `SSTable.Path` (`sstable.go`)
- [ ] `WAL.Truncate` — called right after a successful flush (`wal.go`)
- [ ] `DB` grows a flush trigger in `Put` (memtable over `SizeBytes` threshold) that calls `FlushMemtable` then `wal.Truncate`, and appends the new `SSTable` to `db.ssts`
- [ ] `DB.Get` — check `mem` first, then `ssts` newest → oldest
- [ ] `DB.Open` — load existing SSTable files from `dir` before replaying the WAL
- [ ] `DB.Close` — flush the active memtable, close all SSTable file handles and the WAL

---

## Stage 4 — Fast reads at scale  ·  ~1–2 weeks  ·  (M4)
**Finish:** Sparse per-block index + bloom filter; newest-wins reads across many
SSTables. (You write the tests for this stage — a bloom-negative timing check.)
**Artifact:** **A read-optimized store.** A `Get` for a missing key *skips* files via
the bloom filter instead of scanning them; a `Get` for a present key binary-searches
to a block via the sparse index. Prove it with a timing test.
**Portfolio value:** HIGH. "Implemented bloom filters and sparse indexing to cut
read amplification" is a real systems sentence.
- [ ] A test showing an absent-key `Get` touches far fewer files than a scan would.
- [ ] Newest version of a key always wins across multiple SSTables.

**Implementation checklist:**
- [ ] Bloom filter built during `FlushMemtable`, serialized into the SSTable file (`sstable.go`)
- [ ] `SSTable` fields grow a loaded bloom filter (`sstable.go`)
- [ ] `OpenSSTable` — reload sparse index and bloom filter into memory (`sstable.go`)
- [ ] `SSTable.Get` — check the bloom filter first, early-return on a negative before touching the sparse index or data blocks (`sstable.go`)
- [ ] `db.ssts` ordering decision (newest-first, per the `db.go` struct comment) actually enforced wherever tables are appended
- [ ] Write the bloom-negative timing test yourself (no test file provided for this stage)

---

## Stage 5 — Self-maintaining store  ·  ~1–2 weeks  ·  (M5)
**Finish:** `Compact` — a k-way merge of SSTables that keeps the newest version of
each key and drops shadowed keys and dead tombstones. (You write the compaction test.)
**Artifact:** **A complete LSM engine.** Write, overwrite, and delete the same key
across several flushes, run compaction, and only the latest value survives — and
disk usage actually goes *down*. This is the full LSM loop, closed.
**Portfolio value:** HIGH. Compaction is the part people hand-wave in interviews;
you'll have written one.
- [ ] A test: overwrite + delete a key across flushes, compact, assert only latest survives.
- [ ] Disk footprint measurably shrinks after compaction.

**Implementation checklist:**
- [ ] `Compact` — k-way merge of `inputs` (ordered newest-to-oldest), keep first-seen (i.e. newest) version of each key (`compaction.go`)
- [ ] Drop a key entirely when its newest surviving version is a tombstone (real deletion, not just shadowing)
- [ ] Write output via the same `FlushMemtable`-style path so compacted tables share the M3/M4 file format
- [ ] Delete the old input files (via `SSTable.Path`) once the new merged table(s) are safely written
- [ ] Wire a trigger for when `Compact` runs (e.g. `DB` calls it after N flushes) — pick a simple policy, don't over-design
- [ ] Write the compaction test yourself (no test file provided for this stage)

---

## Stage 6 — Measured & understood  ·  ~3–5 days  ·  (M6)
**Finish:** Un-skip and fill in `BenchmarkPut` / `BenchmarkGet`. Find the bottleneck.
**Artifact:** **A benchmarked engine + a written analysis.** Real throughput and
latency numbers, plus a paragraph naming which layer caps performance and *why*
(fsync? memtable sort? compaction? bloom false-positive rate?).
**Portfolio value:** HIGHEST per word. "I built it" is common; "I built it, measured
it, and can explain its limits" is what a Staff interview is actually listening for.
- [ ] `make bench` produces numbers.
- [ ] One paragraph: the bottleneck, the evidence, and what you'd change to move it.

**Implementation checklist:**
- [ ] Fill in `BenchmarkPut` — sequential keys first, then random; note the gap (`bench_test.go`)
- [ ] Fill in `BenchmarkGet` — cold store vs warm, bloom filter should show up as savings (`bench_test.go`)
- [ ] Profile (`pprof`) to find the actual bottleneck rather than guessing — likely candidates: `fsync` per `Append`, memtable sort at flush, bloom false-positive rate, compaction I/O
- [ ] Write the named-bottleneck paragraph the stage gate asks for

---

## Capstone — the durable artifact  ·  ~1 weekend
**Finish:** Push to GitHub with a README that explains the design; write one short
post (even 400 words) on what you learned building it.
**Final artifact of the whole project:**
1. **A finished, tested, benchmarked, documented LSM storage engine in Go, public on
   your GitHub.** A thing that is *yours* and cannot be obsoleted by anyone's reorg.
2. **A write-up** that turns it into a story you can tell — and a "teach publicly" rep.
3. **Readiness to read QuestDB's storage layer** and go land issue #5041 — the same
   engine, in production, in Java.
- [ ] Repo public with a real README.
- [ ] Short write-up published or drafted.
- [ ] Open QuestDB's storage code and recognise what you're looking at.

---

### The shape, in one line
Stages 1–3 make it **real** (stop here and you still have something honest).
Stages 4–6 make it **good**. The capstone makes it **yours, and visible.**
