# womprat project Makefile
#
# Goals:
# - keep the normal path one-command simple (`make release`)
# - make build prerequisites explicit (`make setup` / `make doctor`)
# - keep generated Windows resources reproducible from checked-in assets
# - provide a stable hook for local dependency/module patches

# Resolve once before child TMPDIR. PROJECT_TMP_BASE appends /womprat;
# PROJECT_TMP_ROOT is compatible if both agree. Invalid overrides fail closed.
# CI: RUNNER_TEMP -> inherited TMPDIR -> system temp (never workspace).
# Local: usable /workspace/tmp -> system temp. Hierarchy: cache/build/tests/logs/runs.
SHELL := /bin/bash
shell_quote = '$(subst ','"'"',$(1))'
ROOT_ENV := $(if $(filter undefined,$(origin PROJECT_TMP_BASE)),,PROJECT_TMP_BASE=$(call shell_quote,$(PROJECT_TMP_BASE))) $(if $(filter undefined,$(origin PROJECT_TMP_ROOT)),,PROJECT_TMP_ROOT=$(call shell_quote,$(PROJECT_TMP_ROOT)))
ROOT_RESOLVED := $(shell $(ROOT_ENV) bash -c 'source scripts/project-tmp.sh; project_tmp_resolve womprat')
ifeq ($(ROOT_RESOLVED),)
$(error Cannot resolve a safe Womprat temporary root)
endif
override PROJECT_TMP_ROOT := $(ROOT_RESOLVED)
export PROJECT_TMP_ROOT
override WOMPRAT_TMP_ROOT := $(PROJECT_TMP_ROOT)
override WOMPRAT_RUN_DIR := $(WOMPRAT_TMP_ROOT)/runs/make/$(shell date -u +%Y%m%dT%H%M%S)-$(shell echo $$$$)
export WOMPRAT_TMP_ROOT WOMPRAT_RUN_DIR
SHELL := $(CURDIR)/scripts/make-shell.sh
.SHELLFLAGS := -eu -o pipefail -c
# Paths/environment are enforced by scripts/paths.sh for every recipe.
# Retained profiles, binaries and logs live in evidence/, never in clean scopes.
APP       := womprat
VERSION   ?= 0.4.2
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
GO        ?= go
BUN       ?= bun
WINDRES   ?= llvm-windres
PYTHON    ?= python3

GOFLAGS   ?=
LDFLAGS   := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
GUIFLAGS  := -H windowsgui $(LDFLAGS)

CMD_DIR   := cmd/womprat
override DIST_DIR := $(WOMPRAT_TMP_ROOT)/build/dist
override TMP_DIR := $(WOMPRAT_RUN_DIR)/generated
override RESOURCE_DIR := $(WOMPRAT_TMP_ROOT)/build/resources
TEST_FLAGS ?=
TEST_PACKAGES ?= ./...
BUN_PROFILE := bash scripts/bun-profile.sh
DOC_ICON  := docs/icon.png
ICO       := $(CMD_DIR)/icon.ico
WINRES_ICO:= $(CMD_DIR)/winres/icon.ico
MANIFEST  := $(CMD_DIR)/womprat.manifest
RC        := $(CMD_DIR)/womprat.rc
RC_NOINC  := $(TMP_DIR)/womprat-noinclude.rc
RSRC_ARM64:= $(RESOURCE_DIR)/rsrc_windows_arm64.syso
RSRC_AMD64:= $(RESOURCE_DIR)/rsrc_windows_amd64.syso

EXE_ARM64 := $(DIST_DIR)/$(APP)-windows-arm64.exe
EXE_AMD64 := $(DIST_DIR)/$(APP)-windows-amd64.exe
BIN_LINUX := $(DIST_DIR)/$(APP)-linux-amd64
BIN_DARWIN:= $(DIST_DIR)/$(APP)-darwin-arm64

.PHONY: help all setup doctor deps tidy download patch verify test vet compile-windows frontend-check \
        resources resources-arm64 resources-amd64 icon icon-check windows windows-arm64 \
        windows-amd64 windows-intel intel linux linux-debug linux-gui ux-test darwin sha256 release release-intel dist \
        clean clean-generated clean-dist dev run status

.DEFAULT_GOAL := help

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage: make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: windows-arm64 ## Build the default Windows ARM64 executable

setup: doctor deps resources ## Validate tools, resolve Go deps, and generate resources

status: ## Show current build metadata
	@echo "APP=$(APP)"
	@echo "VERSION=$(VERSION)"
	@echo "COMMIT=$(COMMIT)"
	@echo "GO=$$($(GO) version 2>/dev/null || echo missing)"
	@echo "BUN=$$(command -v $(BUN) 2>/dev/null || echo missing)"
	@echo "WINDRES=$$(command -v $(WINDRES) 2>/dev/null || echo missing)"

doctor: ## Check required build tools are available
	@command -v $(GO) >/dev/null || { echo "missing Go toolchain" >&2; exit 1; }
	@command -v $(BUN) >/dev/null || { echo "missing bun (needed for frontend syntax/bundle checks)" >&2; exit 1; }
	@command -v $(WINDRES) >/dev/null || { echo "missing llvm-windres (needed for Windows resources)" >&2; exit 1; }
	@command -v $(PYTHON) >/dev/null || { echo "missing python3" >&2; exit 1; }
	@test -f $(DOC_ICON) || { echo "missing $(DOC_ICON)" >&2; exit 1; }
	@test -f $(MANIFEST) || { echo "missing $(MANIFEST)" >&2; exit 1; }

# Dependency lifecycle -------------------------------------------------------

download: ## Download Go modules without mutating go.mod/go.sum
	$(GO) mod download

tidy: ## Tidy Go modules (mutates go.mod/go.sum when needed)
	$(GO) mod tidy

deps: tidy download ## Tidy and download Go module dependencies

# Patch hook ----------------------------------------------------------------

patch: ## Apply optional patches from patches/*.patch, if present
	@if [ -d patches ] && ls patches/*.patch >/dev/null 2>&1; then \
		for p in patches/*.patch; do \
			echo "Applying $$p"; \
			git apply --check "$$p" && git apply "$$p"; \
		done; \
	else \
		echo "No patches/ directory or patches/*.patch files; nothing to apply."; \
	fi

# Frontend and tests ---------------------------------------------------------

frontend-check: ## Bundle-check embedded HTML/JS entry points with Bun
	@rm -rf $(TMP_DIR)/frontend-index $(TMP_DIR)/frontend-settings $(TMP_DIR)/frontend-vnc $(TMP_DIR)/frontend-rdp
	@mkdir -p $(TMP_DIR)
	$(BUN) build $(CMD_DIR)/frontend/index.html --outdir=$(TMP_DIR)/frontend-index
	$(BUN) build $(CMD_DIR)/frontend/settings.html --outdir=$(TMP_DIR)/frontend-settings
	$(BUN) build $(CMD_DIR)/frontend/vnc.js --outdir=$(TMP_DIR)/frontend-vnc
	$(BUN) build $(CMD_DIR)/frontend/rdp.js --outdir=$(TMP_DIR)/frontend-rdp

vet: ## Run go vet for the Windows ARM64 target
	GOOS=windows GOARCH=arm64 $(GO) vet ./...

test: ## Run Go tests with retained CPU/allocation profiles
	bash scripts/test-profile.sh host . '$(TEST_PACKAGES)' $(TEST_FLAGS)

.PHONY: test-race test-webview2 test-rdp paths
paths: ## Print effective project cache/build/temp locations
	@env | sort | grep -E '^(WOMPRAT_[A-Z_]+|GOCACHE|GOMODCACHE|GOPATH|GOTMPDIR|TMP|TEMP|BUN_INSTALL_CACHE_DIR|npm_config_cache|PLAYWRIGHT_BROWSERS_PATH|XDG_CACHE_HOME|WINEPREFIX)='

test-race: ## Run profiled race tests
	bash scripts/test-profile.sh race . '$(TEST_PACKAGES)' -race $(TEST_FLAGS)

test-webview2: ## Run profiled native Windows COM tests (Windows host required)
	bash scripts/test-profile.sh webview2 internal/go-webview2 ./pkg/edge $(TEST_FLAGS)

test-rdp: ## Run profiled internal library tests (vendored web assets are not built)
	bash scripts/test-profile.sh rdp third_party/go-rdp './internal/...' $(TEST_FLAGS)

compile-windows: resources ## Compile Windows arm64 and amd64 in isolated source trees
	bash scripts/windows-build.sh arm64 $(WOMPRAT_RUN_DIR)/compile-arm64.exe
	bash scripts/windows-build.sh amd64 $(WOMPRAT_RUN_DIR)/compile-amd64.exe

.PHONY: frontend-test
frontend-test: ## Run server-independent frontend and portable-path regressions
	$(BUN_PROFILE) frontend test tests/ux/paths.test.mjs tests/ux/rdp-resize.test.mjs tests/ux/terminal-controls.test.mjs tests/ux/frontend-state.test.mjs tests/ux/vnc-lifecycle.test.mjs

verify: frontend-check frontend-test test vet compile-windows ## Run all non-interactive checks

# Icons/resources ------------------------------------------------------------

icon-check: ## Verify icon inputs/outputs exist
	@test -f $(DOC_ICON) || { echo "missing $(DOC_ICON)" >&2; exit 1; }
	@test -f $(ICO) || { echo "missing $(ICO); run icon generation workflow" >&2; exit 1; }
	@test -f $(WINRES_ICO) || { echo "missing $(WINRES_ICO); run icon generation workflow" >&2; exit 1; }

icon: icon-check ## Validate icon assets (docs/icon.png, icon.ico, winres/icon.ico)
	@echo "Icon assets present: $(DOC_ICON), $(ICO), $(WINRES_ICO)"

$(RC_NOINC): $(ICO) $(MANIFEST) | $(TMP_DIR)
	@printf '1 ICON "icon.ico"\n1 24 "womprat.manifest"\n' > $@

$(TMP_DIR):
	@mkdir -p $@

$(DIST_DIR) $(RESOURCE_DIR):
	@source scripts/paths.sh; womprat_owned_dir '$@'

resources-arm64: icon $(RC_NOINC) | $(RESOURCE_DIR) ## Generate Windows ARM64 resource object outside source
	$(WINDRES) --target=aarch64-w64-windows-gnu -I $(CURDIR)/$(CMD_DIR) -O coff $(RC_NOINC) -o $(RSRC_ARM64)

resources-amd64: icon $(RC_NOINC) | $(RESOURCE_DIR) ## Generate Windows x64 resource object outside source
	$(WINDRES) --target=x86_64-w64-windows-gnu -I $(CURDIR)/$(CMD_DIR) -O coff $(RC_NOINC) -o $(RSRC_AMD64)

resources: resources-arm64 resources-amd64 ## Generate Windows resources under build/resources

# Builds ---------------------------------------------------------------------

windows-arm64: resources | $(DIST_DIR) ## Build Windows ARM64 GUI executable
	bash scripts/windows-build.sh arm64 $(EXE_ARM64) "$(GUIFLAGS)"
	@ls -lh $(EXE_ARM64)

windows: windows-arm64 ## Alias for Windows ARM64 build

windows-amd64: resources-amd64 | $(DIST_DIR) ## Build Windows AMD64/Intel x64 GUI executable
	bash scripts/windows-build.sh amd64 $(EXE_AMD64) "$(GUIFLAGS)"
	@ls -lh $(EXE_AMD64)

windows-intel: windows-amd64 ## Alias for Windows Intel/x64 build

intel: windows-intel ## Short alias for Windows Intel/x64 build

linux: | $(DIST_DIR) ## Build Linux AMD64 debug server binary (serves shell/API for Xvfb+xdotool debugging)
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_LINUX) ./$(CMD_DIR)
	@ls -lh $(BIN_LINUX)

ux-test: | $(DIST_DIR) ## Build a debug headless Linux binary and run the Playwright UX test
	$(GO) build -tags=profile -ldflags="-X main.debugBuild=1" -o $(DIST_DIR)/$(APP)-linux-debug ./$(CMD_DIR)
	WOMPRAT_BIN=$(DIST_DIR)/$(APP)-linux-debug $(BUN_PROFILE) ux run tests/ux/ux.mjs
	$(BUN_PROFILE) title test tests/ux/title-reporter.test.mjs

.PHONY: ux-setup
ux-setup: ## Install project-local browser dependencies and cached Chromium
	$(BUN) add --dev playwright
	$(BUN) x playwright install chromium

linux-debug: linux ## Build Linux binary and launch the Xvfb/xdotool debug harness
	WOMPRAT_BIN=$(BIN_LINUX) bash scripts/linux-debug.sh

linux-gui: | $(DIST_DIR) $(TMP_DIR) ## Build the Linux WebKitGTK GUI app (needs libgtk-3-dev + libwebkit2gtk-4.1-dev)
	@mkdir -p $(TMP_DIR)/pcshim
	@printf 'Name: webkit2gtk-4.0 (shim)\nDescription: shim -> 4.1\nVersion: 0\nRequires: webkit2gtk-4.1\n' > $(TMP_DIR)/pcshim/webkit2gtk-4.0.pc
	CGO_ENABLED=1 PKG_CONFIG=/usr/bin/pkg-config \
		PKG_CONFIG_PATH=$(TMP_DIR)/pcshim:/usr/lib/x86_64-linux-gnu/pkgconfig:/usr/share/pkgconfig \
		GOOS=linux GOARCH=amd64 $(GO) build -tags webkitgui -ldflags="$(LDFLAGS) -X main.debugBuild=1" -o $(DIST_DIR)/$(APP)-linux-gui ./$(CMD_DIR)
	@ls -lh $(DIST_DIR)/$(APP)-linux-gui

darwin: | $(DIST_DIR) ## Build Darwin ARM64 binary (for compile sanity only; app runtime is Windows-focused)
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DARWIN) ./$(CMD_DIR)
	@ls -lh $(BIN_DARWIN)

sha256: ## Write SHA256SUMS.txt for the built Windows executables
	@cd $(DIST_DIR) && sha256sum $(APP)-windows-*.exe > SHA256SUMS.txt && cat SHA256SUMS.txt

release: setup patch verify windows-arm64 ## Setup/patch/check/build pipeline for Windows ARM64

release-intel: setup patch verify windows-intel ## Setup/patch/check/build pipeline for Windows Intel/x64

# Local dev ------------------------------------------------------------------

run: ## Run locally with the host Go toolchain (non-Windows paths only)
	$(GO) run ./$(CMD_DIR)

dev: run ## Alias for local run

# Cleanup --------------------------------------------------------------------

# Explicit idle confirmation prevents accidental cleanup during another job.
clean-generated: ## Remove resource outputs only (requires WOMPRAT_CONFIRM_IDLE=1)
	@test "$${WOMPRAT_CONFIRM_IDLE:-}" = 1 || { echo 'Confirm no Womprat jobs are using build outputs: WOMPRAT_CONFIRM_IDLE=1'; exit 1; }
	@source scripts/paths.sh; womprat_owned_dir '$(RESOURCE_DIR)'; rm -rf -- '$(RESOURCE_DIR)'

clean-dist: ## Remove build/dist only (requires WOMPRAT_CONFIRM_IDLE=1)
	@test "$${WOMPRAT_CONFIRM_IDLE:-}" = 1 || { echo 'Confirm no Womprat jobs are using build outputs: WOMPRAT_CONFIRM_IDLE=1'; exit 1; }
	@source scripts/paths.sh; womprat_owned_dir '$(DIST_DIR)'; rm -rf -- '$(DIST_DIR)'

clean: clean-generated clean-dist ## Remove owned build outputs; never caches, runs or evidence
