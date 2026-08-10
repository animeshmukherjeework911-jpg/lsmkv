package lsmkv

import (
	"hash/fnv"
	"math"
)

type BloomFilter struct {
	numBits   uint64
	numHashes uint64
	bitArray  []byte
}

func NewBloomFilter(expectedKeys int, falsePositiveRate float64) *BloomFilter {

	expectedKeys = max(1, expectedKeys)
	if !(0 < falsePositiveRate && falsePositiveRate < 1) {
		panic("False positive rate is expected to be between 0 and 1")
	}

	numBits := math.Ceil(-(float64(expectedKeys) * math.Log(falsePositiveRate)) / (math.Ln2 * math.Ln2))
	numHashes := math.Round((numBits / float64(expectedKeys) * math.Ln2))
	numHashes = max(1, numHashes)
	size := int((numBits + 7) / 8)
	return &BloomFilter{
		numBits:   uint64(numBits),
		numHashes: uint64(numHashes),
		bitArray:  make([]byte, size),
	}
}

func (b *BloomFilter) Add(key []byte) error {
	h1, h2, err := b.ComputeBaseKeyHash(key)

	if err != nil {
		return err
	}

	for i := uint64(0); i < b.numHashes; i++ {
		pos := (h1 + i*h2) % b.numBits
		b.bitArray[pos/8] |= (1 << (pos % 8))
	}

	return nil
}

func (b *BloomFilter) MayContain(key []byte) (bool, error) {
	h1, h2, err := b.ComputeBaseKeyHash(key)
	if err != nil {
		return false, err
	}

	for i := uint64(0); i < b.numHashes; i++ {
		pos := (h1 + i*h2) % b.numBits

		if b.bitArray[pos/8]&(1<<(pos%8)) == 0 {
			return false, nil
		}
	}

	return true, nil

}

func (b *BloomFilter) ComputeBaseKeyHash(key []byte) (uint64, uint64, error) {
	h1 := fnv.New64()
	if _, err := h1.Write(key); err != nil {
		return uint64(0), uint64(0), err
	}

	h2 := fnv.New64a()
	if _, err := h2.Write(key); err != nil {
		return uint64(0), uint64(0), err
	}

	return h1.Sum64(), h2.Sum64(), nil

}
