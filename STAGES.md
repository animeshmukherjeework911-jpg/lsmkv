# lsmkv — Stages & Artifacts

Each stage ends in a **shippable artifact** — something that demonstrably works and
that you can point to. Stages are checkpoints, not just steps: you can stop after
Stage 3 and still have a real, persistent key-value store worth talking about.

Rule of the road (same as the README): the gate is a passing test or a runnable
demo, never "I read the chapter." Effort estimates assume ~1 focused hour a day
while working full-time. They are guidance, not deadlines.

**Remaining work, in hours (last updated after Stage 4 shipped and `SSTable.Records()`
landed):**

| Stage | Status | Remaining |
|---|---|---|
| 0–4 | ✅ done | 0 hrs |
| 5 — Compaction (heap-based merge) | ✅ done | 0 hrs |
| 5b — Crash-safe compaction | ✅ done | 0 hrs |
| 6 — Benchmarks & analysis | ✅ done | 0 hrs |
| Capstone | not started | ~1 weekend |
| **Total to full completion** | | **~18.5–23.5 hrs** (+ capstone) |

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
- [x] `TestCrashRecovery_TornWrite` green
- [x] Can explain, out loud, why fsync-before-ack is the whole durability promise.

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
- [x] Un-skip `TestCrashRecovery_TornWrite`, hand-truncate a WAL file mid-record in the test, assert `Open` still succeeds and only the intact records survive

---

## Stage 2 — Working key-value store  ·  ~3–5 days  ·  (M2)
**Finish:** Memtable + tombstones + the read path over the memtable.
`TestPutGet` and `TestDeleteTombstone` green.
**Artifact:** **A usable in-memory KV store, backed by the durable log.** You can
put, get, and delete keys, and a deleted key stays dead. It's not yet on disk, but
it works and it's crash-durable via Stage 1.
**Portfolio value:** medium. The store now *does something*.
- [x] `TestPutGet` green
- [x] `TestDeleteTombstone` green
- [x] A 10-line demo (or test) that puts, gets, deletes, and prints results.

**Implementation checklist:**
- [x] `Memtable` backing storage — a `map[string]Record` is fine to start (`memtable.go`)
- [x] `Memtable.Put` — insert/overwrite, tombstone is just `RecordKind = RecordDelete`, never a map delete (`memtable.go`)
- [x] `Memtable.Get` — a tombstone counts as found; caller must not fall through (`memtable.go`)
- [x] `Memtable.SizeBytes` — running byte count, drives the M3 flush threshold (`memtable.go`)
- [x] `DB.Delete` — writes a `RecordDelete` through the same `wal.Append` → `mem.Put` path as `Put` (`db.go`)
- [x] `DB.Get` — return `(nil, false, nil)` on a tombstone hit, not the tombstone's value

---

## Stage 3 — Persistent store  ·  ~1–2 weeks  ·  (M3)  ·  ⭐ MINIMUM REAL ARTIFACT
**Finish:** Flush the memtable to an immutable SSTable file; truncate the WAL after
flush; read across memtable + on-disk files. `TestPersistenceAcrossReopen` green.
**Artifact:** **A real persistent key-value store.** Data outlives the process — it
lives in SSTable files on disk and comes back after a clean restart. This is the
"it's actually a database now" moment.
**Portfolio value:** HIGH. This is the honest floor you can stop at and still say
"I built a persistent LSM key-value store from scratch" and mean it.
- [x] `TestPersistenceAcrossReopen` green
- [x] Write keys, restart the process, read them back — on disk, no WAL replay needed.
- [x] You can describe your SSTable file format on a whiteboard.

**Implementation checklist:**
- [x] `Memtable.Sorted` — ascending key order, ready to stream to disk (`memtable.go`)
- [x] `SSTable` fields — path, file handle, in-memory sparse index (`sstable.go`)
- [x] `FlushMemtable` — write sorted records + sparse index + footer to a new file (`sstable.go`)
- [x] `SSTable.Get` — binary-search the sparse index to a block, scan within it (`sstable.go`)
- [x] `SSTable.Path` (`sstable.go`)
- [x] `WAL.Truncate` — called right after a successful flush (`wal.go`)
- [x] `DB` grows a flush trigger in `Put` (memtable over `SizeBytes` threshold) that calls `FlushMemtable` then `wal.Truncate`, and appends the new `SSTable` to `db.ssts`
- [x] `DB.Get` — check `mem` first, then `ssts` newest → oldest
- [x] `DB.Open` — load existing SSTable files from `dir` before replaying the WAL
- [x] `DB.Close` — flush the active memtable, close all SSTable file handles and the WAL

---

## Stage 4 — Fast reads at scale  ·  ~1–2 weeks  ·  (M4)  ·  ✅ DONE
**Finish:** Sparse per-block index + bloom filter; newest-wins reads across many
SSTables. (You write the tests for this stage — a bloom-negative timing check.)
**Artifact:** **A read-optimized store.** A `Get` for a missing key *skips* files via
the bloom filter instead of scanning them; a `Get` for a present key binary-searches
to a block via the sparse index. Prove it with a timing test.
**Portfolio value:** HIGH. "Implemented bloom filters and sparse indexing to cut
read amplification" is a real systems sentence.
- [x] A test showing an absent-key `Get` touches far fewer files than a scan would
      (`TestBloomFilterSkipsScanForAbsentKey`, a deterministic `ReadAt`-counter check
      rather than wall-clock timing).
- [x] Newest version of a key always wins across multiple SSTables
      (`TestNewestWinsAcrossMultipleSSTables` — overwrite, fall-through, tombstone shadowing).

**Implementation checklist:**
- [x] Bloom filter built during `FlushMemtable`, serialized into the SSTable file (`bloom.go`, `sstable.go`)
- [x] Sparse index grows a loaded bloom filter (`index_sparse.go`)
- [x] `OpenSSTable` — reload sparse index and bloom filter directly from the footer, without decoding data blocks (`sstable.go`)
- [x] `SSTable.Get` — check the bloom filter first, early-return on a negative before touching the sparse index or data blocks (`index_sparse.go`)
- [x] `db.ssts` ordering (newest-last, per the `db.go` struct comment) verified by test
- [x] Bloom-negative test, plus a data-block-corruption test proving `OpenSSTable` never decodes data blocks on open

---

## Stage 5 — Self-maintaining store  ·  (M5)  ·  ✅ DONE
**Finish:** `Compact` — a k-way merge of SSTables that keeps the newest version of
each key and drops shadowed keys and dead tombstones. (You write the compaction test.)
**Artifact:** **A complete LSM engine.** Write, overwrite, and delete the same key
across several flushes, run compaction, and only the latest value survives — and
disk usage actually goes *down*. This is the full LSM loop, closed.
**Portfolio value:** HIGH. Compaction is the part people hand-wave in interviews;
you'll have written one.
- [x] A test: overwrite + delete a key across flushes, compact, assert only latest survives.
      (`TestCompact_NewestWinsAndTombstonesDropKeys`, `compaction_test.go`)
- [x] Disk footprint measurably shrinks after compaction. (`TestCompact_DiskFootprintShrinks`)

**Implementation checklist:**
- [x] `SSTable.Records()` — decode the data-block region directly (bounded by the footer's
      `sparseIndexOffset`) into every record in ascending key order, without touching the
      sparse index/bloom/footer sections. Prerequisite for compaction, not originally a
      named checklist item. — **~0.5–1 hr, done**
- [x] `Compact` — **heap-based** k-way merge of `inputs` (ordered newest-to-oldest): one
      cursor per input's `Records()` slice, a min-heap keyed by `(Key, input recency)`,
      pop the smallest key across all cursors each step, keep only the first (= newest)
      version seen per key, advance that cursor. Chosen over a load-everything-into-a-map
      merge specifically because it's the technique real LSM engines use — see the
      real-world-fidelity discussion this stage prompted. (`compaction.go`) — **~2.5–3 hrs**
- [x] Drop a key entirely when its newest surviving version is a tombstone (real deletion,
      not just shadowing) — folds into the heap loop above — **~0.5–1 hr**
- [x] Write output via the same `FlushMemtable`-style path: stage the merged records into a
      fresh `*Memtable` and call `FlushMemtable` unchanged, so compacted tables share the
      exact M3/M4 file format for free — **~1–1.5 hrs**
- [x] Delete the old input files (via `SSTable.Path`) once the new merged table is safely
      written — the *basic* mechanism; made crash-safe as part of Stage 5b below — **~0.5 hr**
- [x] Wire a trigger for when `Compact` runs (e.g. `DB` calls it after N flushes) — pick a
      simple policy, don't over-design — **~1 hr** (`DB.maybeCompact`, flat
      `sstableCompactionThreshold`, `db.go`)
- [x] Write the compaction test yourself (no test file provided for this stage) — **~1.5–2 hrs**
- [x] Edge-case buffer (all-same-key across every input, single-input compaction, an
      input that's entirely tombstones) — **~1–1.5 hrs** (`TestCompact_SingleInputDropsTombstones`,
      `TestCompact_AllTombstonesProducesNoOutput`)

**Stage 5: done.**

---

## Stage 5b — Crash-safe compaction  ·  (M5b)  ·  ✅ DONE
**Why this stage exists:** the engine is deliberately single-threaded (no goroutines, no
locking anywhere) — a real, reasonable scope cut for a learning project. But single-threaded
is not the same as crash-safe, and compaction is the one place a crash mid-operation can
actually lose or duplicate data: it reads N files, writes a new one, and deletes the old
ones — three separate durability events with no atomicity between them. Stage 1 already
proved this project takes "survives `kill -9`" seriously for the WAL; compaction deserves
the same treatment, or the crash-safety story for the whole engine is only half-told.
**Finish:** A crash at any point during compaction — before the new file is durable, after
it's durable but before old files are deleted, or mid-deletion — must never lose data and
must never let stale, superseded data resurface.
**Artifact:** **A compaction step that survives `kill -9` like the WAL does.** Same
credibility move as Stage 1, applied to the part of the engine most people don't bother
making crash-safe in a learning project.
**Portfolio value:** HIGH — this is the detail that separates "I implemented compaction"
from "I implemented compaction and thought about what happens when it's interrupted,"
which is exactly the kind of thing a systems interview probes for.
- [x] A test: crash the process (simulated) between writing the new compacted file and
      deleting the old input files — leave both on disk — and assert `Open` + `Get` still
      return correct data, with no duplicate or incorrectly-shadowed results.
      (`TestCompact_SurvivesCrashBeforeInputDeletion`, `compaction_test.go`)
- [x] A test: crash with only a temp/incomplete compaction file present (never renamed into
      place) — assert `Open` ignores or cleans it up, never treats it as a valid SSTable.
      (`TestCompact_OpenCleansUpOrphanedTempFile`)

**Implementation checklist:**
- [x] Write compacted output to a temp path, `Sync`, then atomically `os.Rename` into its
      final filename — the rename is the commit point; nothing before it is visible as a
      real SSTable — **~1 hr** (`compactionTempSuffix`, `Compact`, `compaction.go`)
- [x] Only unlink old input files *after* the rename succeeds — never delete-then-write
      — **~0.5 hr** (mostly a matter of correct ordering in `Compact`, small on its own)
- [x] Sequence-number assignment for the compacted output: it must sort as newer than every
      input it replaces, but not newer than any table created after compaction started —
      this is the subtle correctness piece, worth designing on paper before coding —
      **~1–1.5 hrs**. Turned out to fall out for free: `nextSSTablePath` is computed from
      the on-disk directory state *before* old inputs are deleted, and the engine is
      single-threaded so nothing else can create a table mid-`Compact` — no separate design
      needed.
- [x] `DB.Open` — detect and remove orphaned temp/incomplete compaction files left behind by
      a crash before the rename ever happened — **~1 hr** (`removeOrphanedCompactionTemps`,
      `db.go`)
- [x] The two crash-simulation tests above — **~1.5–2 hrs**
- [x] Edge-case buffer (crash mid-rename, crash after deleting some but not all old inputs)
      — **~1 hr** — covered by the same crash test: both stale inputs and the committed
      output coexist on disk and `Get` still resolves correctly via sequence-number ordering.

**Stage 5b total: ~6–7 hrs**

---

## Stage 6 — Measured & understood  ·  (M6)  ·  ✅ DONE
**Finish:** Un-skip and fill in `BenchmarkPut` / `BenchmarkGet`. Find the bottleneck.
**Artifact:** **A benchmarked engine + a written analysis.** Real throughput and
latency numbers, plus a paragraph naming which layer caps performance and *why*
(fsync? memtable sort? compaction? bloom false-positive rate?).
**Portfolio value:** HIGHEST per word. "I built it" is common; "I built it, measured
it, and can explain its limits" is what a Staff interview is actually listening for.
- [x] `make bench` produces numbers.
- [x] One paragraph: the bottleneck, the evidence, and what you'd change to move it.
      (`BENCHMARKS.md`)

**Implementation checklist:**
- [x] Fill in `BenchmarkPut` — sequential keys first, then random; note the gap (`bench_test.go`) — **~1–1.5 hrs**
- [x] Fill in `BenchmarkGet` — cold store vs warm, bloom filter should show up as savings (`bench_test.go`) — **~1–1.5 hrs**
- [x] Profile (`pprof`) to find the actual bottleneck rather than guessing — likely candidates: `fsync` per `Append`, memtable sort at flush, bloom false-positive rate, compaction I/O — **~1.5–2 hrs**.
      `-cpuprofile` on `BenchmarkPut/Sequential` showed ~zero on-CPU samples despite
      multi-ms wall time (I/O-wait, not compute-bound); `strace -c` confirmed `fsync`
      as 83% of syscall time. Comparing `SSTableHit` to `AbsentKey` in `BenchmarkGet`
      also surfaced a real read-amplification bug in `SSTable.Get` (reads matched-offset
      to EOF instead of the bounded record) — documented, not fixed, since Stage 6 is
      about measuring and understanding, not optimizing.
- [x] Write the named-bottleneck paragraph the stage gate asks for — **~0.5–1 hr**
      (`BENCHMARKS.md`)

**Stage 6: done.** See `BENCHMARKS.md` for numbers + analysis.

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
Stages 4–5 make it **good**. Stage 5b makes it **trustworthy under a crash, not just
correct on the happy path** — the same promise Stage 1 made for the WAL, now made for
compaction too. Stage 6 makes it **understood**. The capstone makes it **yours, and
visible.**
