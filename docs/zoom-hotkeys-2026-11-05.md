# Zoom hotkey correction

Browser tabs now route zoom-in, zoom-out and reset to the native WebView on Windows and Linux. Browser shortcuts are `Ctrl++` / `Ctrl+=`, `Ctrl+-` and `Ctrl+0`; Shift and numeric-keypad variants work too. These act on the native view even when the address bar has focus. Browser tabs retain independent zoom, bounded to 30–300%; reset is 100%.

Terminals require `Ctrl+Shift++` / `Ctrl+Shift+=`, `Ctrl+Shift+-` and `Ctrl+Shift+0`. Reset restores the configured font size. Ordinary Ctrl chords stay reserved for the remote shell. Settings iframe keyboard events now reach the shell's zoom/reset handler. Hiding Linux native browser content returns GTK keyboard focus to the shell, so terminal keys reach xterm.

## Verification

Source `79fa5fb` adds routing, tests and Windows ABI bindings; `e502839` fixes the GTK focus transition. Native Windows COM/ABI tests passed on amd64 and arm64 in validation-only release run [42004448665](https://github.com/rcarmo/womprat/actions/runs/42004448665). That run's Linux job failed the terminal focus assertion; no publication occurred. The corrected full Fedora/Atomic run [42005697271](https://github.com/rcarmo/womprat/actions/runs/42005697271) passed browser viewport changes with page/address focus, shifted reset, terminal font increase/decrease/reset, all other native lifecycle/security checks, shipping restart sessions and profiling. Native Windows UI keyboard integration was not run; native COM float ABI/HRESULT tests and shell/browser-message keyboard integration were run separately. Windows ARM64 uses a native trampoline to place the double in D0; amd64 uses XMM1 through SyscallN.

Local verification: 60 Bun regression tests, full Go/race tests, Windows amd64/arm64 compile checks, frontend bundle checks and actionlint passed. `make zoom-test` passes with real Chromium keyboard events for terminal/Settings focus and injected content-bridge messages. Unit regressions cover shifted/keypad aliases, terminal Ctrl-only pass-through, reset and active native-view dispatch. The broad existing `make ux-test` stopped at unrelated setup/history overlays intercepting pointer clicks; it is not reported as passing. Failed probe logs were inspected and removed. The focused zoom test was added separately without weakening those broader assertions.

## Profile review

Native Windows tests (20 repetitions per architecture): amd64 150ms sampled CPU, 5385KiB / 41163 objects; arm64 160ms, 5301KiB / 39644 objects. Small test runtime was dominated by allocation-profiling/GC overhead; both native-float ABI tests passed. Host Go contracts: 430ms CPU over 5.53s, 14592KiB / 37494 objects, chiefly test HTTP setup and password KDF work.

Corrected Fedora native Go: 750ms sampled CPU over 3.51s, 8626KiB / 34191 objects. GTK-linked contracts: 360ms over 5.91s, 15135KiB / 38526 objects. Native perf: 236 samples, zero lost, loader relocation about 26.3% cumulative with renderer stacks. GTK host heaptrack: 262939 calls, 26020 temporary, 4.42MB peak heap, 224.13MB peak RSS including overhead. Same startup/security/persistence/fixture costs as prior workloads; no new material application outlier or performance-gain claim. Sandboxed renderer allocation coverage and steady-state limits remain unchanged.

Focused local zoom workload also captured Bun CPU, a live Bun heap and sampled Chromium CPU/allocations. Bun sampled 258 stacks, dominated by anonymous/runtime/Playwright initialisation; live heap leaders were compiled code (~7.52MB), function closures (~3.75MB) and object shapes (~2.51MB). Renderer samples were mostly idle/program with xterm glyph/atlas rendering the largest named work; sampled allocations were V8/compiler, xterm and WebGL setup. This short workload's Go CPU capture had zero samples; its 2727KiB / 2961 allocated objects were dominated by profiling overhead, so it is not CPU evidence. The full Fedora capture above is nonempty. No material new zoom-allocation hotspot identified. Captures, matching binaries, heap snapshots, renderer traces and remote profile artifacts were deleted after review; only these conclusions and intentional candidate assets remain.

## Test candidate

The downloaded Linux AppImage embeds Go 1.27.3, version 0.4.6 / commit e502839, linux/amd64 v1, webkitgui only, without test/profile tags or direct-dial override. Checksum verified after download:

```text
c9e8d9ff4b6b7a59778a75becbf69f074bf7ff2e7605b57c450622fe7965e425  Womprat-0.4.6-x86_64.AppImage
```

This is a test candidate; the public v0.4.5 release and its tag have not changed. Live authenticated tailnet validation remains separate from the keyboard correction.
