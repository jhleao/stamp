VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test smoke install

build:
	mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o bin/stamp ./cmd/stamp

test:
	go test ./...

smoke: build
	./scripts/smoke.sh

install: build
	install -m 0755 bin/stamp /usr/local/bin/stamp
