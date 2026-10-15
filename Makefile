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

.PHONY: check-paths all build install test tidy fmt vet clean smoke help \
        build-gapt build-disctrack install-gapt install-disctrack smoke-gapt smoke-disctrack \
        setup setup-scrape doctor

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
	@echo "  setup              install both binaries + the scrape toolchain (new box)"
	@echo "  setup-scrape       uv + Playwright Chromium + its system libs (disctrack sync)"
	@echo "  doctor             check every disctrack sync prerequisite, fix nothing"
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

test: check-paths
	go test ./...

# C6 (megh CONSTRAINTS.md): no tracked file may encode one machine's paths.
# One implementation, in the dotfiles repo, which is mounted at the same path on
# the Mac and on every box. Skipped rather than failed where it is absent, so
# `make test` still works on a machine that has no dotfiles checkout.
check-paths:
	@script="$$HOME/dotfiles/shared/checks/no-machine-paths.sh"; \
	if [ -x "$$script" ]; then "$$script" .; \
	else echo "skipping machine-path check ($$script not present)"; fi

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

# --- disctrack sync toolchain -----------------------------------------
#
# `disctrack sync` shells out to uv, which runs python/snapshot.py, which
# drives Playwright + Chromium. Three separate things have to be present and
# none of them come from `go install`, so a fresh box fails three times in a
# row, each failure only visible after the previous one is fixed:
#
#   1. no uv          -> `exec: "uv": executable file not found in $PATH`
#   2. no browser     -> Playwright's "Executable doesn't exist at ..." banner
#   3. no system libs -> `validateDependenciesLinux` host-requirements error
#
# `make setup-scrape` does all three, idempotently. `make doctor` reports on
# them without changing anything.
#
# uv is installed via astral.sh rather than pip because Debian/Ubuntu ship
# Python as an externally-managed environment (PEP 668), where a plain
# `pip3 install uv` refuses to run.

UV           ?= $(shell command -v uv 2>/dev/null || echo $(HOME)/.local/bin/uv)
PLAYWRIGHT_PKG := playwright>=1.40

setup: install setup-scrape
	@echo
	@echo "setup complete. verify with: make doctor"

setup-scrape:
	@if ! command -v uv >/dev/null 2>&1 && [ ! -x "$(UV)" ]; then \
		echo "--- installing uv (astral.sh; pip is blocked by PEP 668 on Debian/Ubuntu) ---"; \
		curl -LsSf https://astral.sh/uv/install.sh | sh; \
	else \
		echo "--- uv present: $$($(UV) --version) ---"; \
	fi
	@echo "--- installing Playwright Chromium ---"
	@$(UV) run --with "$(PLAYWRIGHT_PKG)" playwright install chromium
	@echo "--- installing Chromium system libraries (apt; may prompt for sudo) ---"
	@if [ "$$(id -u)" = "0" ]; then \
		$(UV) run --with "$(PLAYWRIGHT_PKG)" playwright install-deps chromium; \
	else \
		sudo -E $(UV) run --with "$(PLAYWRIGHT_PKG)" playwright install-deps chromium; \
	fi
	@echo
	@echo "scrape toolchain ready. ensure $(HOME)/.local/bin is on PATH, then:"
	@echo "  disctrack sync <channel> --headless"

doctor:
	@echo "--- disctrack sync prerequisites ---"
	@printf '%-22s' "go binaries:"; \
		if command -v disctrack >/dev/null 2>&1; then echo "ok ($$(command -v disctrack))"; \
		else echo "MISSING -> make install"; fi
	@printf '%-22s' "uv:"; \
		if command -v uv >/dev/null 2>&1; then echo "ok ($$(uv --version))"; \
		else echo "MISSING -> make setup-scrape"; fi
	@printf '%-22s' "snapshot.py:"; \
		if [ -f "$(DT_SNAPSHOT)" ]; then echo "ok ($(DT_SNAPSHOT))"; \
		else echo "MISSING at $(DT_SNAPSHOT)"; fi
	@printf '%-22s' "chromium:"; \
		if ls $(HOME)/.cache/ms-playwright/chromium-* >/dev/null 2>&1; then echo "ok"; \
		else echo "MISSING -> make setup-scrape"; fi
	@printf '%-22s' "chromium libs:"; \
		if $(UV) run --with "$(PLAYWRIGHT_PKG)" python -c "import sys; from playwright.sync_api import sync_playwright;\
		 p=sync_playwright().start();\
		 b=p.chromium.launch(headless=True); b.close(); p.stop()" >/dev/null 2>&1; \
		then echo "ok (headless Chromium launched)"; \
		else echo "BROKEN -> make setup-scrape"; fi
	@printf '%-22s' "tracking root:"; \
		if command -v disctrack >/dev/null 2>&1 && disctrack config >/dev/null 2>&1; \
		then echo "ok ($$(disctrack config | awk '/tracking_root/{print $$2}'))"; \
		else echo "unresolved from here (expected: gap-tracker is not a tracking root; check from your project dir)"; fi
	@echo
	@echo "note: a valid Discord login lives in the Chromium profile dir and cannot be"
	@echo "checked here. If sync stalls on a login page, rerun without --headless."

clean:
	rm -rf $(BIN_DIR)
