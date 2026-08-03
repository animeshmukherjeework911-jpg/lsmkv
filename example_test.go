package lsmkv

import (
	"fmt"
	"os"
)

// Example demonstrates the Stage 2 gate: put, get, delete, and confirm the
// delete sticks. Run it with `go test -run Example -v`.
func Example() {
	dir, err := os.MkdirTemp("", "lsmkv-example")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	db, err := Open(dir)
	if err != nil {
		panic(err)
	}

	if err := db.Put([]byte("lang"), []byte("go")); err != nil {
		panic(err)
	}
	v, ok, _ := db.Get([]byte("lang"))
	fmt.Printf("get lang -> %q %v\n", v, ok)

	if err := db.Delete([]byte("lang")); err != nil {
		panic(err)
	}
	_, ok, _ = db.Get([]byte("lang"))
	fmt.Printf("get lang after delete -> found=%v\n", ok)

	// Output:
	// get lang -> "go" true
	// get lang after delete -> found=false
}
