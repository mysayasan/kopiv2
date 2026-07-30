<#
.SYNOPSIS
  Run a k6 load test against a running mymatasan, myseliasan or myiotsan instance,
  streaming live metrics into a local Grafana + InfluxDB stack.

.DESCRIPTION
  Brings up the metrics backend (Grafana on http://localhost:3300, InfluxDB) if
  it isn't already running, then executes a k6 script from ./scripts inside the
  compose network so it can write to InfluxDB while hitting the app via
  host.docker.internal. A JSON summary is saved to ./results.

  Scripts:
    smoke   (default) 1 VU, quick — verifies target + creds + endpoints.
    load              ramping read load on the dashboard surface (thresholds).
    stress            steps VUs way up to find the breaking point.

.EXAMPLE
  ./run.ps1                      # smoke test (reads config/target.env)

.EXAMPLE
  ./run.ps1 -Script load -TargetVus 100 -Hold 2m

.EXAMPLE
  # An identity provider's real ceiling: a login storm, one full bcrypt per iteration.
  ./run.ps1 -App myidsan -Script stress -Mode login

.EXAMPLE
  ./run.ps1 -Script stress -BaseUrl https://host.docker.internal:3000 -User admin -Pass 'secret'

.EXAMPLE
  ./run.ps1 -App myiotsan -Script load

.NOTES
  Leaves Grafana/InfluxDB running so you can inspect results. Stop with:
      docker compose down            (keep data)
      docker compose down -v         (also wipe InfluxDB)

  myiotsan caveat: its scripts load the HTTP CONSOLE only. The appliance's real
  throughput risk is MQTT telemetry into SQLite, which k6 does not speak — a green
  run here says the UI stays responsive, not that the box keeps up with the estate.
#>
[CmdletBinding()]
param(
  [ValidateSet('mymatasan', 'myseliasan', 'myiotsan', 'myidsan')]
  [string]$App = 'mymatasan',
  [ValidateSet('smoke', 'load', 'stress')]
  [string]$Script = 'smoke',
  [string]$BaseUrl,
  [string]$User,
  [string]$Pass,
  [int]$TargetVus,
  [int]$MaxVus,
  [string]$Ramp,
  [string]$Hold,
  # myidsan-stress.js only: 'read' (default) measures the read path, 'login' pays a full
  # bcrypt per iteration to measure sign-in throughput -- the ceiling that actually matters
  # for an identity provider, and one an order of magnitude below the read ceiling.
  [ValidateSet('read', 'login')]
  [string]$Mode,
  [switch]$NoBackend,   # skip bringing up Grafana/InfluxDB (already up)
  [switch]$KeepOpen     # print the Grafana URL and leave everything running
)

$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $here

# --- Per-app wiring ----------------------------------------------------------
# mymatasan authenticates with HTTP Basic replayed on every request (common.js);
# its scripts are scripts/<name>.js and it listens on :3000. myseliasan (:3002)
# and myiotsan (:3003) do a JSON login + cookie session (session.js) — bcrypt once
# per VU, not per request — and their scripts are prefixed <app>-<name>.js. Each
# app has its own target.env.
if ($App -eq 'myseliasan') {
  $envFileName = 'myseliasan.target.env'
  $scriptFile = "myseliasan-$Script"
  $defaultBaseUrl = 'https://host.docker.internal:3002'
}
elseif ($App -eq 'myidsan') {
  # Also JSON login + cookie session, but on myidsan's own path (/api/login/default) and
  # on :3001. Its stress script additionally supports MODE=login, which pays a full
  # bcrypt per iteration to measure sign-in throughput rather than read throughput.
  $envFileName = 'myidsan.target.env'
  $scriptFile = "myidsan-$Script"
  $defaultBaseUrl = 'https://host.docker.internal:3001'
}
elseif ($App -eq 'myiotsan') {
  $envFileName = 'myiotsan.target.env'
  $scriptFile = "myiotsan-$Script"
  $defaultBaseUrl = 'https://host.docker.internal:3003'
}
else {
  $envFileName = 'target.env'
  $scriptFile = "$Script"
  $defaultBaseUrl = 'https://host.docker.internal:3000'
}

# --- Load config/<app>.env (KEY=VALUE) unless overridden by params -----------
$envFile = Join-Path $here (Join-Path 'config' $envFileName)
$cfg = @{}
if (Test-Path $envFile) {
  Get-Content $envFile | ForEach-Object {
    $line = $_.Trim()
    if ($line -and -not $line.StartsWith('#') -and $line.Contains('=')) {
      $k, $v = $line.Split('=', 2)
      $cfg[$k.Trim()] = $v.Trim()
    }
  }
}
if (-not $BaseUrl) { $BaseUrl = $cfg['BASE_URL'] }
if (-not $BaseUrl) { $BaseUrl = $defaultBaseUrl }
if (-not $PSBoundParameters.ContainsKey('User')) { $User = $cfg['AUTH_USER'] }
if (-not $PSBoundParameters.ContainsKey('Pass')) { $Pass = $cfg['AUTH_PASS'] }
$BaseUrl = $BaseUrl.TrimEnd('/')

# --- Preflight ---------------------------------------------------------------
try { docker version --format '{{.Server.Version}}' | Out-Null }
catch { Write-Error 'Docker is not available. Start Docker Desktop and retry.'; exit 1 }

# Env consumed by docker-compose.yml (${BASE_URL} etc.)
$env:BASE_URL = $BaseUrl
$env:AUTH_USER = $User
$env:AUTH_PASS = $Pass

if ($User) {
  if ($App -eq 'mymatasan') { Write-Host "Auth   : HTTP Basic as '$User'" -ForegroundColor Cyan }
  else { Write-Host "Auth   : JSON login as '$User' (cookie session, once per VU)" -ForegroundColor Cyan }
}
else { Write-Host 'Auth   : none (anonymous surface only)' -ForegroundColor Yellow }
Write-Host "App    : $App"
Write-Host "Target : $BaseUrl"
Write-Host "Script : $scriptFile.js"

# --- Bring up the metrics backend --------------------------------------------
# docker streams progress to stderr, which PS 5.1 turns into a terminating error
# under ErrorActionPreference=Stop. Drop to Continue around native docker calls
# and gate on $LASTEXITCODE instead.
if (-not $NoBackend) {
  Write-Host 'Backend: starting InfluxDB + Grafana...' -ForegroundColor DarkGray
  $eap = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
  docker compose up -d influxdb grafana 2>&1 | Out-Null
  $ErrorActionPreference = $eap
  if ($LASTEXITCODE -ne 0) { Write-Error 'Failed to start Grafana/InfluxDB (see docker output).'; exit 1 }
  Write-Host 'Grafana: http://localhost:3300  (dashboard: "k6 Load Testing Results")' -ForegroundColor Green
}

# --- Per-script k6 env -------------------------------------------------------
$k6env = @()
if ($TargetVus) { $k6env += @('-e', "TARGET_VUS=$TargetVus") }
if ($MaxVus) { $k6env += @('-e', "MAX_VUS=$MaxVus") }
if ($Ramp) { $k6env += @('-e', "RAMP=$Ramp") }
if ($Hold) { $k6env += @('-e', "HOLD=$Hold") }
if ($Mode) { $k6env += @('-e', "MODE=$Mode") }

$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$summary = "/results/$scriptFile-$stamp.summary.json"

Write-Host "Report : results/$scriptFile-$stamp.summary.json" -ForegroundColor DarkGray
Write-Host ''

# --- Run k6 ------------------------------------------------------------------
$eap = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
docker compose run --rm `
  -e "K6_OUT=influxdb=http://influxdb:8086/k6" `
  $k6env `
  k6 run --summary-export=$summary /scripts/$scriptFile.js
$code = $LASTEXITCODE
$ErrorActionPreference = $eap
Write-Host ''
if ($code -eq 0) {
  Write-Host "Done. Thresholds passed. Open http://localhost:3300 for the dashboard." -ForegroundColor Green
}
else {
  Write-Host "k6 exited $code (a threshold failed or the run errored). Check the summary + dashboard." -ForegroundColor Yellow
}

if (-not $KeepOpen -and -not $NoBackend) {
  Write-Host "(Backend left running. Stop with: docker compose down)" -ForegroundColor DarkGray
}
exit $code
