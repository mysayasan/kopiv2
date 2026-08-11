#!/bin/sh
# Drives the uninstall-behaviour suites for one or more apps, in containers.
#
#   deploy/nfpm/tests/run.sh                    # all four packaged apps
#   deploy/nfpm/tests/run.sh myseliasan         # just one
#
# Requires docker and nothing else — no Go build, no local dpkg/rpm. Each app gets a
# throwaway .deb/.rpm built around its real maintainer scripts, then:
#
#   debian:bookworm-slim   dpkg/apt paths (upgrade, keep, wipe, env var, dry run)
#   fedora:41              rpm/dnf paths (scriptlets take a count, not an action word)
#   python:3.12-slim       the interactive confirmation, on a real pty
#
# This exists because the uninstall path is invisible until the day someone needs it, and
# by then getting it wrong means either destroyed recordings or an un-decommissionable box.
set -eu

repo=$(cd "$(dirname "$0")/../../.." && pwd)
tests=$repo/deploy/nfpm/tests
apps=${*:-"mymatasan myseliasan myiotsan myidsan"}
fail=0

for app in $apps; do
    echo
    echo "################################################################"
    echo "#  $app"
    echo "################################################################"

    "$tests/build-test-package.sh" "$app"

    stage=$repo/.pkgtest/$app
    host=$stage
    if command -v cygpath >/dev/null 2>&1; then
        host=$(cygpath -w "$stage")
    fi
    # Test scripts are mounted, not copied into the package, so an edit is picked up
    # without rebuilding.
    cp "$tests/uninstall-deb.sh" "$tests/uninstall-rpm.sh" "$tests/uninstall-tty.sh" \
       "$tests/drive-pty.py" "$stage/"

    for spec in "debian:bookworm-slim uninstall-deb.sh" \
                "fedora:41 uninstall-rpm.sh" \
                "python:3.12-slim uninstall-tty.sh"; do
        image=${spec%% *}
        script=${spec##* }
        if docker run --rm -v "$host://w" -w //w "$image" sh "//w/$script" "$app"; then
            :
        else
            fail=1
        fi
    done
done

echo
if [ "$fail" -eq 0 ]; then
    echo "===== uninstall behaviour: ALL SUITES PASSED ====="
else
    echo "===== uninstall behaviour: FAILURES ABOVE ====="
fi
exit $fail
