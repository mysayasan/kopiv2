#!/usr/bin/env bash
# Assembles the archive payload (everything that ships alongside the binary) into
# packaging/staging/ with the exact on-disk layout the app expects at its home
# dir. GoReleaser strips the "packaging/staging/" prefix, so the archive root ends
# up as: mymatasan(.exe), config.json, static/, ai/, deploy/.
#
# Run automatically by GoReleaser's before-hook; safe to run by hand.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

stage="packaging/staging"
rm -rf "$stage"
mkdir -p "$stage/ai" "$stage/deploy"

# Web UI (served from <home>/static), preserving the assets/ subtree.
cp -r apps/mymatasan/static "$stage/static"

# AI worker scripts (model weights are fetched at runtime, so not bundled).
cp apps/mymatasan/ai/*.py "$stage/ai/"
cp apps/mymatasan/ai/requirements-*.txt "$stage/ai/"
cp apps/mymatasan/ai/setup.* "$stage/ai/"

# Default config at the home root.
cp deploy/dist/config.json "$stage/config.json"

# Service supervisor examples + install notes.
cp deploy/systemd/mymatasan.service "$stage/deploy/"
cp deploy/windows/mymatasan.winsw.xml "$stage/deploy/"
cp deploy/launchd/com.mysayasan.mymatasan.plist "$stage/deploy/"
cp deploy/README.md "$stage/deploy/"

echo "staged archive payload in $stage"
