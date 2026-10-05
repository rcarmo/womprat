# Womprat development

Canonical project name: `womprat`.

## Scratch and caches

Resolve the root once, before replacing child `TMPDIR`. An absolute
`PROJECT_TMP_BASE` selects `<base>/womprat`. An absolute `PROJECT_TMP_ROOT`
ending in `womprat` is also supported; if both are supplied they must agree.
Invalid, unusable or conflicting overrides fail without fallback.

Without overrides, CI tries `$RUNNER_TEMP`, the original inherited `$TMPDIR`,
then platform temp, even when `/workspace/tmp` exists. Local runs prefer usable
`/workspace/tmp`, then platform temp. Always append `/womprat` (POSIX fallback:
`/tmp/womprat`). The resolver snapshots `PROJECT_ORIGINAL_TMPDIR` before any
redirection and exports the resolved root to children; nested commands never
append `womprat` again. Every root uses this hierarchy:

- `cache/go/{build,mod,path}`, `cache/{bun,npm,playwright,xdg,wine,python,pip,uv,golangci-lint,tinygo}`
- `build/{dist,resources,tools}`
- `tests/` and `logs/` for disposable test/log scratch when needed
- `runs/<purpose>/<run-id>/` for isolated temporary files, source staging and test homes

The Makefile uses `scripts/make-shell.sh` for every recipe. Direct commands must first
`source scripts/paths.sh`. It sets `TMPDIR`, `TMP`, `TEMP`, `GOTMPDIR`, `GOCACHE`,
`GOMODCACHE`, `GOPATH`, `GOBIN`, Bun/npm caches, Playwright, XDG cache, Python, Wine,
TinyGo and lint caches. Do not override these with home caches or bare `/tmp`.
`make paths` shows effective settings. Installed toolchains/dependencies are durable;
this policy does not relocate them.

The checked-in `scripts/project-tmp.sh` implements the portable resolver; CI has
no dependency on `/workspace/Makefile` or any other host helper. GitHub runners
select `$RUNNER_TEMP/womprat` when usable, regardless of `/workspace/tmp`.
Go setup caching is disabled; project commands use the environment wrapper.
Runner-owned tool installation is separate from project caches.

Generated Windows `.syso` files live in `build/resources`. Windows builds copy
sources into an isolated run directory because Go needs resources beside package
sources. Existing source-side `.syso`, `dist`, `.tmp`, home caches and older
`/workspace/tmp/womprat-*` artifacts are historical; do not remove or relocate
without checking owners, active jobs and evidence retention.

`make clean` removes only `build/dist` and `build/resources` and requires
`WOMPRAT_CONFIRM_IDLE=1`. Confirm all users of those outputs are idle first.
It never deletes caches, runs, source, toolchains, another project or evidence.
Run scratch cleanup is manual and limited to a completed run's owned directory.
Symlink/ownership checks in `scripts/paths.sh` must remain intact.

## Tests and evidence

Use `make test`, `make test-race`, `make frontend-test`, `make ux-test`,
`make test-rdp`, and `make test-webview2` (native Windows only). Set
`TEST_PACKAGES`/`TEST_FLAGS` for focused workloads. All Go execution goes through
`scripts/test-profile.sh`, including nested modules. It retains per-package test
binaries, CPU and memory profiles, command lines, toolchain/revision metadata and
logs under `evidence/tests/<label>-<run-id>/`. Evidence is ignored by Git, durable,
and outside every cleanup scope. CI uploads it even on failure.

Inspect cumulative CPU, `alloc_space` and `alloc_objects` after every Go run.
Record application hotspots separately from test/runtime overhead in an evidence
analysis note. Generated top tables alone do not complete this review. Missing
profiles are failures; empty CPU samples need a representative repeated workload.
Record baseline comparisons and allocation/latency results when making performance
claims. Do not silently accept unprofiled subprocesses or fuzz workers.

Bun tests use `scripts/bun-profile.sh` for per-file CPU/heap capture. Bun 1.4.2
ignores test CLI profile flags; the preload uses `bun:jsc.profile` and a full
V8-format heap snapshot. Heap snapshots show retained objects, not cumulative
allocations; Chromium additionally records sampled allocations. Browser checks also
capture Chromium renderer CPU and sampled allocations; the Linux UX server uses
a profiling-only build tag and flushes profiles on shutdown. Browser/driver
profiles do not cover every Chromium utility/GPU process; report that boundary.
No live remote tests or native-device claims without explicit access/approval.

`third_party/go-rdp` is a vendor copy. Its Go test recipes share Womprat paths and
profiling without narrowing the upstream package patterns. Root `make test-rdp`
explicitly covers only internal libraries; full vendor targets also require
frontend assets absent from this checkout and may fail package discovery. Standalone upstream frontend/build/install/docker/watch/JS recipes
are blocked because they write outside the project hierarchy; use the owning
upstream project for that work. Do not weaken this guard to run a test unprofiled.

Debug/UX homes remain isolated under runs; never point test mutations at real
config or authenticated tailnet state. The debug script requires an existing
display: Xvfb creates host `/tmp` sockets regardless of `TMPDIR`, so starting it
requires a user-approved exception or separately managed display.

Preserve persisted exit-node routing and tailnet-only DNS. Synthetic tests and
Wine do not establish native Windows ARM64 acceptance. Never overwrite a release.
