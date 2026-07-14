#!/usr/bin/env bash
# Assembles the myiotsan archive payload (everything that ships alongside the binary)
# into packaging/staging-myiotsan/ with the exact on-disk layout the app expects at its
# home dir. GoReleaser strips the "packaging/staging-myiotsan/" prefix, so the archive
# root ends up as:
#   myiotsan(.exe), config.json, static/, deploy/
#
# Deliberately a separate script from stage-archive.sh (mymatasan) rather than a shared
# parameterised one: the payloads genuinely differ. myiotsan is pure Go — no ffmpeg, no
# Python, no ai/ worker scripts, no YOLO model. Its whole payload is the web bundle and a
# default config. (The MQTT broker is embedded in the binary, so nothing to stage for it.)
#
# Run automatically by GoReleaser's before-hook; safe to run by hand.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

stage="packaging/staging-myiotsan"
rm -rf "$stage"
mkdir -p "$stage/deploy"

# Web UI (served from <home>/static), preserving the assets/ subtree. The committed
# apps/myiotsan/static bundle at the release tag IS the shipped UI — CI does not rebuild
# it (that would dirty the tree).
cp -r apps/myiotsan/static "$stage/static"

# Default config at the home root: sqlite, no secrets, empty admin password (the app then
# generates a per-install one), MQTT broker on 1883.
cp deploy/dist/myiotsan-config.json "$stage/config.json"

# Service supervisor examples + install notes.
cp deploy/systemd/myiotsan.service "$stage/deploy/"
cp deploy/windows/myiotsan.winsw.xml "$stage/deploy/"
cp deploy/launchd/com.mysayasan.myiotsan.plist "$stage/deploy/"
cp deploy/README-myiotsan.md "$stage/deploy/README.md"

echo "staged myiotsan archive payload in $stage"
