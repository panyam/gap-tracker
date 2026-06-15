# gap-tracker Makefile
#
# Go build/install targets for the gapt and disctrack binaries.
# - gapt: PR triage / classification (the original tool).
# - disctrack: Discord channel resolver against .discord-tracking.yaml.
# The Python version of gapt (main.py) still works in parallel until the port
# reaches parity — see docs/STATE_MACHINE.md for the migration status.

BIN_DIR      := ./bin
GOBIN        ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN        := $(shell go env GOPATH)/bin
endif

# gapt build inputs
GAPT_BIN     := gapt
GAPT_PKG     := ./cmd/gapt
GAPT_LOCAL   := $(BIN_DIR)/$(GAPT_BIN)

# disctrack build inputs
DT_BIN       := disctrack
DT_PKG       := ./cmd/disctrack
DT_LOCAL     := $(BIN_DIR)/$(DT_BIN)

VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT       ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE   ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

GAPT_LDFLAGS := -X github.com/panyam/gap-tracker/internal/cli.Version=$(VERSION) \
                -X github.com/panyam/gap-tracker/internal/cli.Commit=$(COMMIT) \
                -X github.com/panyam/gap-tracker/internal/cli.BuildDate=$(BUILD_DATE)

DT_SNAPSHOT  := $(abspath python/snapshot.py)
DT_LDFLAGS   := -X github.com/panyam/gap-tracker/internal/disctrackcli.Version=$(VERSION) \
                -X github.com/panyam/gap-tracker/internal/disctrackcli.Commit=$(COMMIT) \
                -X github.com/panyam/gap-tracker/internal/disctrackcli.BuildDate=$(BUILD_DATE) \
                -X github.com/panyam/gap-tracker/internal/disctrack.DefaultSnapshotScript=$(DT_SNAPSHOT)

.PHONY: all build install test tidy fmt vet clean smoke help \
        build-gapt build-disctrack install-gapt install-disctrack smoke-gapt smoke-disctrack

all: build

help:
	@echo "gap-tracker Makefile targets:"
	@echo "  build              build both binaries ($(GAPT_BIN) + $(DT_BIN)) into $(BIN_DIR)"
	@echo "  install            'go install' both into \$$GOBIN ($(GOBIN))"
	@echo "  build-gapt         build only gapt"
	@echo "  build-disctrack    build only disctrack"
	@echo "  install-gapt       install only gapt"
	@echo "  install-disctrack  install only disctrack"
	@echo "  smoke              build both + print versions/help"
	@echo "  test               run all go tests"
	@echo "  tidy               go mod tidy"
	@echo "  fmt                gofmt -s -w ."
	@echo "  vet                go vet ./..."
	@echo "  clean              remove $(BIN_DIR)"

build: build-gapt build-disctrack

build-gapt:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(GAPT_LDFLAGS)" -o $(GAPT_LOCAL) $(GAPT_PKG)
	@echo "built $(GAPT_LOCAL)"

build-disctrack:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(DT_LDFLAGS)" -o $(DT_LOCAL) $(DT_PKG)
	@echo "built $(DT_LOCAL)"

install: install-gapt install-disctrack

install-gapt:
	@echo "installing $(GAPT_BIN) to $(GOBIN)"
	go install -ldflags "$(GAPT_LDFLAGS)" $(GAPT_PKG)
	@echo "installed: $(GOBIN)/$(GAPT_BIN)"

install-disctrack:
	@echo "installing $(DT_BIN) to $(GOBIN)"
	go install -ldflags "$(DT_LDFLAGS)" $(DT_PKG)
	@echo "installed: $(GOBIN)/$(DT_BIN)"

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	gofmt -s -w .

vet:
	go vet ./...

smoke: smoke-gapt smoke-disctrack

smoke-gapt: build-gapt
	@echo "--- $(GAPT_BIN) version ---"
	@$(GAPT_LOCAL) version
	@echo "--- $(GAPT_BIN) --help ---"
	@$(GAPT_LOCAL) --help | head -20

smoke-disctrack: build-disctrack
	@echo "--- $(DT_BIN) version ---"
	@$(DT_LOCAL) version
	@echo "--- $(DT_BIN) --help ---"
	@$(DT_LOCAL) --help | head -20

clean:
	rm -rf $(BIN_DIR)
