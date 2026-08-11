#!/bin/sh
# Runs INSIDE a container that has BOTH dpkg and python3 (python:3.12-slim), so the
# interactive confirmation branches actually execute on a terminal.
#
# What matters here: --purge-data demands the literal word ERASE, and anything else must
# leave the machine untouched. A yes/no typo must not cost someone their recordings.
#
# Usage (from the staging dir mounted at /w):  sh uninstall-tty.sh <app>
set -u

app=${1:?usage: uninstall-tty.sh <app>}
PKG=/w/${app}_1.0.0_amd64.deb
data=/opt/$app
fail=0

say()  { echo; echo "---- $* ----"; }
check() { d=$1; shift; if "$@" >/dev/null 2>&1; then echo "  PASS: $d"; else echo "  FAIL: $d"; fail=1; fi; }
checknot() { d=$1; shift; if "$@" >/dev/null 2>&1; then echo "  FAIL: $d"; fail=1; else echo "  PASS: $d"; fi; }
# shellcheck disable=SC2317,SC2329  # invoked indirectly, as the command passed to check/checknot
# (SC2317 on ShellCheck <= 0.9 as shipped on the runners, SC2329 on newer builds)
installed() { dpkg-query -W -f='${Status}' "$app" 2>/dev/null | grep -q 'ok installed'; }
seed() { mkdir -p "$data/data"; echo db > "$data/data/app.db"; }
drive() { timeout 180 python3 /w/drive-pty.py "$app" "$@" >/dev/null 2>&1; }

command -v python3 >/dev/null 2>&1 || { echo "uninstall-tty.sh: no python3, cannot make a pty" >&2; exit 2; }

say "purge prompt: a wrong answer aborts"
dpkg -i "$PKG" >/dev/null; seed
drive yes --purge-data
check "package still installed" installed
check "data untouched" test -f "$data/data/app.db"

say "purge prompt: typing ERASE wipes"
drive ERASE --purge-data
checknot "package removed" installed
checknot "data dir gone" test -d "$data"

say "keep prompt: 'n' aborts"
dpkg -i "$PKG" >/dev/null; seed
drive n
check "package still installed" installed
check "data untouched" test -f "$data/data/app.db"

say "keep prompt: 'y' removes the package and keeps data"
drive y
checknot "package removed" installed
check "data kept" test -f "$data/data/app.db"

echo
if [ $fail -eq 0 ]; then echo "tty: ALL CHECKS PASSED ($app)"; else echo "tty: CHECKS FAILED ($app)"; fi
exit $fail
