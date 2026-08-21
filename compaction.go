package lsmkv

import (
	"bytes"
	"container/heap"
	"errors"
	"os"
)

type mergeItem struct {
	record   Record
	inputIdx int
}

type mergeHeap []mergeItem

var ErrReadingInputRecord = errors.New("error reading input record")

// compactionTempSuffix marks a compacted table that FlushMemtable has
// durably written but that hasn't yet been renamed into its real filename.
// A file with this suffix is never a valid SSTable — Open cleans up any it
// finds left behind by a crash before the rename.
const compactionTempSuffix = ".tmp"

func (h *mergeHeap) Len() int {
	return len(*h)
}

func (h *mergeHeap) Less(i, j int) bool {

	res := bytes.Compare((*h)[i].record.Key, (*h)[j].record.Key)

	if res == 0 {
		return (*h)[i].inputIdx < (*h)[j].inputIdx
	} else {
		return res < 0
	}

}

func (h *mergeHeap) Swap(i, j int) {
	(*h)[i], (*h)[j] = (*h)[j], (*h)[i]
}

func (h *mergeHeap) Push(x any) {
	heapItem, ok := x.(mergeItem)
	if ok {
		(*h) = append((*h), heapItem)
	}
}

func (h *mergeHeap) Pop() any {
	old := *h
	n := len(old)
	if n == 0 {
		return mergeItem{}
	}
	last := old[n-1]
	*h = old[:n-1]
	return last
}

// syncDir fsyncs a directory so that file creations, renames, and removals
// inside it are durable — POSIX does not guarantee a directory entry change
// survives a crash until the directory itself has been fsynced, the same way
// a file's own contents need Sync after Write.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

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
	perInput := make([][]Record, len(inputs))
	for i, input := range inputs {
		records, err := input.Records()
		if err != nil {
			return nil, ErrReadingInputRecord
		}
		perInput[i] = records
	}

	// pos[i] is how many records of perInput[i] have been pushed onto the heap
	// so far — the "cursor" into that input.
	pos := make([]int, len(inputs))

	h := &mergeHeap{}
	pushNext := func(inputIdx int) {
		if pos[inputIdx] < len(perInput[inputIdx]) {
			heap.Push(h, mergeItem{record: perInput[inputIdx][pos[inputIdx]], inputIdx: inputIdx})
			pos[inputIdx]++
		}
	}
	for i := range inputs {
		pushNext(i)
	}

	merged := NewMemtable()
	for h.Len() > 0 {
		newest := heap.Pop(h).(mergeItem)
		pushNext(newest.inputIdx)

		// Older versions of the same key sitting elsewhere in the heap right now
		// are shadowed by newest — drain and discard them, advancing their cursors.
		for h.Len() > 0 && bytes.Equal((*h)[0].record.Key, newest.record.Key) {
			shadowed := heap.Pop(h).(mergeItem)
			pushNext(shadowed.inputIdx)
		}

		if newest.record.Kind != RecordDelete {
			merged.Put(newest.record)
		}
	}

	var out *SSTable
	if merged.SizeBytes() > 0 {
		finalPath, err := nextSSTablePath(outDir)
		if err != nil {
			return nil, err
		}

		// Write + fsync under a temp name first (FlushMemtable already syncs the
		// file before returning). Only once those bytes are durably on disk do we
		// rename into the real filename — the rename is the commit point: nothing
		// before it is visible to Open as a real SSTable, so a crash mid-write
		// just leaves an orphaned .tmp file for Open to clean up.
		tempPath := finalPath + compactionTempSuffix
		out, err = FlushMemtable(merged, tempPath)
		if err != nil {
			return nil, err
		}

		if err := os.Rename(tempPath, finalPath); err != nil {
			return nil, err
		}
		// The rename itself needs to survive a crash too: on POSIX a directory
		// entry change isn't guaranteed durable until the directory is fsynced.
		if err := syncDir(outDir); err != nil {
			return nil, err
		}
		out.path = finalPath
	}

	// Only delete inputs once the merged table (if any) is durably on disk and
	// renamed into place — deleting first would lose data on a crash between
	// the two steps.
	for _, input := range inputs {
		if err := os.Remove(input.Path()); err != nil {
			return nil, err
		}
	}
	if err := syncDir(outDir); err != nil {
		return nil, err
	}

	if out == nil {
		return nil, nil
	}
	return []*SSTable{out}, nil
}
