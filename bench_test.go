package lsmkv

import "testing"

// M6 — find the engine's limits. Fill these in LAST, never before the store is
// correct. The number itself is worthless; being able to explain WHY it's that
// number — which layer is the bottleneck and why — is the entire deliverable.

func BenchmarkPut(b *testing.B) {
	b.Skip("un-skip in M6")
	// TODO(M6): sequential Put throughput first, then random keys. Explain the gap.
}

func BenchmarkGet(b *testing.B) {
	b.Skip("un-skip in M6")
	// TODO(M6): Get on a cold store vs a warm one. Watch the bloom filter earn its keep.
}
