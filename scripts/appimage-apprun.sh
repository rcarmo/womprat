#!/usr/bin/env bash
set -euo pipefail
APPDIR="$(cd -- "$(dirname -- "$(readlink -f -- "$0")")" && pwd)"
# Stable helper path lives only in this process's private mount namespace.
# WebKit's inner sandbox cannot mount /proc/self/cwd symlink paths. No files
# are installed under the host /opt, and its own sandbox stays enabled.
if [[ ${WOMPRAT_HEADLESS:-} != 1 && $APPDIR != /opt/womprat ]]; then
  test -x /usr/bin/bwrap && test -x /usr/bin/xdg-dbus-proxy || { echo 'Install bubblewrap and xdg-dbus-proxy to run the WebKit sandbox.' >&2; exit 1; }
  exec /usr/bin/bwrap --bind / / --tmpfs /opt --ro-bind "$APPDIR" /opt/womprat --chdir /opt/womprat /opt/womprat/AppRun "$@"
fi
export APPDIR
export LD_LIBRARY_PATH="$APPDIR/usr/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export XDG_DATA_DIRS="$APPDIR/usr/share:${XDG_DATA_DIRS:-/usr/local/share:/usr/share}"
export GSETTINGS_SCHEMA_DIR="$APPDIR/usr/share/glib-2.0/schemas"
export GTK_EXE_PREFIX="$APPDIR/usr"
export GTK_DATA_PREFIX="$APPDIR/usr"
export GDK_PIXBUF_MODULEDIR="$APPDIR/usr/lib/gdk-pixbuf-2.0/2.10.0/loaders"
export GDK_PIXBUF_MODULE_FILE="$APPDIR/usr/lib/gdk-pixbuf-2.0/2.10.0/loaders.cache"
export GTK_IM_MODULE_FILE="$APPDIR/usr/lib/gtk-3.0/3.0.0/immodules.cache"
export GST_PLUGIN_SYSTEM_PATH_1_0="$APPDIR/usr/lib/gstreamer-1.0"
export GST_PLUGIN_SCANNER_1_0="$APPDIR/usr/libexec/gstreamer-1.0/gst-plugin-scanner"
export ALSA_CONFIG_PATH="$APPDIR/usr/share/alsa/alsa.conf"
# XWayland is supported on Fedora desktops; do not bundle a graphics driver.
export GDK_BACKEND=x11
# WebKit sandbox helpers are trusted host programs. Never disable the sandbox.
if [[ ${WOMPRAT_HEADLESS:-} != 1 ]]; then
  test -x /usr/bin/bwrap && test -x /usr/bin/xdg-dbus-proxy || { echo 'Install bubblewrap and xdg-dbus-proxy to run the WebKit sandbox.' >&2; exit 1; }
fi
export WEBKIT_INJECTED_BUNDLE_PATH="$APPDIR/usr/lib/webkit2gtk-4.1/injected-bundle"
cd "$APPDIR"
exec "$APPDIR/usr/bin/womprat" "$@"
