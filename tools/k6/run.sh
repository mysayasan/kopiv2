#!/usr/bin/env bash
# Run a k6 load test against a running mymatasan, myseliasan or myiotsan instance,
# streaming live metrics into a local Grafana + InfluxDB stack (Grafana on
# http://localhost:3300).
#
# Usage:
#   ./run.sh                             # mymatasan smoke (reads config/target.env)
#   ./run.sh load                        # mymatasan ramping read load
#   ./run.sh --app myseliasan            # myseliasan smoke (config/myseliasan.target.env)
#   ./run.sh --app myseliasan stress     # myseliasan stress
#   ./run.sh --app myiotsan              # myiotsan smoke (config/myiotsan.target.env)
#   ./run.sh --app myidsan               # myidsan smoke (config/myidsan.target.env)
#
# mymatasan uses HTTP Basic replayed per request; myseliasan and myiotsan do a JSON
# login + cookie session (so bcrypt runs once per VU, not once per request). Each
# app has its own config/<app>.target.env and its scripts are scripts/<name>.js
# (mymatasan) / scripts/myseliasan-<name>.js / scripts/myiotsan-<name>.js.
#
# myiotsan caveat: these scripts load the HTTP CONSOLE only. Its real throughput
# risk is MQTT telemetry into SQLite, which k6 does not speak.
#
# Env overrides: APP, BASE_URL, AUTH_USER, AUTH_PASS, TARGET_VUS, MAX_VUS, RAMP, HOLD,
#                MODE (myidsan-stress.js only: read|login -- 'login' pays a full bcrypt per
#                iteration to measure sign-in throughput, the ceiling that matters for an IdP)
set -euo pipefail
cd "$(dirname "$0")"

APP="${APP:-mymatasan}"
if [[ "${1:-}" == "--app" || "${1:-}" == "-a" ]]; then APP="$2"; shift 2; fi
case "$APP" in mymatasan|myseliasan|myiotsan|myidsan) ;; *) echo "unknown app '$APP' (mymatasan|myseliasan|myiotsan|myidsan)"; exit 2;; esac

SCRIPT="${1:-smoke}"
case "$SCRIPT" in smoke|load|stress) ;; *) echo "unknown script '$SCRIPT' (smoke|load|stress)"; exit 2;; esac

# Per-app wiring: env file, script prefix, default port.
if [[ "$APP" == "myseliasan" ]]; then
  ENVFILE="config/myseliasan.target.env"; SCRIPTFILE="myseliasan-${SCRIPT}"; DEFAULT_BASE_URL="https://host.docker.internal:3002"
elif [[ "$APP" == "myidsan" ]]; then
  # Also JSON login + cookie session, but on myidsan's own path (/api/login/default) and
  # on :3001. Its stress script additionally supports MODE=login, which pays a full bcrypt
  # per iteration to measure sign-in throughput rather than read throughput.
  ENVFILE="config/myidsan.target.env"; SCRIPTFILE="myidsan-${SCRIPT}"; DEFAULT_BASE_URL="https://host.docker.internal:3001"
elif [[ "$APP" == "myiotsan" ]]; then
  ENVFILE="config/myiotsan.target.env"; SCRIPTFILE="myiotsan-${SCRIPT}"; DEFAULT_BASE_URL="https://host.docker.internal:3003"
else
  ENVFILE="config/target.env"; SCRIPTFILE="${SCRIPT}"; DEFAULT_BASE_URL="https://host.docker.internal:3000"
fi

# Load config/<app>.target.env unless already exported.
if [[ -f "$ENVFILE" ]]; then
  # shellcheck disable=SC2046
  set -a; . <(grep -vE '^\s*(#|$)' "$ENVFILE"); set +a
fi
export BASE_URL="${BASE_URL:-$DEFAULT_BASE_URL}"
export AUTH_USER="${AUTH_USER:-}"
export AUTH_PASS="${AUTH_PASS:-}"

command -v docker >/dev/null || { echo "Docker not available."; exit 1; }

if [[ -n "$AUTH_USER" ]]; then
  if [[ "$APP" == "mymatasan" ]]; then
    echo "Auth   : HTTP Basic as '$AUTH_USER'"
  else
    echo "Auth   : JSON login as '$AUTH_USER' (cookie session, once per VU)"
  fi
else
  echo "Auth   : none (anonymous)"
fi
echo "App    : $APP"
echo "Target : $BASE_URL"
echo "Script : ${SCRIPTFILE}.js"

echo "Backend: starting InfluxDB + Grafana..."
docker compose up -d influxdb grafana >/dev/null
echo 'Grafana: http://localhost:3300  (dashboard: "k6 Load Testing Results")'

STAMP="$(date +%Y%m%d-%H%M%S)"
SUMMARY="/results/${SCRIPTFILE}-${STAMP}.summary.json"
echo "Report : results/${SCRIPTFILE}-${STAMP}.summary.json"
echo

K6ENV=()
for v in TARGET_VUS MAX_VUS RAMP HOLD MODE; do
  [[ -n "${!v:-}" ]] && K6ENV+=("-e" "$v=${!v}")
done

set +e
docker compose run --rm \
  -e "K6_OUT=influxdb=http://influxdb:8086/k6" \
  "${K6ENV[@]}" \
  k6 run --summary-export="$SUMMARY" "/scripts/${SCRIPTFILE}.js"
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
