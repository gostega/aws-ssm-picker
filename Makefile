BIN := aws-ssm-picker
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT)
GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

.PHONY: all vet build test fmt install ci

all: vet build

vet:
	go vet ./...

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .
	@./$(BIN) --version

test:
	go test -race ./...

fmt:
	gofmt -w .

# Install to $GOBIN (or ~/go/bin) so the `ssm` symlink picks up the new build.
install:
	go install -ldflags "$(LDFLAGS)" .
	@$(GOBIN)/$(BIN) --version

# What CI runs — check formatting instead of rewriting it.
ci:
	@unformatted=$$(gofmt -l .); \
	  if [ -n "$$unformatted" ]; then echo "not gofmt'd:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go build ./...
	go test -race ./...
	gosec ./...
