BIN := aws-ssm-picker

.PHONY: all vet build test fmt install ci

all: vet build

vet:
	go vet ./...

build:
	go build -o $(BIN) .

test:
	go test -race ./...

fmt:
	gofmt -w .

# Install to $GOBIN (or ~/go/bin) so `ssmpick` picks up the new build.
install:
	go install .

# What CI runs — check formatting instead of rewriting it.
ci:
	@unformatted=$$(gofmt -l .); \
	  if [ -n "$$unformatted" ]; then echo "not gofmt'd:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go build ./...
	go test -race ./...
	gosec ./...
