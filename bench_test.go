package lsmkv

import (
	"fmt"
	"math/rand"
	"testing"
)

// M6 — find the engine's limits. Fill these in LAST, never before the store is
// correct. The number itself is worthless; being able to explain WHY it's that
// number — which layer is the bottleneck and why — is the entire deliverable.

var benchValue = []byte("the-quick-brown-fox-jumps-over-the-lazy-dog-0123456789")

// BenchmarkPut compares sequential-key throughput against random-key
// throughput. A B-tree-backed store would show a real gap here (random
// insertion order fragments its page layout); an LSM's memtable is a plain
// map, so Put's own cost is order-independent — any gap you see is coming
// from elsewhere (the WAL fsync dominates both, see BENCHMARKS.md).
func BenchmarkPut(b *testing.B) {
	b.Run("Sequential", func(b *testing.B) {
		dir := b.TempDir()
		db, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := []byte(fmt.Sprintf("key-%010d", i))
			if err := db.Put(key, benchValue); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()

		if err := db.Close(); err != nil {
			b.Fatal(err)
		}
	})

	b.Run("Random", func(b *testing.B) {
		dir := b.TempDir()
		db, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}

		r := rand.New(rand.NewSource(1))
		keys := make([][]byte, b.N)
		for i := range keys {
			keys[i] = []byte(fmt.Sprintf("key-%010d", r.Int63n(1<<62)))
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := db.Put(keys[i], benchValue); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()

		if err := db.Close(); err != nil {
			b.Fatal(err)
		}
	})
}

// benchTableKey names the i-th key written into bench table t, so keys sort
// and group predictably: table 0 holds the numerically smallest keys, and
// every table's key range is disjoint from every other's.
func benchTableKey(table, i int) string {
	return fmt.Sprintf("key-%02d-%06d", table, i)
}

// buildBenchStore writes `tables` SSTables of `perTable` records each,
// directly via FlushMemtable — bypassing DB.Put's flush-size threshold so the
// benchmark controls table count and size exactly, the way a warmed-up,
// already-compacted store would look in production.
func buildBenchStore(b *testing.B, tables, perTable int) string {
	b.Helper()
	dir := b.TempDir()
	for t := 0; t < tables; t++ {
		m := NewMemtable()
		for i := 0; i < perTable; i++ {
			m.Put(Record{
				Key:   []byte(benchTableKey(t, i)),
				Value: benchValue,
				Kind:  RecordSet,
			})
		}
		path, err := nextSSTablePath(dir)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := FlushMemtable(m, path); err != nil {
			b.Fatal(err)
		}
	}
	return dir
}

// BenchmarkGet compares three points on the read path:
//   - MemtableHit: the key hasn't flushed yet — a single map lookup, no I/O.
//   - SSTableHit: the key only exists in the oldest of several SSTables, so
//     Get's newest-to-oldest scan must reject every newer table before
//     finding it — each rejection is a bloom-filter check, and only the
//     final table pays for an index lookup + disk read + decode.
//   - AbsentKey: the key was never written. Every table rejects on the bloom
//     filter alone; comparing this to SSTableHit isolates what the bloom
//     filter is actually saving — a full index-lookup-and-decode per table.
func BenchmarkGet(b *testing.B) {
	const tables = 5
	const perTable = 2000

	b.Run("MemtableHit", func(b *testing.B) {
		dir := b.TempDir()
		db, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		defer db.Close()

		key := []byte("hot-key")
		if err := db.Put(key, benchValue); err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, ok, err := db.Get(key); err != nil || !ok {
				b.Fatalf("Get: ok=%v err=%v", ok, err)
			}
		}
	})

	b.Run("SSTableHit", func(b *testing.B) {
		dir := buildBenchStore(b, tables, perTable)
		db, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		defer db.Close()

		key := []byte(benchTableKey(0, 0)) // only in the oldest table

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, ok, err := db.Get(key); err != nil || !ok {
				b.Fatalf("Get: ok=%v err=%v", ok, err)
			}
		}
	})

	b.Run("AbsentKey", func(b *testing.B) {
		dir := buildBenchStore(b, tables, perTable)
		db, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		defer db.Close()

		key := []byte("key-that-was-never-written")

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, ok, err := db.Get(key); err != nil || ok {
				b.Fatalf("Get: unexpected hit, ok=%v err=%v", ok, err)
			}
		}
	})
}
