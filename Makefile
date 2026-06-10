# womprat project Makefile
#
# Goals:
# - keep the normal path one-command simple (`make release`)
# - make build prerequisites explicit (`make setup` / `make doctor`)
# - keep generated Windows resources reproducible from checked-in assets
# - provide a stable hook for local dependency/module patches

APP       := womprat
VERSION   ?= 0.3.0
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
GO        ?= go
BUN       ?= bun
WINDRES   ?= llvm-windres
PYTHON    ?= python3

GOFLAGS   ?=
LDFLAGS   := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
GUIFLAGS  := -H windowsgui $(LDFLAGS)

CMD_DIR   := cmd/womprat
DIST_DIR  := dist
TMP_DIR   := .tmp
DOC_ICON  := docs/icon.png
ICO       := $(CMD_DIR)/icon.ico
WINRES_ICO:= $(CMD_DIR)/winres/icon.ico
MANIFEST  := $(CMD_DIR)/womprat.manifest
RC        := $(CMD_DIR)/womprat.rc
RC_NOINC  := $(TMP_DIR)/womprat-noinclude.rc
RSRC_ARM64:= $(CMD_DIR)/rsrc_windows_arm64.syso
RSRC_AMD64:= $(CMD_DIR)/rsrc_windows_amd64.syso

EXE_ARM64 := $(DIST_DIR)/$(APP)-windows-arm64.exe
EXE_AMD64 := $(DIST_DIR)/$(APP)-windows-amd64.exe
BIN_LINUX := $(DIST_DIR)/$(APP)-linux-amd64
BIN_DARWIN:= $(DIST_DIR)/$(APP)-darwin-arm64

.PHONY: help all setup doctor deps tidy download patch verify test vet compile-windows frontend-check \
        resources resources-arm64 resources-amd64 icon icon-check windows windows-arm64 \
        windows-amd64 windows-intel intel linux darwin sha256 release release-intel dist \
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

test: ## Run Go tests on the host toolchain
	$(GO) test ./...

compile-windows: ## Compile-only check for Windows arm64 and amd64
	GOOS=windows GOARCH=arm64 $(GO) build $(GOFLAGS) -o /dev/null ./$(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -o /dev/null ./$(CMD_DIR)

verify: frontend-check test vet compile-windows ## Run all non-interactive checks

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

$(DIST_DIR):
	@mkdir -p $@

resources-arm64: icon $(RC_NOINC) ## Generate Windows ARM64 resource object (.syso)
	$(WINDRES) --target=aarch64-w64-windows-gnu -I $(CURDIR)/$(CMD_DIR) -O coff $(RC_NOINC) -o $(RSRC_ARM64)

resources-amd64: icon $(RC_NOINC) ## Generate Windows Intel/x64 resource object (.syso)
	$(WINDRES) --target=x86_64-w64-windows-gnu -I $(CURDIR)/$(CMD_DIR) -O coff $(RC_NOINC) -o $(RSRC_AMD64)

resources: resources-arm64 resources-amd64 ## Generate all checked-in Windows resource objects

# Builds ---------------------------------------------------------------------

windows-arm64: resources | $(DIST_DIR) ## Build Windows ARM64 GUI executable
	GOOS=windows GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags="$(GUIFLAGS)" -o $(EXE_ARM64) ./$(CMD_DIR)
	@ls -lh $(EXE_ARM64)

windows: windows-arm64 ## Alias for Windows ARM64 build

windows-amd64: resources-amd64 | $(DIST_DIR) ## Build Windows AMD64/Intel x64 GUI executable
	GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags="$(GUIFLAGS)" -o $(EXE_AMD64) ./$(CMD_DIR)
	@ls -lh $(EXE_AMD64)

windows-intel: windows-amd64 ## Alias for Windows Intel/x64 build

intel: windows-intel ## Short alias for Windows Intel/x64 build

linux: | $(DIST_DIR) ## Build Linux AMD64 binary (for compile sanity only; app runtime is Windows-focused)
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_LINUX) ./$(CMD_DIR)
	@ls -lh $(BIN_LINUX)

darwin: | $(DIST_DIR) ## Build Darwin ARM64 binary (for compile sanity only; app runtime is Windows-focused)
	GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BIN_DARWIN) ./$(CMD_DIR)
	@ls -lh $(BIN_DARWIN)

sha256: ## Write SHA256SUMS.txt for the built Windows executables
	@cd $(DIST_DIR) && sha256sum *.exe > SHA256SUMS.txt && cat SHA256SUMS.txt

release: clean setup patch verify windows-arm64 ## Full clean setup/patch/check/build pipeline for Windows ARM64

release-intel: clean setup patch verify windows-intel ## Full clean setup/patch/check/build pipeline for Windows Intel/x64

# Local dev ------------------------------------------------------------------

run: ## Run locally with the host Go toolchain (non-Windows paths only)
	$(GO) run ./$(CMD_DIR)

dev: run ## Alias for local run

# Cleanup --------------------------------------------------------------------

clean-generated: ## Remove generated temporary files
	rm -rf $(TMP_DIR)

clean-dist: ## Remove dist directory
	rm -rf $(DIST_DIR)

clean: clean-generated clean-dist ## Remove build products
	rm -f $(EXE_ARM64) $(EXE_AMD64) $(BIN_LINUX) $(BIN_DARWIN)
