.PHONY: build vet test fmt lint

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
