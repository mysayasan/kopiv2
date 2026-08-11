#!/bin/sh
# Runs INSIDE a debian container against a test .deb built by build-test-package.sh.
#
# The contract under test, for every app:
#   - an UPGRADE never touches the data dir (postrm also runs on upgrade)
#   - a plain remove/purge KEEPS /opt/<app>          (an accident must not destroy data)
#   - --purge-data / KOPIV2_PURGE_DATA=1 ERASES it   (a clean wipe must be possible)
#   - keep beats clean when both are asked for
#
# Usage (from the staging dir mounted at /w):  sh uninstall-deb.sh <app>
set -u

app=${1:?usage: uninstall-deb.sh <app>}
PKG=/w/${app}_1.0.0_amd64.deb
data=/opt/$app
fail=0

say()  { echo; echo "---- $* ----"; }
check() { d=$1; shift; if "$@" >/dev/null 2>&1; then echo "  PASS: $d"; else echo "  FAIL: $d"; fail=1; fi; }
checknot() { d=$1; shift; if "$@" >/dev/null 2>&1; then echo "  FAIL: $d"; fail=1; else echo "  PASS: $d"; fi; }
# shellcheck disable=SC2317,SC2329  # invoked indirectly, as the command passed to check/checknot
# (SC2317 on ShellCheck <= 0.9 as shipped on the runners, SC2329 on newer builds)
installed() { dpkg-query -W -f='${Status}' "$app" 2>/dev/null | grep -q 'ok installed'; }
seed() { mkdir -p "$data/data" "$data/secret"; echo db > "$data/data/app.db"; echo key > "$data/secret/atrest.key"; }

say "install"
dpkg -i "$PKG" >/dev/null
check "binary installed" test -f "$data/$app"
check "uninstall helper installed and executable" test -x "/usr/sbin/$app-uninstall"
check "service account created" getent passwd "$app"

say "upgrade must not touch data"
seed
dpkg -i "$PKG" >/dev/null
check "data survived the upgrade" test -f "$data/data/app.db"
check "service account still present" getent passwd "$app"

say "no tty and no --yes: refuse, change nothing"
"$app-uninstall" --purge-data >/dev/null 2>&1; rc=$?
check "exited non-zero" test $rc -ne 0
check "package still installed" installed
check "data untouched" test -f "$data/data/app.db"

say "--dry-run changes nothing"
"$app-uninstall" --purge-data --dry-run -y >/dev/null 2>&1
check "package still installed" installed
check "data untouched" test -f "$data/data/app.db"

say "default uninstall keeps data"
"$app-uninstall" -y >/dev/null 2>&1
checknot "package removed" installed
checknot "binary gone" test -f "$data/$app"
check "data kept" test -f "$data/data/app.db"
check "at-rest key kept" test -f "$data/secret/atrest.key"

say "--purge-data wipes"
dpkg -i "$PKG" >/dev/null
check "kept data is picked up by the reinstall" test -f "$data/data/app.db"
"$app-uninstall" --purge-data -y >/dev/null 2>&1
checknot "package removed" installed
checknot "data dir gone" test -d "$data"
checknot "service account gone" getent passwd "$app"
checknot "service group gone" getent group "$app"
checknot "unit file gone" test -e "/etc/systemd/system/$app.service"

say "KOPIV2_PURGE_DATA=1 apt-get purge wipes (no helper involved)"
dpkg -i "$PKG" >/dev/null; seed
KOPIV2_PURGE_DATA=1 DEBIAN_FRONTEND=noninteractive apt-get purge -y "$app" >/dev/null 2>&1
checknot "data dir gone" test -d "$data"
checknot "service account gone" getent passwd "$app"

say "plain apt-get purge keeps data"
dpkg -i "$PKG" >/dev/null; seed
DEBIAN_FRONTEND=noninteractive apt-get purge -y "$app" >/dev/null 2>&1
check "data kept" test -f "$data/data/app.db"

say "--keep-data overrides --purge-data"
dpkg -i "$PKG" >/dev/null
"$app-uninstall" --purge-data --keep-data -y >/dev/null 2>&1
check "data kept" test -f "$data/data/app.db"

say "helper is a no-op when nothing is installed"
rm -rf "$data"
cp /w/uninstall.sh "/usr/sbin/$app-uninstall"; chmod 755 "/usr/sbin/$app-uninstall"
"$app-uninstall" --purge-data -y >/dev/null 2>&1
check "exited cleanly" test $? -eq 0

echo
if [ $fail -eq 0 ]; then echo "deb: ALL CHECKS PASSED ($app)"; else echo "deb: CHECKS FAILED ($app)"; fi
exit $fail
