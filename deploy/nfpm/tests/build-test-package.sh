#!/bin/sh
# Builds a throwaway .deb and .rpm around the REAL maintainer scripts for one app, so the
# uninstall suites can drive them through real dpkg/apt and rpm/dnf.
#
# No Go build is involved: the "binary" is a placeholder, because what is under test is the
# maintainer scripts' behaviour (keep vs wipe, upgrade vs final removal), not the app.
#
# Usage:  deploy/nfpm/tests/build-test-package.sh <app>
# Output: .pkgtest/<app>/{<app>_1.0.0_amd64.deb,<app>-1.0.0-1.x86_64.rpm}
#
# Requires docker (uses the goreleaser/nfpm image, so nothing needs installing).
set -eu

app=${1:?usage: build-test-package.sh <app>}
repo=$(cd "$(dirname "$0")/../../.." && pwd)
stage=$repo/.pkgtest/$app

case "$app" in
    mymatasan)
        # mymatasan's scripts predate the per-app naming and are unprefixed.
        postinstall=postinstall.sh; preremove=preremove.sh; postremove=postremove.sh ;;
    myseliasan|myiotsan|myidsan)
        postinstall=$app-postinstall.sh; preremove=$app-preremove.sh; postremove=$app-postremove.sh ;;
    *)
        echo "build-test-package.sh: unknown app '$app'" >&2; exit 2 ;;
esac

rm -rf "$stage"
mkdir -p "$stage/root"

# Strip CR on the way in. A Windows checkout has a CRLF working tree (the index is LF, so
# releases are unaffected), and a CRLF shebang fails at exec as "/bin/sh\r: not found" —
# which would look like a script bug rather than a checkout artefact.
for f in "$postinstall" "$preremove" "$postremove" uninstall.sh "$app.service"; do
    tr -d '\r' < "$repo/deploy/nfpm/$f" > "$stage/$(basename "$f")"
done

printf '#!/bin/sh\necho placeholder %s\n' "$app" > "$stage/root/$app"

cat > "$stage/nfpm.yaml" <<EOF
name: $app
arch: amd64
platform: linux
version: 1.0.0
maintainer: mysayasan <mysayasan@gmail.com>
description: $app uninstall-behaviour test package (not for distribution)
license: PolyForm-Noncommercial-1.0.0
contents:
  - src: ./root/$app
    dst: /opt/$app/$app
    file_info: {mode: 0755}
  - src: ./$app.service
    dst: /etc/systemd/system/$app.service
  - src: ./uninstall.sh
    dst: /usr/sbin/$app-uninstall
    file_info: {mode: 0755}
scripts:
  postinstall: ./$postinstall
  preremove: ./$preremove
  postremove: ./$postremove
EOF

# On Git Bash, docker needs a native host path for -v, and the container-side paths must
# survive MSYS mangling ("//w" is left alone and means "/w" to Linux).
host=$stage
if command -v cygpath >/dev/null 2>&1; then
    host=$(cygpath -w "$stage")
fi

docker run --rm -v "$host://w" -w //w goreleaser/nfpm package -p deb -t //w
docker run --rm -v "$host://w" -w //w goreleaser/nfpm package -p rpm -t //w

echo "built test packages for $app in $stage"
