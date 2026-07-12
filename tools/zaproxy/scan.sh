#!/usr/bin/env bash
# Run an OWASP ZAP security scan against a running mymatasan/myseliasan instance.
# POSIX/bash counterpart of scan.ps1 (for Linux/Mac/WSL/Git-Bash).
#
#   ./scan.sh [baseline|api|full] [--yes]              # mymatasan (default)
#   ./scan.sh --app myseliasan [baseline|api|full] [--yes]
#
# Config is read from config/<app>.env (copy from the matching .example).
# Env overrides: APP, TARGET, ZAP_AUTH_USER, ZAP_AUTH_PASS, ZAP_IMAGE
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

# Positional args in any order: mode (baseline|api|full), --yes, and
# --app <mymatasan|myseliasan> (or APP env var).
app="${APP:-mymatasan}"
mode=""
yes=""
while [ $# -gt 0 ]; do
  case "$1" in
    --app) app="${2:-}"; shift 2;;
    --yes) yes="1"; shift;;
    baseline|api|full) mode="$1"; shift;;
    *) echo "Unknown arg: $1"; exit 1;;
  esac
done
mode="${mode:-baseline}"

case "$app" in mymatasan|myseliasan) ;; *) echo "App must be mymatasan|myseliasan"; exit 1;; esac
case "$mode" in baseline|api|full) ;; *) echo "Mode must be baseline|api|full"; exit 1;; esac

# Per-app wiring: config file, plan file, default port, report prefix.
if [ "$app" = "myseliasan" ]; then
  envname="myseliasan.target.env"
  plan_file="myseliasan-$mode.yaml"
  default_target="https://host.docker.internal:3002"
  report_prefix="myseliasan-$mode"
else
  envname="target.env"
  plan_file="$mode.yaml"
  default_target="https://host.docker.internal:3000"
  report_prefix="$mode"
fi

# Load config/<app>.env (does not clobber vars already set in the environment)
envfile="$here/config/$envname"
if [ -f "$envfile" ]; then
  # shellcheck disable=SC1090
  set -a; . "$envfile"; set +a
fi

TARGET="${TARGET:-$default_target}"
TARGET="${TARGET%/}"
ZAP_AUTH_USER="${ZAP_AUTH_USER:-}"
ZAP_AUTH_PASS="${ZAP_AUTH_PASS:-}"
ZAP_IMAGE="${ZAP_IMAGE:-ghcr.io/zaproxy/zaproxy:stable}"

# mymatasan uses a Basic-auth header (injected by the plan's replacer). myseliasan
# does its own JSON login inside the plan, so it needs no header — just the creds.
auth_header=""
if [ -n "$ZAP_AUTH_USER" ]; then
  if [ "$app" = "myseliasan" ]; then
    echo "Auth: JSON login as '$ZAP_AUTH_USER' (cookie session)"
  else
    auth_header="Basic $(printf '%s:%s' "$ZAP_AUTH_USER" "$ZAP_AUTH_PASS" | base64 | tr -d '\n')"
    echo "Auth: HTTP Basic as '$ZAP_AUTH_USER'"
  fi
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
echo "App    : $app"
echo "Target : $TARGET"
echo "Plan   : $plan_file"
echo "Reports: reports/$report_prefix-$stamp.{html,json}"
echo

docker run --rm \
  --add-host=host.docker.internal:host-gateway \
  -v "$mount_src:/zap/wrk:rw" \
  -e "TARGET=$TARGET" \
  -e "ZAP_AUTH_HEADER=$auth_header" \
  -e "ZAP_AUTH_USER=$ZAP_AUTH_USER" \
  -e "ZAP_AUTH_PASS=$ZAP_AUTH_PASS" \
  -e "STAMP=$stamp" \
  "$ZAP_IMAGE" \
  zap.sh -cmd -autorun "/zap/wrk/plans/$plan_file"

code=$?
echo
if [ "$code" -eq 0 ]; then
  echo "Scan complete. Open reports/$report_prefix-$stamp.html"
else
  echo "ZAP exited $code (findings raised or a job warned). Check the report + output above."
fi
exit "$code"
