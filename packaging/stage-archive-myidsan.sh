#!/usr/bin/env bash
# Assembles the myidsan archive payload (everything that ships alongside the
# binary) into packaging/staging-myidsan/ with the exact on-disk layout the app
# expects at its home dir. GoReleaser strips the "packaging/staging-myidsan/"
# prefix, so the archive root ends up as:
#   myidsan(.exe), config.json, static/, deploy/
#
# Deliberately a separate script from the mymatasan/myseliasan ones rather than a
# shared parameterised one, matching the repo convention: the payloads genuinely
# differ per app. myidsan is pure Go — its whole payload is the web bundle and a
# default config.
#
# Run automatically by GoReleaser's before-hook; safe to run by hand.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

stage="packaging/staging-myidsan"
rm -rf "$stage"
mkdir -p "$stage/deploy"

# Web UI (served from <home>/static), preserving the assets/ subtree. The committed
# apps/myidsan/static bundle at the release tag IS the shipped UI — CI does not
# rebuild it (that would dirty the tree).
cp -r apps/myidsan/static "$stage/static"

# Default config at the home root: sqlite, no secrets, first run generates the
# bootstrap password and jwt secret.
cp deploy/dist/myidsan-config.json "$stage/config.json"

# Service supervisor examples + install notes.
cp deploy/systemd/myidsan.service "$stage/deploy/"
cp deploy/windows/myidsan.winsw.xml "$stage/deploy/"
cp deploy/launchd/com.mysayasan.myidsan.plist "$stage/deploy/"
cp deploy/README-myidsan.md "$stage/deploy/README.md"

echo "staged myidsan archive payload in $stage"
