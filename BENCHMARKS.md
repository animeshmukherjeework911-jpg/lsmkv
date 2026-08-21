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

## Compaction

Not separately benchmarked here — `Compact`'s cost is dominated by the same forces
already measured: one `FlushMemtable` write+fsync for the output (`BenchmarkPut`-shaped
cost, scaled to the merged size) plus `os.Remove` + directory fsync per input file.
`TestCompact_DiskFootprintShrinks` (`compaction_test.go`) already demonstrates the
payoff — shrunk disk footprint — that justifies paying that cost periodically rather
than on every write.
