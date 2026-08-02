package lsmkv

import "testing"

// These tests are your milestone gates. They all fail today (ErrNotImplemented).
// Each one turns green when you finish the milestone named in its comment. Never
// edit a test to make it pass — make the engine satisfy the test.

// GATE M2 — a value you put comes back in the same session.
func TestPutGet(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	v, ok, err := db.Get([]byte("k"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || string(v) != "v" {
		t.Fatalf("want v/true, got %q/%v", v, ok)
	}
}

// GATE M2 — a delete hides a key, and the key does NOT resurrect.
func TestDeleteTombstone(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete([]byte("k")); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := db.Get([]byte("k")); ok {
		t.Fatal("deleted key came back from the dead")
	}
}

// GATE M3 — data survives a clean Close + reopen (real on-disk persistence).
func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()

	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Put([]byte("durable"), []byte("yes")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	v, ok, _ := db2.Get([]byte("durable"))
	if !ok || string(v) != "yes" {
		t.Fatalf("lost data across reopen: got %q/%v", v, ok)
	}
}
