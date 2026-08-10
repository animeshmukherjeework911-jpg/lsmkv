package lsmkv

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestBloomFilterNoFalseNegatives(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)

	keys := make([][]byte, 1000)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%d", i))
		if err := bf.Add(keys[i]); err != nil {
			t.Fatalf("Add(%q) returned error: %v", keys[i], err)
		}
	}

	for _, key := range keys {
		ok, err := bf.MayContain(key)
		if err != nil {
			t.Fatalf("MayContain(%q) returned error: %v", key, err)
		}
		if !ok {
			t.Errorf("MayContain(%q) = false, want true (false negative)", key)
		}
	}
}

func TestBloomFilterFalsePositiveRate(t *testing.T) {
	const n = 10000
	const targetFPRate = 0.01

	bf := NewBloomFilter(n, targetFPRate)

	present := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("present-%d", i))
		present[string(key)] = true
		if err := bf.Add(key); err != nil {
			t.Fatalf("Add returned error: %v", err)
		}
	}

	falsePositives := 0
	trials := 100000
	for i := 0; i < trials; i++ {
		key := []byte(fmt.Sprintf("absent-%d", i))
		if present[string(key)] {
			continue // shouldn't happen given disjoint prefixes, but guard anyway
		}
		ok, err := bf.MayContain(key)
		if err != nil {
			t.Fatalf("MayContain returned error: %v", err)
		}
		if ok {
			falsePositives++
		}
	}

	observedRate := float64(falsePositives) / float64(trials)
	// Allow generous slack (3x target) since this is a probabilistic property,
	// not an exact bound, and we don't want a flaky test.
	maxAcceptable := targetFPRate * 3
	if observedRate > maxAcceptable {
		t.Errorf("observed false positive rate %.4f exceeds acceptable bound %.4f (target %.4f)",
			observedRate, maxAcceptable, targetFPRate)
	}
	t.Logf("observed false positive rate: %.4f (target %.4f)", observedRate, targetFPRate)
}

func TestBloomFilterAbsentKeyBeforeAnyAdd(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)
	ok, err := bf.MayContain([]byte("nonexistent"))
	if err != nil {
		t.Fatalf("MayContain returned error: %v", err)
	}
	if ok {
		t.Errorf("MayContain on empty filter = true, want false")
	}
}

func TestBloomFilterSmallExpectedKeys(t *testing.T) {
	// Regression test: small n / loose fpRate combos must not let numHashes
	// round down to 0, which would make MayContain vacuously true for everything.
	bf := NewBloomFilter(1, 0.5)
	if bf.numHashes < 1 {
		t.Fatalf("numHashes = %d, want >= 1", bf.numHashes)
	}

	if err := bf.Add([]byte("a")); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	ok, err := bf.MayContain([]byte("a"))
	if err != nil {
		t.Fatalf("MayContain returned error: %v", err)
	}
	if !ok {
		t.Errorf("MayContain(%q) = false, want true", "a")
	}

	// Not a hard guarantee (still probabilistic), but with numHashes >= 1 and a
	// reasonably sized bit array, an unrelated key should usually come back false.
	ok, err = bf.MayContain([]byte("completely-different-key-xyz"))
	if err != nil {
		t.Fatalf("MayContain returned error: %v", err)
	}
	_ = ok // not asserted: could legitimately be a false positive at this tiny scale
}

func TestBloomFilterRandomKeys(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const n = 500
	bf := NewBloomFilter(n, 0.05)

	keys := make([][]byte, n)
	for i := range keys {
		k := make([]byte, 16)
		rng.Read(k)
		keys[i] = k
		if err := bf.Add(k); err != nil {
			t.Fatalf("Add returned error: %v", err)
		}
	}

	for _, k := range keys {
		ok, err := bf.MayContain(k)
		if err != nil {
			t.Fatalf("MayContain returned error: %v", err)
		}
		if !ok {
			t.Errorf("MayContain(%x) = false, want true (false negative)", k)
		}
	}
}
