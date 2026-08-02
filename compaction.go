package lsmkv

// Compact merges several SSTables into fewer, cleaner ones. It is where read
// amplification is paid back down (fewer files to check per Get) and where
// tombstones finally erase data for real: when the newest version of a key is a
// tombstone and no older table survives the merge to contradict it, both the key
// and its tombstone are dropped from the output.
//
// Mechanically this is a k-way merge of sorted inputs — the Merge-K-Sorted-Lists
// pattern from your DSA roadmap, now with a reason to exist. Keep the newest
// version of each key (inputs are ordered newest-to-oldest) and skip the rest.
func Compact(inputs []*SSTable, outDir string) ([]*SSTable, error) {
	return nil, ErrNotImplemented // TODO(M5)
}
