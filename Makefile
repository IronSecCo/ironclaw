.PHONY: build vet test fmt lint smoke

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

fmt:
	gofmt -w .

lint:
	go vet ./...

# Release-readiness smoke matrix (IRO-295): build the demo images from the current
# checkout, then run EVERY examples/*/ recipe end-to-end against one offline
# control-plane and assert real output. Zero credentials (mock provider). Needs
# Docker + jq. Exits non-zero on any empty/incorrect example output.
smoke:
	examples/smoke-matrix.sh
