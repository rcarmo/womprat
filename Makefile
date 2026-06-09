# womprat project Makefile
#
# Goals:
# - keep the normal path one-command simple (`make release`)
# - make build prerequisites explicit (`make setup` / `make doctor`)
# - keep generated Windows resources reproducible from checked-in assets
# - provide a stable hook for local dependency/module patches

APP       := womprat
VERSION   ?= 0.1.0
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
GO        ?= go
BUN       ?= bun
WINDRES   ?= llvm-windres
PYTHON    ?= python3

GOFLAGS   ?=
LDFLAGS   := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
GUIFLAGS  := -H windowsgui $(LDFLAGS)

DIST_DIR  := dist
TMP_DIR   := .tmp
DOC_ICON  := docs/icon.png
ICO       := icon.ico
WINRES_ICO:= winres/icon.ico
MANIFEST  := womprat.manifest
RC        := womprat.rc
RC_NOINC  := $(TMP_DIR)/womprat-noinclude.rc
RSRC_ARM64:= rsrc_windows_arm64.syso

EXE_ARM64 := $(APP).exe
EXE_AMD64 := $(APP)-amd64.exe
BIN_LINUX := $(APP)-linux-amd64
BIN_DARWIN:= $(APP)-darwin-arm64

.PHONY: help all setup doctor deps tidy download patch verify test vet frontend-check \
        resources resources-arm64 icon icon-check windows windows-arm64 windows-amd64 \
        linux darwin release dist clean clean-generated clean-dist dev run status

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
	@rm -rf $(TMP_DIR)/frontend-index $(TMP_DIR)/frontend-settings
	@mkdir -p $(TMP_DIR)
	$(BUN) build frontend/index.html --outdir=$(TMP_DIR)/frontend-index
	$(BUN) build frontend/settings.html --outdir=$(TMP_DIR)/frontend-settings

vet: ## Run go vet for the Windows ARM64 target
	GOOS=windows GOARCH=arm64 $(GO) vet ./...

test: ## Run Go tests for the Windows ARM64 target
	GOOS=windows GOARCH=arm64 $(GO) test ./...

verify: frontend-check test vet ## Run all non-interactive checks

# Icons/resources ------------------------------------------------------------

icon-check: ## Verify icon inputs/outputs exist
	@test -f $(DOC_ICON) || { echo "missing $(DOC_ICON)" >&2; exit 1; }
	@test -f $(ICO) || { echo "missing $(ICO); run icon generation workflow" >&2; exit 1; }
	@test -f $(WINRES_ICO) || { echo "missing $(WINRES_ICO); run icon generation workflow" >&2; exit 1; }

icon: icon-check ## Validate icon assets (docs/icon.png, icon.ico, winres/icon.ico)
	@echo "Icon assets present: $(DOC_ICON), $(ICO), $(WINRES_ICO)"

$(RC_NOINC): $(ICO) $(MANIFEST) | $(TMP_DIR)
	@printf '1 ICON "$(ICO)"\n1 24 "$(MANIFEST)"\n' > $@

$(TMP_DIR):
	@mkdir -p $@

resources-arm64: icon $(RC_NOINC) ## Generate Windows ARM64 resource object (.syso)
	$(WINDRES) --target=aarch64-w64-windows-gnu -I $(CURDIR) -O coff $(RC_NOINC) -o $(RSRC_ARM64)

resources: resources-arm64 ## Generate all checked-in Windows resource objects

# Builds ---------------------------------------------------------------------

windows-arm64: resources ## Build Windows ARM64 GUI executable
	GOOS=windows GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags="$(GUIFLAGS)" -o $(EXE_ARM64) .
	@ls -lh $(EXE_ARM64)

windows: windows-arm64 ## Alias for Windows ARM64 build

windows-amd64: ## Build Windows AMD64 GUI executable (resource object not generated here)
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags="$(GUIFLAGS)" -o $(EXE_AMD64) .
	@ls -lh $(EXE_AMD64)

linux: ## Build Linux AMD64 binary (for compile sanity only; app runtime is Windows-focused)
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_LINUX) .
	@ls -lh $(BIN_LINUX)

darwin: ## Build Darwin ARM64 binary (for compile sanity only; app runtime is Windows-focused)
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DARWIN) .
	@ls -lh $(BIN_DARWIN)

release: clean setup patch verify windows-arm64 ## Full clean setup/patch/check/build pipeline

# Local dev ------------------------------------------------------------------

run: ## Run locally with the host Go toolchain (non-Windows paths only)
	$(GO) run .

dev: run ## Alias for local run

# Cleanup --------------------------------------------------------------------

clean-generated: ## Remove generated temporary files
	rm -rf $(TMP_DIR)

clean-dist: ## Remove dist directory
	rm -rf $(DIST_DIR)

clean: clean-generated ## Remove build products
	rm -f $(EXE_ARM64) $(EXE_AMD64) $(BIN_LINUX) $(BIN_DARWIN)
