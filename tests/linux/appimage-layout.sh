#!/usr/bin/env bash
# Disposable Fedora acceptance container only. Never run on the host: this
# deliberately changes /opt between empty-directory and Atomic symlink layouts.
set -euo pipefail
[[ $(id -u) == 0 && -f /.dockerenv && $# == 2 && $1 != 0 ]] || exit 2
uid=$1 gid=$2
user=$(getent passwd "$uid" | cut -d: -f1)
[[ -n $user && -d /opt && ! -L /opt && -z $(ls -A /opt) ]] || exit 2
# Parent already resolved/owns these paths as the acceptance UID. This root
# container helper must not rerun that UID's ownership resolver as root.
[[ $WOMPRAT_RUN_DIR == "$PROJECT_TMP_ROOT"/runs/* && ! -L $WOMPRAT_RUN_DIR && $(stat -c %u "$WOMPRAT_RUN_DIR") == "$uid" ]] || exit 2
fixture=$(mktemp -d "$WOMPRAT_RUN_DIR/apprun-layout-XXXXXXXX")
cleanup(){
 local status=$?
 if [[ -L /opt ]];then rm /opt;mkdir /opt;fi
 rm -rf -- "$fixture"
 exit "$status"
}
trap cleanup EXIT
mkdir -p "$fixture/app/usr/bin"
cp "$WOMPRAT_RUN_DIR/source/scripts/appimage-apprun.sh" "$fixture/app/AppRun"
cp /usr/bin/xdg-dbus-proxy "$fixture/app/usr/bin/xdg-dbus-proxy"
printf 'layout sentinel\n' > "$fixture/app/sentinel"
cat > "$fixture/app/usr/bin/womprat" <<'STUB'
#!/bin/bash
set -euo pipefail
[[ $(id -u) != 0 && $# == 2 && $1 == 'argument with spaces' && $2 == '--flag' ]]
[[ $APPDIR == "$(readlink -f /opt)/womprat" && -r /opt/womprat/sentinel ]]
[[ $(cat /opt/womprat/sentinel) == 'layout sentinel' ]]
printf 'PASS AppRun reached payload exactly once as normal user\n'
STUB
chmod +x "$fixture/app/AppRun" "$fixture/app/usr/bin/womprat"
chown -R "$uid:$gid" "$fixture"
for layout in directory absolute-symlink relative-symlink;do
 case $layout in
  directory) ;;
  absolute-symlink) rmdir /opt;ln -s /var/opt /opt;;
  relative-symlink) rm /opt;ln -s var/opt /opt;;
 esac
 before=$(readlink -f /opt)
 timeout --kill-after=2 15 runuser -u "$user" -- "$fixture/app/AppRun" 'argument with spaces' --flag
 [[ $(readlink -f /opt) == "$before" && ! -e /opt/womprat ]]
 printf 'PASS AppRun %s layout: no host mount or recursion\n' "$layout"
done
