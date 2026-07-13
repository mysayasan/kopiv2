#!/usr/bin/env bash
# Generates the Windows resource objects (.syso) that embed an app's brand icon +
# version metadata into its .exe. Go links a *_windows_<arch>.syso automatically for
# the matching GOOS/GOARCH, so we emit one per Windows arch.
#
# These files are .gitignored (they carry the release version and are build
# output), so generating them here keeps the tree "clean" for GoReleaser's
# dirty-state check while still stamping the exe.
#
# Usage: packaging/gen-winres.sh [version] [app]
#   version: dotted release version (e.g. 1.74.0); defaults to 0.0.0.
#   app:     mymatasan (default) or myseliasan — selects the icon + versioninfo.json.
set -euo pipefail

VERSION="${1:-0.0.0}"
APP="${2:-mymatasan}"
IFS='.' read -r MAJ MIN PAT _ <<<"${VERSION#v}"
# Keep only the leading digits of each part (snapshot builds look like
# "1.74.1-snapshot-abc", and -ver-* flags require plain integers).
MAJ="${MAJ%%[!0-9]*}"; MIN="${MIN%%[!0-9]*}"; PAT="${PAT%%[!0-9]*}"
MAJ="${MAJ:-0}"; MIN="${MIN:-0}"; PAT="${PAT:-0}"

GV="github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1"
ICON="packaging/windows/${APP}.ico"
CFG="cmd/${APP}/versioninfo.json"

[ -f "$ICON" ] || { echo "gen-winres: no icon at $ICON" >&2; exit 1; }
[ -f "$CFG" ]  || { echo "gen-winres: no versioninfo at $CFG" >&2; exit 1; }

verflags=(-ver-major "$MAJ" -ver-minor "$MIN" -ver-patch "$PAT" \
          -product-ver-major "$MAJ" -product-ver-minor "$MIN" -product-ver-patch "$PAT")

# amd64
go run "$GV" -icon="$ICON" -64 "${verflags[@]}" \
  -o "cmd/${APP}/resource_windows_amd64.syso" "$CFG"
# arm64
go run "$GV" -icon="$ICON" -arm -64 "${verflags[@]}" \
  -o "cmd/${APP}/resource_windows_arm64.syso" "$CFG"

echo "generated windows resource objects for ${APP} v${VERSION} (amd64, arm64)"
