package lsmkv

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"hash/fnv"
	"math"
)

type BloomFilter struct {
	numBits   uint64
	numHashes uint64
	bitArray  []byte
}

var ErrShortBloomFilter = errors.New("short or corrupt record")

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

func EncodeBloomFilter(b *BloomFilter) []byte {
	// [ crc32   uint32 (4 bytes) ]
	// [ numBits uint64 (8 bytes) ]
	// [ numHashes uint64 (8 bytes) ]
	// [ bitArray (remaining bytes, length = (numBits+7)/8) ]

	payload := make([]byte, 8+8+(b.numBits+7)/8)
	binary.LittleEndian.PutUint64(payload[0:8], uint64(b.numBits))
	binary.LittleEndian.PutUint64(payload[8:16], uint64(b.numHashes))
	copy(payload[16:], b.bitArray)
	rc := crc32.ChecksumIEEE(payload)
	buf := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], uint32(rc))
	copy(buf[4:], payload)
	return buf
}

func DecodeBloomFilter(buf []byte) (bloom BloomFilter, err error) {
	headerLen := 4 + 8 + 8

	if len(buf) < headerLen {
		return BloomFilter{}, ErrShortBloomFilter
	}

	storedCrc := binary.LittleEndian.Uint32(buf[0:4])
	numBits := binary.LittleEndian.Uint64(buf[4:12])
	numHashes := binary.LittleEndian.Uint64(buf[12:20])
	bitArray := make([]byte, len(buf)-headerLen)
	copy(bitArray, buf[headerLen:])

	payload := buf[4:]
	if crc32.ChecksumIEEE(payload) != storedCrc {
		return BloomFilter{}, ErrShortBloomFilter
	}

	bloom = BloomFilter{
		numBits:   uint64(numBits),
		numHashes: uint64(numHashes),
		bitArray:  bitArray,
	}

	return bloom, nil

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
