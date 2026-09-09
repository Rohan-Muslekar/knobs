.PHONY: test test-integration build build-server build-web build-knobs

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

build-web:
	cd web && npm ci && npm run build

build-server:
	go build -o bin/server ./cmd/server

build-knobs:
	go build -o bin/knobs ./cmd/knobs

build: build-web build-server build-knobs
