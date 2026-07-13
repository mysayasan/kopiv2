#!/usr/bin/env bash
# Assembles the myseliasan archive payload (everything that ships alongside the
# binary) into packaging/staging-myseliasan/ with the exact on-disk layout the app
# expects at its home dir. GoReleaser strips the "packaging/staging-myseliasan/"
# prefix, so the archive root ends up as:
#   myseliasan(.exe), config.json, static/, deploy/
#
# Deliberately a separate script from stage-archive.sh (mymatasan) rather than a
# shared parameterised one: the payloads genuinely differ. myseliasan is pure Go —
# no ffmpeg, no Python, no ai/ worker scripts, no YOLO model. Its whole payload is
# the web bundle and a default config.
#
# Run automatically by GoReleaser's before-hook; safe to run by hand.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

stage="packaging/staging-myseliasan"
rm -rf "$stage"
mkdir -p "$stage/deploy"

# Web UI (served from <home>/static), preserving the assets/ subtree. The committed
# apps/myseliasan/static bundle at the release tag IS the shipped UI — CI does not
# rebuild it (that would dirty the tree).
cp -r apps/myseliasan/static "$stage/static"

# Default config at the home root: sqlite, no secrets, SSO left for the operator.
cp deploy/dist/myseliasan-config.json "$stage/config.json"

# Service supervisor examples + install notes.
cp deploy/systemd/myseliasan.service "$stage/deploy/"
cp deploy/windows/myseliasan.winsw.xml "$stage/deploy/"
cp deploy/launchd/com.mysayasan.myseliasan.plist "$stage/deploy/"
cp deploy/README-myseliasan.md "$stage/deploy/README.md"

echo "staged myseliasan archive payload in $stage"
