# gap-tracker Makefile
#
# Go build/install targets for the gapt binary.
# The Python version (main.py) still works in parallel until the port reaches
# parity — see docs/STATE_MACHINE.md for the migration status.

BINARY       := gapt
CMD_PKG      := ./cmd/gapt
BIN_DIR      := ./bin
LOCAL_BIN    := $(BIN_DIR)/$(BINARY)
GOBIN        ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN        := $(shell go env GOPATH)/bin
endif

VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT       ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE   ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS      := -X github.com/panyam/gap-tracker/internal/cli.Version=$(VERSION) \
                -X github.com/panyam/gap-tracker/internal/cli.Commit=$(COMMIT) \
                -X github.com/panyam/gap-tracker/internal/cli.BuildDate=$(BUILD_DATE)

.PHONY: all build install test tidy fmt vet clean smoke help

all: build

help:
	@echo "gap-tracker Makefile targets:"
	@echo "  build    compile ./cmd/gapt to $(LOCAL_BIN)"
	@echo "  install  'go install' gapt into \$$GOBIN ($(GOBIN))"
	@echo "  test     run all go tests"
	@echo "  tidy     go mod tidy"
	@echo "  fmt      gofmt -s -w ."
	@echo "  vet      go vet ./..."
	@echo "  smoke    build + run 'gapt version' locally"
	@echo "  clean    remove $(BIN_DIR)"

build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(LOCAL_BIN) $(CMD_PKG)
	@echo "built $(LOCAL_BIN)"

install:
	@echo "installing gapt to $(GOBIN)"
	go install -ldflags "$(LDFLAGS)" $(CMD_PKG)
	@echo "installed: $(GOBIN)/$(BINARY)"

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	gofmt -s -w .

vet:
	go vet ./...

smoke: build
	@echo "--- gapt version ---"
	@$(LOCAL_BIN) version
	@echo "--- gapt --help ---"
	@$(LOCAL_BIN) --help | head -20

clean:
	rm -rf $(BIN_DIR)
