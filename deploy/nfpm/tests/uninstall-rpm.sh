#!/bin/sh
# Runs INSIDE a fedora container against a test .rpm built by build-test-package.sh.
#
# Same contract as the deb suite, but rpm passes the scriptlets a COUNT rather than an
# action word: postun sees 0 on the final erase and 1+ during an upgrade. Getting that
# guard wrong wipes data on every upgrade, which is exactly why this suite exists.
#
# Usage (from the staging dir mounted at /w):  sh uninstall-rpm.sh <app>
set -u

app=${1:?usage: uninstall-rpm.sh <app>}
PKG=/w/$app-1.0.0-1.x86_64.rpm
data=/opt/$app
fail=0

say()  { echo; echo "---- $* ----"; }
check() { d=$1; shift; if "$@" >/dev/null 2>&1; then echo "  PASS: $d"; else echo "  FAIL: $d"; fail=1; fi; }
checknot() { d=$1; shift; if "$@" >/dev/null 2>&1; then echo "  FAIL: $d"; fail=1; else echo "  PASS: $d"; fi; }
seed() { mkdir -p "$data/data" "$data/secret"; echo db > "$data/data/app.db"; echo key > "$data/secret/atrest.key"; }

say "install"
rpm -i "$PKG"
check "binary installed" test -f "$data/$app"
check "uninstall helper installed and executable" test -x "/usr/sbin/$app-uninstall"
check "service account created" getent passwd "$app"

say "upgrade (postun count 1) must not touch data"
seed
rpm -U --force "$PKG"
check "data survived" test -f "$data/data/app.db"
check "service account still present" getent passwd "$app"

say "default uninstall keeps data"
"$app-uninstall" -y >/dev/null 2>&1
checknot "package removed" rpm -q "$app"
checknot "binary gone" test -f "$data/$app"
check "data kept" test -f "$data/data/app.db"
check "at-rest key kept" test -f "$data/secret/atrest.key"

say "--purge-data wipes"
rpm -i "$PKG"
check "kept data is picked up by the reinstall" test -f "$data/data/app.db"
"$app-uninstall" --purge-data -y >/dev/null 2>&1
checknot "package removed" rpm -q "$app"
checknot "data dir gone" test -d "$data"
checknot "service account gone" getent passwd "$app"
checknot "service group gone" getent group "$app"
checknot "unit file gone" test -e "/etc/systemd/system/$app.service"

say "KOPIV2_PURGE_DATA=1 rpm -e wipes (no helper involved)"
rpm -i "$PKG"; seed
KOPIV2_PURGE_DATA=1 rpm -e "$app"
checknot "data dir gone" test -d "$data"
checknot "service account gone" getent passwd "$app"

say "plain rpm -e keeps data"
rpm -i "$PKG"; seed
rpm -e "$app"
check "data kept" test -f "$data/data/app.db"

echo
if [ $fail -eq 0 ]; then echo "rpm: ALL CHECKS PASSED ($app)"; else echo "rpm: CHECKS FAILED ($app)"; fi
exit $fail
