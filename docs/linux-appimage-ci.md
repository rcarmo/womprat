# Linux AppImage acceptance

Linux AppImage CI is wired into main pushes, pull requests, manual validation and the release workflow. It currently fails at `make linux-release-check`: the Linux browser host and its acceptance hooks have not been implemented. The existing `webkitgui` target navigates one WebView and replaces the shell; it cannot produce an accepted AppImage.

Windows jobs remain independent. A failed Linux gate makes the overall release run fail and withholds Linux assets; Windows publication can still finish. Existing releases and tags are unchanged.

## Build and acceptance hooks

`make linux-appimage VERSION=<version> COMMIT=<sha>` runs `scripts/linux-release.sh`. The browser port must implement these checked-in hooks before the gate can advance:

| Hook | Invocation | Required behaviour |
| --- | --- | --- |
| `scripts/linux-appimage-build.sh` | `--output <absolute AppImage path>` | Build normal release flags, bundle native GUI/WebKit helpers and required dependencies, desktop metadata, icon and licence notices. Never substitute the headless server or enable debug direct dialing. `VERSION` and `COMMIT` are environment values. |
| `tests/linux/browser-parity.mjs` | `--appimage <path> --profiles <dir> --require-release` | Exercise the packaged GUI and the browser/network contracts below; fail on unsupported or skipped requirements. |
| `tests/linux/fedora-appimage.sh` | `--appimage <path> --profiles <dir> --fedora-version 44` | Run the same package as a normal user on Fedora 44, without host GTK/WebKit packages masking missing dependencies; test launch and browser acceptance, extraction fallback, shutdown and clean isolated state. |

Hook presence is an implementation preflight, not functional acceptance. Both test hooks must return success before assets reach `build/dist` or CI's `linux-appimage-release` artifact. Do not add placeholder hooks, green smoke-only tests, environment bypasses, `continue-on-error` or unconditional uploads to enable publication.

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

Transient `linux-appimage-profiles` artifacts have one-day retention for current analysis. Analyse promptly, retain concise conclusions and delete remote artifacts and matching local profiles/binaries/logs immediately after use. Failed/probe scratch follows the same rule. Never include profiles in published release assets.

## Publication

The release workflow adds `Womprat-<version>-x86_64.AppImage` and `LINUX-SHA256SUMS.txt` only after Linux acceptance and the Windows release job succeed. `validate_only=true` builds/tests without publishing. The Linux publisher rechecks the accepted checksum artifact. No current AppImage is accepted or published.
