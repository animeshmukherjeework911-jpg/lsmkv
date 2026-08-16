package lsmkv

import (
	"encoding/binary"
	"errors"
)

const (
	footerSize = 32
)

var errShortFooter = errors.New("err, short footer found")

type Footer struct {
	sparseIndexOffset uint64
	sparseIndexLength uint64
	bloomFilterOffset uint64
	bloomFilterLength uint64
}

func (f *Footer) EncodeFooter() []byte {

	buf := make([]byte, footerSize)

	binary.LittleEndian.PutUint64(buf[0:8], f.sparseIndexOffset)
	binary.LittleEndian.PutUint64(buf[8:16], f.sparseIndexLength)
	binary.LittleEndian.PutUint64(buf[16:24], f.bloomFilterOffset)
	binary.LittleEndian.PutUint64(buf[24:32], f.bloomFilterLength)

	return buf

}

func DecodeFooter(buf []byte) (*Footer, error) {

	if len(buf) != footerSize {
		return nil, errShortFooter
	}

	return &Footer{
		sparseIndexOffset: binary.LittleEndian.Uint64(buf[0:8]),
		sparseIndexLength: binary.LittleEndian.Uint64(buf[8:16]),
		bloomFilterOffset: binary.LittleEndian.Uint64(buf[16:24]),
		bloomFilterLength: binary.LittleEndian.Uint64(buf[24:32]),
	}, nil
}
