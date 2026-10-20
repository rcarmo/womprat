<!-- RUI-PROFILE-LIFECYCLE-20261005 -->
## Current profiling and cleanup rule — supersedes older text below

Rui's explicit rule: **profile and tune during pre-release tests; remove profiling data immediately after analysis/use.** Ordinary development tests do not require profiling on every run. Targeted diagnostic profiling is optional when useful and follows the same disposal rule.

- During pre-release verification, capture CPU and heap/allocation behaviour, analyse hotspots and tune avoidable allocations/repeated work. Compare equivalent workloads without weakening correctness/security/numerical contracts. For Go, inspect CPU, alloc_space and alloc_objects; coverage alone is not profiling.
- **Act on measured outliers before declaring profiling complete.** Rank material CPU, allocation, memory, latency and repeated-work outliers; investigate their cause and implement safe fixes, then remeasure equivalent workloads and verify correctness. Recording a hotspot and deleting captures is not completion. Judge optimizations by their measured overall benefit, not a zero-regression rule for every metric. Minor, quantified regressions in secondary performance/resource metrics are acceptable when justified by a larger verified improvement or a concrete incremental path to it. Compare equivalent workloads, account for measurement noise, document absolute and relative changes, downstream impact and the next validation step; do not assume speculative future gains. Preserve correctness, cancellation, security, isolation and numerical contracts. Revert changes with no demonstrated benefit or unacceptable net regressions; do not automatically reject a useful increment merely because another metric moves slightly backwards. Prioritise completing the release unless measurements show a major performance issue; do not expand optimisation indefinitely. Minor residual issues and justified secondary regressions may be documented as non-blocking follow-ups under Rui’s release direction. If a major outlier cannot safely be fixed within scope, record its measurements, cause or uncertainty, attempted fixes, concrete blocker and an owned follow-up; explicitly disclose it as unresolved and obtain Rui’s acceptance before treating pre-release profiling as complete. Do not silently defer it because an API contract or harness is involved; investigate contract-preserving options first. Dispose of captures only after the current analysis/comparison is finished; retain concise before/after conclusions, not raw archives.
- Keep captures and matching artifacts only while the current analysis needs them. Once used, immediately delete raw profiles, traces, matching test binaries, temporary fixtures and disposable logs, including failed/probe artifacts after diagnosis. Retain only concise conclusions and important measurements/limitations. Do not keep indefinite raw archives or copy them into exports/reports/evidence to evade cleanup.
- Remove all completed disposable cache/build/test/run data promptly. Never delete files still in use: finish or safely pause the owning job and clean at a safe boundary. Preserve source, installed toolchains, durable datasets/checkpoints and intentional release assets. Minimise disk usage; no random exports or redundant snapshots.
- This rule overrides **every conflicting older paragraph in this file and linked local guidance**, including “profile every test”, “unprofiled tests prohibited”, “retain all raw evidence”, “never delete profiles” and cleanup exclusions based only on an evidence/profiles directory name. Update helper/CI cleanup behaviour accordingly; do not weaken pre-release analysis.
- Existing no-agent-contact and execution-pause rules remain unchanged. This policy grants no unsolicited coordination or automatic job restart.
<!-- /RUI-PROFILE-LIFECYCLE-20261005 -->



# Womprat development

Canonical project name: `womprat`.

Project Go toolchain: **1.27.3**, selected by scripts/paths.sh and all three go.mod files; CI uses the same version. Tailscale **1.104.1** uses its own tested gVisor revision `v0.0.0-20260915211658-a6f909f08a72`: latest gVisor `v0.0.0-20261020074958-d5b092c05305` fails standard Go package discovery (`stack` / `bridge_test` conflict). Do not replace this compatibility pin without rechecking builds. DNS uses read-only LocalAPI current-netmap and tsnet's UseNetstackForIP route verifier; direct bound netstack dialing must never fall back to host networking.

Certificate exceptions require a native certificate event, matching URL/leaf certificate and one-use tab-bound consent token. WebView2 ALWAYS_ALLOW caches host/certificate for the shared browser session; this is disclosed in the banner. Never add global ignore-certificate flags or bypass revoked/client-certificate errors.

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

Clean completed owned cache/build/run/profiling output after use, requiring idle consumers and existing ownership/symlink guards. Preserve source, installed toolchains, other projects, durable data/release assets and concise conclusions. Raw profiles and disposable logs are not exempt from cleanup.

## Tests and evidence

Use `make test`, `make test-race`, `make frontend-test`, `make ux-test`, `make test-rdp`, and `make test-webview2` (native Windows only), with TEST_PACKAGES/TEST_FLAGS for focus. During pre-release verification use `scripts/test-profile.sh`, including relevant nested modules, to capture and analyse CPU/heap. `runs/<purpose>/<run-id>/profiles/<label>-<capture-id>/` is transient capture storage: delete profiles, matching binaries, commands and full logs after use, including failed-run output after diagnosis. CI uploads one-day temporary analysis inputs. Delete those artifacts immediately after review using the GitHub artifact API; retain concise conclusions only. PROFILE=0 is the default for ordinary make targets; releases use PROFILE=1. Ordinary development tests need not be profiled.

During pre-release profiling, inspect cumulative CPU, alloc_space and alloc_objects. Separate application costs from test/runtime overhead; tune avoidable allocations, then compare equivalent workloads. Record concise results and missing/empty/subprocess capture limitations. Delete raw artifacts after the analysis; generated tables alone do not complete it.

During pre-release verification, Bun CPU/heap capture may use scripts/bun-profile.sh and bun:jsc.profile; avoid ignored test CLI flags. Chromium renderer CPU/sampled allocations and a profiling-only server build may supplement it. Live heap is not cumulative allocations, and utility/GPU/native processes may be excluded: report that boundary. Analyse/tune, keep concise conclusions, then immediately delete raw captures and disposable logs. Ordinary development tests need not be profiled; remote/native-device tests still require approval.

`third_party/go-rdp` is a vendor copy. Its Go test recipes share Womprat paths and
profiling without narrowing the upstream package patterns. Root `make test-rdp`
explicitly covers only internal libraries; full vendor targets also require
frontend assets absent from this checkout and may fail package discovery. Standalone upstream frontend/build/install/docker/watch/JS recipes
are blocked because they write outside the project hierarchy; use the owning
upstream project for that work. Preserve the path/isolation guard; ordinary development tests need not be profiled.

Debug/UX homes remain isolated under runs; never point test mutations at real
config or authenticated tailnet state. The debug script requires an existing
display: Xvfb creates host `/tmp` sockets regardless of `TMPDIR`, so starting it
requires a user-approved exception or separately managed display.

Preserve persisted exit-node routing and tailnet-only DNS. Synthetic tests and
Wine do not establish native Windows ARM64 acceptance. Never overwrite a release.
