# While goreleaser is used locally to release new versions,
# these make targets ease testing and building this program.

BINARY=chatserver
VERSION= $(shell (git describe --tags --dirty 2>/dev/null || echo dev) |sed -e 's/^v//')
GIT_COMMIT=$(shell git rev-parse HEAD)
LDFLAGS="-s -w -X github.com/ivanfetch/chatserver.Version=$(VERSION) -X github.com/ivanfetch/chatserver.GitCommit=$(GIT_COMMIT)"

all: build

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:go.sum
	go vet ./...

go.sum:go.mod
	go get -t github.com/ivanfetch/chatserver

.PHONY: test
test:go.sum
	go test -coverprofile=cover.out ./...

test-profile:go.sum
	go test -coverprofile=cover.out -blockprofile  blockgoroutines.pprof
	rm chatserver.test

.PHONY: binary
binary:go.sum
	go build -ldflags $(LDFLAGS) -o $(BINARY) cmd/chatserver/main.go

.PHONY: build
build: fmt vet test binary
