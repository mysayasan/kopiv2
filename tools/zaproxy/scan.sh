#!/usr/bin/env bash
# Run an OWASP ZAP security scan against a running mymatasan instance.
# POSIX/bash counterpart of scan.ps1 (for Linux/Mac/WSL/Git-Bash).
#
#   ./scan.sh [baseline|api|full] [--yes]
#
# Config is read from config/target.env (copy from config/target.env.example).
# Env overrides: TARGET, ZAP_AUTH_USER, ZAP_AUTH_PASS, ZAP_IMAGE
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# On Git Bash / MSYS (Windows), stop the shell from rewriting container-internal
# paths like /zap/wrk into C:\... , and hand Docker Desktop a Windows-style mount
# source. Without this the container gets a mangled -autorun path and can't find
# the plan. No-op on real POSIX shells.
mount_src="$here"
case "$(uname -s 2>/dev/null)" in
  MINGW*|MSYS*|CYGWIN*)
    export MSYS2_ARG_CONV_EXCL='*'
    export MSYS_NO_PATHCONV=1
    mount_src="$(cygpath -w "$here" 2>/dev/null || echo "$here")"
    ;;
esac

mode="${1:-baseline}"
yes=""
[ "${2:-}" = "--yes" ] && yes="1"

case "$mode" in baseline|api|full) ;; *) echo "Mode must be baseline|api|full"; exit 1;; esac

# Load config/target.env (does not clobber vars already set in the environment)
envfile="$here/config/target.env"
if [ -f "$envfile" ]; then
  # shellcheck disable=SC1090
  set -a; . "$envfile"; set +a
fi

TARGET="${TARGET:-https://host.docker.internal:3000}"
TARGET="${TARGET%/}"
ZAP_AUTH_USER="${ZAP_AUTH_USER:-}"
ZAP_AUTH_PASS="${ZAP_AUTH_PASS:-}"
ZAP_IMAGE="${ZAP_IMAGE:-ghcr.io/zaproxy/zaproxy:stable}"

auth_header=""
if [ -n "$ZAP_AUTH_USER" ]; then
  auth_header="Basic $(printf '%s:%s' "$ZAP_AUTH_USER" "$ZAP_AUTH_PASS" | base64 | tr -d '\n')"
  echo "Auth: HTTP Basic as '$ZAP_AUTH_USER'"
else
  echo "Auth: none (anonymous surface only)"
fi

if [ "$mode" != "baseline" ] && [ -z "$yes" ]; then
  echo
  echo "  '$mode' runs ACTIVE ATTACK payloads against $TARGET"
  echo "  It can create/modify/delete data. Use a THROWAWAY instance only."
  read -r -p "  Type 'yes' to continue: " ans
  [ "$ans" = "yes" ] || { echo "Aborted."; exit 1; }
fi

command -v docker >/dev/null 2>&1 || { echo "Docker not found. Start Docker and retry."; exit 1; }

stamp="$(date +%Y%m%d-%H%M%S)"
echo "Target : $TARGET"
echo "Plan   : $mode.yaml"
echo "Reports: reports/$mode-$stamp.{html,json}"
echo

docker run --rm \
  --add-host=host.docker.internal:host-gateway \
  -v "$mount_src:/zap/wrk:rw" \
  -e "TARGET=$TARGET" \
  -e "ZAP_AUTH_HEADER=$auth_header" \
  -e "STAMP=$stamp" \
  "$ZAP_IMAGE" \
  zap.sh -cmd -autorun "/zap/wrk/plans/$mode.yaml"

code=$?
echo
if [ "$code" -eq 0 ]; then
  echo "Scan complete. Open reports/$mode-$stamp.html"
else
  echo "ZAP exited $code (findings raised or a job warned). Check the report + output above."
fi
exit "$code"
