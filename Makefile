.PHONY: build test recovery bench fmt vet todo

build:      ## compile everything
	go build ./...

test:       ## run all milestone-gate tests (they start red — that's correct)
	go test ./... -count=1

recovery:   ## run only the crash-recovery tests, verbose
	go test ./... -run Crash -v -count=1

bench:      ## run benchmarks (M6)
	go test ./... -run xxx -bench . -benchmem

fmt:
	go fmt ./...

vet:
	go vet ./...

todo:       ## your progress bar: how much is left to implement
	@grep -rn "TODO(" . | wc -l | xargs echo "open TODOs:"
	@grep -rln "ErrNotImplemented\|panic(\"TODO" . || true
