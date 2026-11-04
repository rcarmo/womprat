# Linux AppImage acceptance

Linux AppImage CI is wired into main pushes, pull requests, manual validation and the release workflow. The GTK/WebKitGTK 4.1 host now keeps the shell and browser views separate. Native fixtures cover create/switch/close/reorder, popup/link navigation, back, managed HttpOnly-cookie downloads, TLS consent and cookie deletion. AppImage hooks build on Debian 12, audit dependencies without host GTK/WebKit, and run normal-user Fedora 44 acceptance. `make linux-release-check` checks hook presence only. The 0.4.6 candidate passed the full Fedora 44 pipeline; checksum, packaged version and profile analysis are recorded in [the native run report](linux-acceptance-2026-11-01.md). The development host still forbids normal-user UID maps, so local container tests do not replace Fedora CI. Live authenticated tailnet checks are separate from synthetic fixtures.

Windows jobs remain independent. A failed Linux gate makes the overall release run fail and withholds Linux assets; Windows publication can still finish. Existing releases and tags are unchanged.

## Build and acceptance hooks

`make linux-appimage VERSION=<version> COMMIT=<sha>` runs `scripts/linux-release.sh`. The checked-in hooks implement these steps:

| Hook | Invocation | Required behaviour |
| --- | --- | --- |
| `scripts/linux-appimage-build.sh` | `--output <absolute AppImage path>` | Build normal release flags, bundle native GUI/WebKit helpers and required dependencies, desktop metadata, icon and licence notices. Never substitute the headless server or enable debug direct dialing. `VERSION` and `COMMIT` are environment values. |
| `tests/linux/browser-parity.mjs` | `--appimage <path> --profiles <dir> --require-release` | Validate the actual AppImage format and run the dependency/release-flag preflight; the Fedora hook below executes the native workload. |
| `tests/linux/fedora-appimage.sh` | `--appimage <path> --profiles <dir> --fedora-version 44` | Run the same package as a normal user on Fedora 44, without host GTK/WebKit packages masking missing dependencies; test launch and browser acceptance, extraction fallback, shutdown and clean isolated state. |

Preflight requires the launcher, layout regression, native runner, restart test, release canary and close helper as regular checked-in files as well as the three entry hooks. Missing or symlinked files block packaging. Hook presence is an implementation preflight, not functional acceptance. Both test hooks must return success before assets reach `build/dist` or CI's `linux-appimage-release` artifact. Do not add placeholder hooks, green smoke-only tests, environment bypasses, `continue-on-error` or unconditional uploads to enable publication.

## Fedora Atomic layouts

Main pushes, PRs, manual builds and releases use the same required job. It tests AppRun as a normal user with `/opt` as a directory, an absolute symlink to `/var/opt`, and a relative symlink to `var/opt`. Each launch must reach the payload once, preserve arguments and leave the outer filesystem unchanged. The full shipping GUI, network canary and process-restart tests then run with `/opt -> var/opt`; sandboxing stays enabled. This models the affected filesystem layout inside Fedora 44, not an installed Atomic desktop or its complete SELinux/Wayland environment.

The reusable CI job executes `tests/ux/linux-release.test.mjs` before packaging to guard this test sequence and release dependency. Tag and manual releases call that job; Linux publication needs its success and rechecks `LINUX-SHA256SUMS.txt`. Windows jobs remain independent. Adding the workflow does not itself publish a release or replace live tailnet verification.

## Required browser contracts

Acceptance must cover:

- persistent shell and independent content views, with stable tab IDs, create/switch/close/reorder/restore and inactive-tab isolation;
- URL, title, favicon, loading/error state, back/forward/reload and content zoom synchronised with the address bar and tab strip;
- links, popups and special SSH/VNC/RDP URLs using the same validation and dispatch as the Windows client;
- browsing through Womprat's loopback proxy and embedded tailnet/exit-node route, fail-closed DNS and connection errors, and no host DNS, LAN/direct fallback or proxy bypass;
- trusted independent network canaries proving denied traffic never reaches the host route, including startup, reconnect, redirects and subprocesses;
- managed downloads, same-origin cookie handling, cancellation/cleanup, cookie/site-data controls and TLS diagnostics/consent without global bypasses;
- release builds ignoring `WOMPRAT_DIRECT=1`, authenticated local APIs, Host-header guard and browser/shell isolation;
- native input/focus, resizing/DPI, WebKit helper relocation and normal-user sandbox operation on Fedora 44; no sandbox-disable flags.

A real device/tailnet check may require separate operator approval. Synthetic routing fixtures must state their coverage and must not silently report live validation.

## Paths and profiles

All hooks source `scripts/paths.sh` or inherit its resolved configuration. CI chooses `$RUNNER_TEMP/womprat`; local runs normally use `/workspace/tmp/womprat`. Build and test staging is below `runs/<purpose>/<run-id>/linux-release-*/`, with outputs in `build/dist` only after acceptance. Container source staging, package-manager caches and temporary display sockets use project-owned mounts; no source/evidence mutations or host `/tmp` display sockets.

Acceptance captures CPU and allocation behaviour of the Go backend and relevant GUI/browser processes. Inspect cumulative CPU, `alloc_space` and `alloc_objects` for Go, investigate measured outliers and remeasure equivalent workloads. Report missing/sparse captures and subprocess boundaries. A screenshot or headless Go test is insufficient.

The native acceptance runner captures Go CPU/heap, heaptrack allocations and inherited perf CPU samples. Perf/heaptrack failure or missing captures fail acceptance. Capture boundaries, WebKit sandbox/preload interaction and equivalent-workload outliers require analysis before release completion.

Transient `linux-appimage-profiles` artifacts have one-day retention for current analysis. Analyse promptly, retain concise conclusions and delete remote artifacts and matching local profiles/binaries/logs immediately after use. Failed/probe scratch follows the same rule. Never include profiles in published release assets.

## Publication

The release workflow adds `Womprat-<version>-x86_64.AppImage` and `LINUX-SHA256SUMS.txt` only after Linux acceptance and the Windows release job succeed. `validate_only=true` builds/tests without publishing. The Linux publisher rechecks the accepted checksum artifact. On 4 November 2026, Rui explicitly requested retagging and rereleasing v0.4.5 with the AppImage. Run 41915187743 passed and published the Linux and rebuilt Windows assets at commit 830bf7c; the public downloads passed checksum/build inspection. This supersedes the earlier candidate-only publication status. Live authenticated tailnet browsing/recovery remains unverified; that separate check needs an authorised existing session or a disposable test auth key and target. Do not copy real user state into fixtures or turn on direct dialing to replace that check.
