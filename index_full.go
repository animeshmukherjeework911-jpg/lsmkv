package lsmkv

type fullIndex struct {
	entries map[string]int64
}

func newFullIndex() *fullIndex {
	return &fullIndex{entries: make(map[string]int64)}
}

func (fi *fullIndex) add(key []byte, offset int64) {
	fi.entries[string(key)] = offset
}

func (fi *fullIndex) lookup(key []byte) (int64, bool) {
	offset, ok := fi.entries[string(key)]
	return offset, ok
}
