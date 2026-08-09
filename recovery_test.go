package lsmkv

import (
	"os"
	"path/filepath"
	"testing"
)

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
// still recovers.
func TestCrashRecovery_TornWrite(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, "wal.log")

	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	keys := []string{"k0", "k1", "k2", "k3"}
	for _, k := range keys {
		if err := db.Put([]byte(k), []byte("value-"+k)); err != nil {
			t.Fatal(err)
		}
	}

	// size of the WAL right after the last INTACT record
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatal(err)
	}
	goodSize := info.Size()

	// one more record, which we're about to tear mid-append
	if err := db.Put([]byte("torn"), []byte("should-not-survive")); err != nil {
		t.Fatal(err)
	}

	// simulate `kill -9` mid-write: truncate a few bytes into the torn record,
	// not at a record boundary.
	if err := os.Truncate(walPath, goodSize+5); err != nil {
		t.Fatal(err)
	}

	// abandon the handle without Close, exactly like TestCrashRecovery_WALReplay
	db = nil

	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("recovery failed to open the db: %v", err)
	}
	defer db2.Close()

	for _, k := range keys {
		v, ok, _ := db2.Get([]byte(k))
		if !ok || string(v) != "value-"+k {
			t.Fatalf("lost intact record %q after torn-write recovery (got %q/%v)", k, v, ok)
		}
	}

	if _, ok, _ := db2.Get([]byte("torn")); ok {
		t.Fatalf("torn record should not have survived recovery, but it did")
	}
}
