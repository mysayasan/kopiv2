#!/usr/bin/env bash
# Run a k6 load test against a running mymatasan instance, streaming live
# metrics into a local Grafana + InfluxDB stack (Grafana on http://localhost:3300).
#
# Usage:
#   ./run.sh                       # smoke test (reads config/target.env)
#   ./run.sh load                  # ramping read load with thresholds
#   ./run.sh stress                # step VUs up to find the breaking point
#
# Env overrides: BASE_URL, AUTH_USER, AUTH_PASS, TARGET_VUS, MAX_VUS, RAMP, HOLD
set -euo pipefail
cd "$(dirname "$0")"

SCRIPT="${1:-smoke}"
case "$SCRIPT" in smoke|load|stress) ;; *) echo "unknown script '$SCRIPT' (smoke|load|stress)"; exit 2;; esac

# Load config/target.env unless already exported.
if [[ -f config/target.env ]]; then
  # shellcheck disable=SC2046
  set -a; . <(grep -vE '^\s*(#|$)' config/target.env); set +a
fi
export BASE_URL="${BASE_URL:-https://host.docker.internal:3000}"
export AUTH_USER="${AUTH_USER:-}"
export AUTH_PASS="${AUTH_PASS:-}"

command -v docker >/dev/null || { echo "Docker not available."; exit 1; }

[[ -n "$AUTH_USER" ]] && echo "Auth   : HTTP Basic as '$AUTH_USER'" || echo "Auth   : none (anonymous)"
echo "Target : $BASE_URL"
echo "Script : $SCRIPT.js"

echo "Backend: starting InfluxDB + Grafana..."
docker compose up -d influxdb grafana >/dev/null
echo 'Grafana: http://localhost:3300  (dashboard: "k6 Load Testing Results")'

STAMP="$(date +%Y%m%d-%H%M%S)"
SUMMARY="/results/${SCRIPT}-${STAMP}.summary.json"
echo "Report : results/${SCRIPT}-${STAMP}.summary.json"
echo

K6ENV=()
for v in TARGET_VUS MAX_VUS RAMP HOLD; do
  [[ -n "${!v:-}" ]] && K6ENV+=("-e" "$v=${!v}")
done

set +e
docker compose run --rm \
  -e "K6_OUT=influxdb=http://influxdb:8086/k6" \
  "${K6ENV[@]}" \
  k6 run --summary-export="$SUMMARY" "/scripts/${SCRIPT}.js"
code=$?
set -e

echo
if [[ $code -eq 0 ]]; then
  echo "Done. Thresholds passed. Open http://localhost:3300 for the dashboard."
else
  echo "k6 exited $code (a threshold failed or the run errored). Check the summary + dashboard."
fi
echo "(Backend left running. Stop with: docker compose down)"
exit $code
