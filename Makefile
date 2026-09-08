.PHONY: test test-integration build build-server

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

build-server:
	go build -o bin/server ./cmd/server

build: build-server
