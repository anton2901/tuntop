# Version comes from the git tag (vX.Y.Z); untagged builds get 0.0.0-dev+<sha>.
GIT_TAG  := $(shell git describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null)
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
VERSION  ?= $(if $(GIT_TAG),$(patsubst v%,%,$(GIT_TAG)),0.0.0-dev+$(COMMIT))
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

PKG      := github.com/anton2901/tuntop/internal/version
LDFLAGS  := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)
BINARIES := tuntop tuntopd

.PHONY: all build build-linux build-darwin push-test test test-remote lint fmt tidy clean release-check release-snapshot

all: lint test build

## build: build binaries for the host into bin/
build:
	@for b in $(BINARIES); do \
		CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$$b ./cmd/$$b || exit 1; \
	done

## build-linux: cross-compile for linux/amd64 into bin/linux-amd64/
build-linux:
	@for b in $(BINARIES); do \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" \
			-o bin/linux-amd64/$$b ./cmd/$$b || exit 1; \
	done

## push-test: copy a linux build and its installer to a test machine: make push-test HOST=<ssh-host>
push-test:
	@test -n "$(HOST)" || { echo "usage: make push-test HOST=<ssh-host>"; exit 2; }
	scripts/push-testkit.sh $(HOST)

## build-darwin: cross-compile for macOS (Apple Silicon and Intel) into bin/darwin-*/
build-darwin:
	@for arch in arm64 amd64; do for b in $(BINARIES); do \
		CGO_ENABLED=0 GOOS=darwin GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o bin/darwin-$$arch/$$b ./cmd/$$b || exit 1; \
	done; done

## test: run unit tests with the race detector
test:
	go test -race ./...

## test-remote: run the unit tests on a Linux machine without Go: make test-remote HOST=<ssh-host>
test-remote:
	@test -n "$(HOST)" || { echo "usage: make test-remote HOST=<ssh-host>"; exit 2; }
	scripts/test-remote.sh $(HOST)

## lint: static analysis and formatting check, for this OS and for Linux
lint:
	golangci-lint run
	GOOS=linux golangci-lint run

## fmt: format sources
fmt:
	golangci-lint fmt

# GoReleaser runs from source at a pinned version; nothing to install.
GORELEASER := go run github.com/goreleaser/goreleaser/v2@v2.18.3

## release-check: validate .goreleaser.yaml
release-check:
	$(GORELEASER) check

## release-snapshot: build every release artifact locally into dist/ (no upload)
release-snapshot:
	$(GORELEASER) release --snapshot --clean

tidy:
	go mod tidy

clean:
	rm -rf bin dist
