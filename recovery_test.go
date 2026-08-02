package lsmkv

import "testing"

// The recovery tests are the soul of the project. Anyone can build a hash map on
// disk; the engineering is in what happens when the process dies mid-write.

// GATE M1 — an acknowledged write survives a crash.
//
// We simulate `kill -9` the only honest cheap way: we abandon the DB handle
// WITHOUT calling Close, then open a fresh handle on the same directory — exactly
// the situation after a hard kill. Recovery must replay the WAL and find the write.
func TestCrashRecovery_WALReplay(t *testing.T) {
	dir := t.TempDir()

	// ---- process, just before the crash ----
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Put([]byte("crash-key"), []byte("survived")); err != nil {
		t.Fatal(err)
	}
	// NO db.Close(). Pretend we were killed on the very next instruction.
	db = nil

	// ---- process, after the crash ----
	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("recovery failed to open the db: %v", err)
	}
	defer db2.Close()

	v, ok, _ := db2.Get([]byte("crash-key"))
	if !ok || string(v) != "survived" {
		t.Fatalf("DURABILITY VIOLATED: acknowledged write lost after crash (got %q/%v)", v, ok)
	}
}

// GATE M1 (harder) — a torn final record is ignored, and everything before it
// still recovers. Un-skip this once Encode/Decode carry a crc.
func TestCrashRecovery_TornWrite(t *testing.T) {
	t.Skip("un-skip in M1 once record encoding carries a crc — see README")
	// TODO(M1): Put N records, then hand-truncate the WAL file a few bytes into
	// the last record to fake a torn append. Reopen and assert:
	//   (1) Open returns nil error, and
	//   (2) the first N-1 keys are present, the torn Nth is simply absent.
}
