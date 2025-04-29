VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD)
LD_FLAGS = -ldflags "-X github.com/lc/void/internal/buildinfo.Version=$(VERSION) -X github.com/lc/void/internal/buildinfo.Commit=$(COMMIT)"

fmt:
	@go fmt ./...
	@gofumpt -l -w .

lint:
	@golangci-lint run ./...

test: fmt
	@go test -v ./...

build: fmt
	@go build $(LD_FLAGS) -o bin/void ./cmd/void
	@go build $(LD_FLAGS) -o bin/voidd ./cmd/voidd

release: fmt
	@VERSION=$(VERSION) COMMIT=$(COMMIT) make clean build

clean:
	@rm -rf bin/

.PHONY: fmt lint test build clean release
