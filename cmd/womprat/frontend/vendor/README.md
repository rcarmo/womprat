# Vendored xterm.js runtime

This directory vendors the xterm.js browser runtime used by Womprat's SSH terminal tabs, so the executable does not fetch terminal code or CSS at runtime.

Vendored packages:

- `@xterm/xterm` 6.0.0
- `@xterm/addon-attach` 0.12.0
- `@xterm/addon-canvas` 0.7.0
- `@xterm/addon-clipboard` 0.2.0
- `@xterm/addon-fit` 0.11.0
- `@xterm/addon-image` 0.9.0
- `@xterm/addon-ligatures` 0.10.0
- `@xterm/addon-progress` 0.2.0
- `@xterm/addon-search` 0.16.0
- `@xterm/addon-serialize` 0.14.0
- `@xterm/addon-unicode-graphemes` 0.4.0
- `@xterm/addon-unicode11` 0.9.0
- `@xterm/addon-web-links` 0.12.0
- `@xterm/addon-webgl` 0.19.0

All are MIT licensed by the xterm.js authors. Womprat currently imports xterm core plus the Attach, Clipboard, Fit, Search, Unicode 11, Unicode Graphemes, Web Links, and WebGL add-ons from this directory. The other checked-in packages are retained as part of the pinned vendor set. See [LICENSES.md](LICENSES.md) for the bundled notices.
