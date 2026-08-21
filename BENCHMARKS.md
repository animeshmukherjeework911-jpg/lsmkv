# Benchmarks (M6)

Reproduce with `make bench` (`go test ./... -run xxx -bench . -benchmem`).

Machine: Intel Core 5 120U, Linux, local disk. Numbers below from
`-benchtime=200ms`; treat magnitudes as directional, not absolute.

```
BenchmarkPut/Sequential-12         98    2359074 ns/op     418 B/op    5 allocs/op
BenchmarkPut/Random-12             92    2442958 ns/op     423 B/op    3 allocs/op
BenchmarkGet/MemtableHit-12  24163698          9.683 ns/op    0 B/op    0 allocs/op
BenchmarkGet/SSTableHit-12       4314      58054 ns/op  337073 B/op   11 allocs/op
BenchmarkGet/AbsentKey-12      140217       1647 ns/op    1040 B/op    5 allocs/op
```

## The bottleneck: `WAL.Append`'s fsync, not the memtable

`Put` costs ~2.4ms regardless of whether keys are sequential or random — a ~250,000x
gap over `BenchmarkGet/MemtableHit`'s 9.7ns. That rules out the memtable (a plain Go
map insert) as the cost driver; if it were doing the work, random keys would be
slower due to hash-table probing patterns, and neither would be measured in
milliseconds. A `-cpuprofile` run over `BenchmarkPut/Sequential` came back with
**zero CPU samples across a 409ms run** — the process isn't burning cycles, it's
blocked. `strace -c` on the same benchmark confirms where: `fsync` accounts for 83%
of traced syscall time, `write` the other 17%. `Put`'s cost is `WAL.Append`'s
`file.Sync()` call — one fsync-to-disk round trip per write, by design (that's the
durability promise: "if `Put` returned nil, the write survives a `kill -9` one
nanosecond later"). Sequential vs. random makes no difference because the disk
doesn't care about key order; it only cares how many fsync calls happen, and that's
one per `Put` either way.

**What would move this:** the only way to cut this without breaking the durability
contract is fewer fsyncs, not cheaper ones — group-commit / batched `Append` (fsync
once per batch of pending writes instead of once per `Put`) is the standard fix, at
the cost of a small window where an acknowledged write depends on a batch of others
also having synced. Out of scope for this project's single-threaded, no-batching
design, but it's the lever a production engine pulls here.

## A real finding on the read path: `SSTable.Get` over-reads

`SSTableHit` moves **337KB per operation** to resolve one ~90-byte record — that's
why it's 6,000x slower than the equivalent bloom-filter *miss* (`AbsentKey`, 1040
B/op), even though both paths check the same 5 SSTables. The reason is in
`SSTable.Get` ([sstable.go](sstable.go)):

```go
buf := make([]byte, info.Size()-offset)
if _, err := s.file.ReadAt(buf, offset); err != nil { ... }
record, _, err := Decode(buf)
```

It reads from the matched offset all the way to **end of file** — every record
after the one it wants, plus the sparse index, bloom filter, and footer — then
decodes only the first record out of that buffer. For a key near the start of an
old, large table this is severe read amplification. The fix doesn't need a format
change: `Record`'s on-disk header (`record.go`) already stores `keyLen`/`valLen` in
its first 13 bytes, so `Get` could read the fixed header first, compute the exact
record length, and issue a second bounded `ReadAt` for just that many bytes —
turning a whole-tail read into two small ones.

`AbsentKey` being ~35x faster than `SSTableHit` (1.6μs vs. 58μs) is the bloom
filter doing exactly its documented job: reject before ever touching the disk. The
gap would be even larger once `SSTable.Get` stops over-reading on the hit path,
which is precisely why the read-amplification bug above was invisible until this
benchmark existed — bloom rejects always looked fast in isolation; only comparing
them to a real hit exposed how much extra a hit was paying for.

## Compared to a real-world LSM: goleveldb

bbolt (below) is a B+tree, so it's a useful contrast but not a fair
apples-to-apples comparison — its whole *shape* trades differently against
disk. [goleveldb](https://github.com/syndtr/goleveldb) is a complete,
production-used reimplementation of LevelDB in pure Go: same log-structured
shape as `lsmkv` (WAL, memtable, immutable SSTables, background compaction),
same language, same machine, same `-benchtime=200ms`, `WriteOptions{Sync:
true}` to match `lsmkv`'s one-fsync-per-write contract:

```
                          lsmkv                    goleveldb
Put/Sequential      2.36 ms/op,   418 B/op    8.39 ms/op,  379 B/op,  6 allocs/op
Put/Random          2.44 ms/op,   423 B/op    7.33 ms/op,  255 B/op,  3 allocs/op
Get/Hit             58.1 μs/op*, 337 KB/op*   898  ns/op,  744 B/op, 13 allocs/op
Get/AbsentKey       1.65 μs/op,  1040 B/op    188  ns/op,  136 B/op,  4 allocs/op
```
\* `lsmkv`'s worst-case hit (key only in the oldest of 5 tables), inflated by
the read-amplification bug documented above.

**`lsmkv`'s writes are faster than a real LSM's here** — surprising until you
remember what `goleveldb` is doing that this project isn't: a write-ahead log
entry AND manifest/version-set bookkeeping AND (once a memtable fills)
background compaction coordination, all behind that one `Sync: true` call.
`lsmkv`'s WAL append is closer to the theoretical floor for "durably persist
one record" — goleveldb pays extra for durability *guarantees this project
doesn't make* (atomic multi-file manifest updates, snapshot isolation, crash
recovery across a much larger state machine). This is a real, honest
trade-off, not a bug on either side: `lsmkv` is faster here because it does
less.

**`goleveldb`'s reads are ~9x-to-65x faster, and that gap is the real
finding.** Some of it is the same bug already documented (`SSTableHit`'s
337KB read amplification) — but even comparing `AbsentKey` to `AbsentKey`
(no bug in that path, pure bloom-filter-reject cost on both sides),
goleveldb is still ~9x faster (188ns vs 1.65μs). `SSTable.Get` in this repo
calls `file.Stat()` on *every* lookup before even consulting the bloom
filter (`sstable.go`) — a syscall paid on every miss, when the file size
practically never changes between reads and could just be cached at
`OpenSSTable` time instead. A production engine like goleveldb caches
aggressively (block cache, open file handles, precomputed metadata) for
exactly this reason: syscalls are expensive relative to an in-memory bloom
check, and a real engine amortizes that cost once per file open, not once
per `Get`.

## Compared to a real embedded store: bbolt

Numbers from someone else's blog post are a different machine, different disk,
different fsync settings — not a fair comparison. So instead: [bbolt](https://github.com/etcd-io/bbolt)
(etcd's B+tree store, pure Go, no cgo) benchmarked the same way, on the same
machine, same value size, same one-fsync-per-write durability contract, same
`-benchtime=200ms` run:

```
                          lsmkv (LSM)              bbolt (B+tree)
Put/Sequential      2.43 ms/op,  431 B/op    3.34 ms/op, 12645 B/op, 51 allocs/op
Put/Random          3.23 ms/op,  406 B/op    2.50 ms/op, 12403 B/op, 51 allocs/op
Get (warm, on disk)  133 μs/op*, 337 KB/op*    734 ns/op,   576 B/op,  9 allocs/op
```
\* `lsmkv`'s `SSTableHit` number — deliberately the worst case (key only in the
oldest of 5 SSTables) and inflated by the read-amplification bug documented
above; a bloom-filter miss (`AbsentKey`) on the same store is ~2.9μs.

**Writes land in the same place, for the reason you'd expect.** Both stores pay
one `fsync` per write, and fsync latency is a disk-hardware fact neither
storage structure can out-design — this benchmark's ~2.5–3.3ms is this
particular disk's fsync cost, full stop. Any B+tree vs. LSM write-throughput
argument really shows up under *batched* commits or concurrent writers, which
neither benchmark here does (both are one write, one fsync, no batching).

**Reads are where the architectures actually diverge, and bbolt wins here by
a lot.** A B+tree lookup is O(log n) page reads against a single file that's
mostly resident in the OS page cache after the initial load — 734ns is
basically "walk a few in-memory pages." lsmkv's LSM design pays an inherent
LSM tax on reads: `Get` may have to consult *multiple* SSTables (newest to
oldest) before finding — or ruling out — a key, and this store's `Get` adds a
self-inflicted over-read on top of that inherent cost (see above). Even
without that bug, an LSM's read path is structurally more expensive than a
B+tree's for a store this size; the trade lsmkv is making is *write* latency
and throughput (sequential appends to an immutable file, no in-place page
splits) in exchange for read complexity — the same trade every real LSM engine
(LevelDB, RocksDB) makes, and why they all lean so heavily on bloom filters
and multi-level compaction to buy back the read cost. At small scale, a
B+tree like bbolt is a perfectly reasonable, often better, choice; the LSM
shape earns its keep at write-heavy scale and against slower/spinning disks,
where turning random writes into sequential ones matters more than it does on
this machine's disk.

## Compaction

Not separately benchmarked here — `Compact`'s cost is dominated by the same forces
already measured: one `FlushMemtable` write+fsync for the output (`BenchmarkPut`-shaped
cost, scaled to the merged size) plus `os.Remove` + directory fsync per input file.
`TestCompact_DiskFootprintShrinks` (`compaction_test.go`) already demonstrates the
payoff — shrunk disk footprint — that justifies paying that cost periodically rather
than on every write.
