#!/usr/bin/env bash
# Internal builder, executed in the project-owned packaging container.
set -euo pipefail
stage=$1 output=$2
cd "$stage/source"
source scripts/paths.sh
appdir="$stage/AppDir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/applications" "$appdir/usr/share/icons/hicolor/256x256/apps"
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -tags webkitgui -ldflags="-s -w -X main.version=$VERSION -X main.commit=$COMMIT" -o "$appdir/usr/bin/womprat" ./cmd/womprat
go version -m "$appdir/usr/bin/womprat" > "$stage/build-settings.txt"
cp "$appdir/usr/bin/womprat" "$stage/womprat-unmodified"
cp docs/icon-256.png "$appdir/usr/share/icons/hicolor/256x256/apps/womprat.png"
cat > "$appdir/usr/share/applications/womprat.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=Womprat
Comment=Tailscale browser, SSH, VNC and RDP client
Exec=womprat
Icon=womprat
Terminal=false
Categories=Network;RemoteAccess;
DESKTOP
export APPIMAGE_EXTRACT_AND_RUN=1
export LINUXDEPLOY="$stage/tools/linuxdeploy.AppImage"
export PATH="$stage/tools:$PATH"
"$LINUXDEPLOY" --appdir "$appdir" --executable "$appdir/usr/bin/womprat" \
  --desktop-file "$appdir/usr/share/applications/womprat.desktop" \
  --icon-file "$appdir/usr/share/icons/hicolor/256x256/apps/womprat.png" --plugin gtk \
  --deploy-deps-only /usr/lib/x86_64-linux-gnu/webkit2gtk-4.1 \
  --deploy-deps-only /usr/lib/x86_64-linux-gnu/gstreamer-1.0 \
  --deploy-deps-only /usr/lib/x86_64-linux-gnu/gstreamer1.0/gstreamer-1.0 > "$stage/deploy.log" 2>&1
# The launcher sets LD_LIBRARY_PATH; no rpath rewrite of the Go executable is
# needed. Preserve its build-info section, which patchelf otherwise invalidates.
cp "$stage/womprat-unmodified" "$appdir/usr/bin/womprat"
cp -a /usr/lib/x86_64-linux-gnu/webkit2gtk-4.1 "$appdir/usr/lib/"
cp -a /usr/lib/x86_64-linux-gnu/gstreamer-1.0 "$appdir/usr/lib/"
mkdir -p "$appdir/usr/libexec/gstreamer-1.0"
cp /usr/lib/x86_64-linux-gnu/gstreamer1.0/gstreamer-1.0/gst-plugin-scanner "$appdir/usr/libexec/gstreamer-1.0/"
# linuxdeploy intentionally excludes several system libraries. Bundle portable
# dependencies of every helper/plugin too; leave glibc and graphics drivers to
# the host. Fedora 44 checks expose any remaining missing dependency.
while IFS= read -r -d '' elf; do
  while read -r library path; do
    [[ $path == /* && -f $path ]] || continue
    case "$library" in
      libc.so.*|libm.so.*|libpthread.so.*|libdl.so.*|librt.so.*|libresolv.so.*|libnss_*.so.*|libGL*.so.*|libEGL.so.*|libgbm.so.*|libdrm*.so.*|libvulkan.so.*|libwayland-*.so.*) continue;;
    esac
    [[ -e $appdir/usr/lib/$library ]] || cp -L "$path" "$appdir/usr/lib/$library"
  done < <(ldd "$elf" 2>/dev/null | awk '/=> \// {print $1, $3}')
done < <(find "$appdir/usr" -type f \( -name '*.so*' -o -name WebKitWebProcess -o -name WebKitNetworkProcess -o -name WebKitGPUProcess -o -name gst-plugin-scanner -o -name womprat \) -print0)
if [[ -d /usr/share/alsa ]]; then cp -a /usr/share/alsa "$appdir/usr/share/"; fi
mkdir -p "$appdir/usr/share/doc/womprat"
cp README.md "$appdir/usr/share/doc/womprat/README.md"
# Retain dependency licence notices. The repository has no project LICENSE;
# do not invent one or silently omit third-party notices.
while IFS= read -r module; do
  [[ -d $module ]] || continue
  for notice in "$module"/{LICENSE,LICENSE.txt,LICENSE.md,COPYING,NOTICE}; do
    [[ -f $notice ]] || continue
    name=$(basename "$module" | tr '/@' '__')
    mkdir -p "$appdir/usr/share/doc/go/$name"
    cp "$notice" "$appdir/usr/share/doc/go/$name/$(basename "$notice")"
  done
done < <(go list -m -f '{{.Dir}}' all)
# WebKit release builds ignore developer-only WEBKIT_EXEC_PATH. Relocate the
# fixed process-directory string without changing ELF offsets or code. The
# replacement resolves within AppDir from the launcher's working directory.
bun scripts/relocate-webkit.mjs "$appdir/usr/lib/libwebkit2gtk-4.1.so.0"
rm -f "$appdir/AppRun"
cp scripts/appimage-apprun.sh "$appdir/AppRun"
chmod +x "$appdir/AppRun"
mksquashfs "$appdir" "$stage/appimage.squashfs" -noappend -comp zstd -all-root -no-progress -processors 2 > "$stage/squashfs.log"
cat "$stage/tools/runtime-x86_64" "$stage/appimage.squashfs" > "$output"
chmod 755 "$output"
printf 'Packaged native GUI: %s\n' "$output"
