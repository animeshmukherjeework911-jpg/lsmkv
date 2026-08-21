package lsmkv

import (
	"os"
	"path/filepath"
	"testing"
)

// flushRecords stages records into a fresh memtable and flushes it to a new
// SSTable in dir, using the same sequential-naming scheme DB uses.
func flushRecords(t *testing.T, dir string, records ...Record) *SSTable {
	t.Helper()
	m := NewMemtable()
	for _, r := range records {
		m.Put(r)
	}
	path, err := nextSSTablePath(dir)
	if err != nil {
		t.Fatal(err)
	}
	sst, err := FlushMemtable(m, path)
	if err != nil {
		t.Fatal(err)
	}
	return sst
}

func rec(key, val string) Record {
	return Record{Key: []byte(key), Value: []byte(val), Kind: RecordSet}
}

func tombstone(key string) Record {
	return Record{Key: []byte(key), Kind: RecordDelete}
}

// GATE M5 — overwrite + delete a key across flushes, compact, assert only the
// latest version of each key survives and a fully-shadowed tombstone drops
// the key from the output entirely.
func TestCompact_NewestWinsAndTombstonesDropKeys(t *testing.T) {
	dir := t.TempDir()

	// Oldest first, matching flush order.
	oldest := flushRecords(t, dir, rec("key-a", "v1"), rec("key-b", "v-b"))
	middle := flushRecords(t, dir, rec("key-a", "v2"))
	newest := flushRecords(t, dir, tombstone("key-a"), rec("key-c", "v-c"))

	// Compact expects newest-to-oldest input order.
	inputs := []*SSTable{newest, middle, oldest}

	outputs, err := Compact(inputs, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 {
		t.Fatalf("got %d output tables, want 1", len(outputs))
	}
	out := outputs[0]

	mustNotFind(t, out, "key-a") // newest version was a tombstone
	rb := mustGet(t, out, "key-b")
	if string(rb.Value) != "v-b" {
		t.Errorf("key-b: got %q, want %q", rb.Value, "v-b")
	}
	rc := mustGet(t, out, "key-c")
	if string(rc.Value) != "v-c" {
		t.Errorf("key-c: got %q, want %q", rc.Value, "v-c")
	}

	// Input files must be gone once compaction succeeds.
	for _, in := range inputs {
		if _, err := os.Stat(in.Path()); !os.IsNotExist(err) {
			t.Errorf("input %s still exists after compaction", in.Path())
		}
	}
}

// An input that is entirely tombstones, with nothing surviving from other
// inputs, must compact down to no output table at all rather than an empty
// SSTable file.
func TestCompact_AllTombstonesProducesNoOutput(t *testing.T) {
	dir := t.TempDir()

	sst := flushRecords(t, dir, tombstone("key-a"), tombstone("key-b"))

	outputs, err := Compact([]*SSTable{sst}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 0 {
		t.Fatalf("got %d output tables, want 0", len(outputs))
	}
	if _, err := os.Stat(sst.Path()); !os.IsNotExist(err) {
		t.Errorf("input %s still exists after compaction", sst.Path())
	}
}

// A single input still goes through Compact (e.g. a scheduled compaction
// pass with only one eligible table) and must still drop its tombstones.
func TestCompact_SingleInputDropsTombstones(t *testing.T) {
	dir := t.TempDir()

	sst := flushRecords(t, dir, rec("key-a", "v1"), tombstone("key-b"))

	outputs, err := Compact([]*SSTable{sst}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 {
		t.Fatalf("got %d output tables, want 1", len(outputs))
	}

	ra := mustGet(t, outputs[0], "key-a")
	if string(ra.Value) != "v1" {
		t.Errorf("key-a: got %q, want %q", ra.Value, "v1")
	}
	mustNotFind(t, outputs[0], "key-b")
}

// GATE M5 — disk footprint must measurably shrink once dead versions and
// tombstones are compacted away.
func TestCompact_DiskFootprintShrinks(t *testing.T) {
	dir := t.TempDir()

	const key = "hot-key"
	var inputs []*SSTable
	for i := 0; i < 5; i++ {
		inputs = append(inputs, flushRecords(t, dir, rec(key, "same-value-every-time")))
	}
	// Compact expects newest-to-oldest; flushRecords appended oldest-to-newest above.
	for i, j := 0, len(inputs)-1; i < j; i, j = i+1, j-1 {
		inputs[i], inputs[j] = inputs[j], inputs[i]
	}

	var before int64
	for _, in := range inputs {
		info, err := os.Stat(in.Path())
		if err != nil {
			t.Fatal(err)
		}
		before += info.Size()
	}

	outputs, err := Compact(inputs, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 {
		t.Fatalf("got %d output tables, want 1", len(outputs))
	}

	info, err := os.Stat(outputs[0].Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() >= before {
		t.Errorf("compacted size %d did not shrink from pre-compaction total %d", info.Size(), before)
	}
}

// GATE M5b — a crash between the rename that commits the compacted output
// and the deletion of the old inputs must leave both on disk, and Open + Get
// must still return correct (newest-wins) data — the compacted table's
// higher sequence number must win over the stale originals sitting next to
// it, never the other way around.
func TestCompact_SurvivesCrashBeforeInputDeletion(t *testing.T) {
	dir := t.TempDir()

	oldest := flushRecords(t, dir, rec("key-a", "v1"), rec("key-b", "v-b"))
	newest := flushRecords(t, dir, rec("key-a", "v2"))
	oldPaths := []string{oldest.Path(), newest.Path()}
	oldContents := make([][]byte, len(oldPaths))
	for i, p := range oldPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		oldContents[i] = data
	}

	if _, err := Compact([]*SSTable{newest, oldest}, dir); err != nil {
		t.Fatal(err)
	}

	// Real compaction already deleted the inputs at this point. Recreate them
	// byte-for-byte at their original paths to simulate a crash that landed
	// after the rename committed the new table but before the old ones were
	// unlinked — exactly the window Stage 5b has to survive.
	for i, p := range oldPaths {
		if err := os.WriteFile(p, oldContents[i], 0644); err != nil {
			t.Fatal(err)
		}
	}

	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if len(db.ssts) < 3 {
		t.Fatalf("expected stale inputs + compacted output to all load, got %d tables", len(db.ssts))
	}

	v, ok, err := db.Get([]byte("key-a"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(v) != "v2" {
		t.Fatalf("key-a: got %q/%v, want %q (compacted value must win over stale input)", v, ok, "v2")
	}
	v, ok, err = db.Get([]byte("key-b"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(v) != "v-b" {
		t.Fatalf("key-b: got %q/%v, want %q", v, ok, "v-b")
	}
}

// GATE M5b — a crash that leaves only an un-renamed .tmp compaction file
// behind (the rename never happened) must never be treated as a valid
// SSTable; Open must detect and remove it.
func TestCompact_OpenCleansUpOrphanedTempFile(t *testing.T) {
	dir := t.TempDir()

	// A real table so Open has something to legitimately load alongside the
	// orphan, proving the cleanup targets only the .tmp file.
	flushRecords(t, dir, rec("key-a", "v1"))

	orphan := filepath.Join(dir, "sstable-00000005.sst"+compactionTempSuffix)
	if err := os.WriteFile(orphan, []byte("not a real sstable"), 0644); err != nil {
		t.Fatal(err)
	}

	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphaned temp file %s still exists after Open", orphan)
	}
	if len(db.ssts) != 1 {
		t.Fatalf("got %d tables after Open, want 1 (orphan must not load as an SSTable)", len(db.ssts))
	}

	v, ok, err := db.Get([]byte("key-a"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(v) != "v1" {
		t.Fatalf("key-a: got %q/%v, want %q", v, ok, "v1")
	}
}
