<#
.SYNOPSIS
  Run an OWASP ZAP security scan against a running mymatasan, myseliasan or
  myiotsan instance.

.DESCRIPTION
  Wraps the official ZAP Docker image (ghcr.io/zaproxy/zaproxy:stable) and its
  Automation Framework. Picks a plan from ./plans, injects the target + HTTP
  Basic auth header, mounts this folder into the container, and drops HTML+JSON
  reports into ./reports.

  Modes:
    baseline  (default) Passive scan only. Safe on a live instance.
    api                 Active attack scan scoped to /api. Destructive routes
                        excluded. Use a THROWAWAY instance.
    full                Active attack scan of the whole app. Use a THROWAWAY
                        instance.

.EXAMPLE
  # Safe passive scan of the local instance (reads config/target.env)
  ./scan.ps1

.EXAMPLE
  ./scan.ps1 -Mode api -Target https://host.docker.internal:3000 -User admin -Pass 'secret'

.EXAMPLE
  ./scan.ps1 -App myiotsan -Mode baseline
#>
[CmdletBinding()]
param(
  [ValidateSet('mymatasan', 'myseliasan', 'myiotsan', 'myidsan')]
  [string]$App = 'mymatasan',
  [ValidateSet('baseline', 'api', 'full')]
  [string]$Mode = 'baseline',
  [string]$Target,
  [string]$User,
  [string]$Pass,
  [string]$Image = 'ghcr.io/zaproxy/zaproxy:stable',
  [switch]$Yes   # skip the confirmation prompt for active (api/full) modes
)

$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path

# --- Per-app wiring ----------------------------------------------------------
# mymatasan authenticates with HTTP Basic (a replacer injects the header).
# myseliasan authenticates with a JSON login + cookie session (the plan's own
# `authentication` block consumes ZAP_AUTH_USER/ZAP_AUTH_PASS), and its active
# plans mirror the CSRF cookie into a header via scripts/csrf-doublesubmit.js.
# myiotsan is also JSON login + cookie session, but on the APPLIANCE auth stack:
# a different login path (/api/auth/login) and no CSRF token at all — its cookie
# is SameSite=Lax, which is the defence — so its plans load no CSRF script.
if ($App -eq 'myseliasan') {
  $envFileName = 'myseliasan.target.env'
  $planFile = "myseliasan-$Mode.yaml"
  $defaultTarget = 'https://host.docker.internal:3002'
  $reportPrefix = "myseliasan-$Mode"
}
elseif ($App -eq 'myidsan') {
  # myidsan is JSON login + cookie session like myseliasan, but on its own login path
  # (/api/login/default) and WITH the CSRF double-submit token, so its active plans load
  # the same csrf-doublesubmit.js script.
  $envFileName = 'myidsan.target.env'
  $planFile = "myidsan-$Mode.yaml"
  $defaultTarget = 'https://host.docker.internal:3001'
  $reportPrefix = "myidsan-$Mode"
}
elseif ($App -eq 'myiotsan') {
  $envFileName = 'myiotsan.target.env'
  $planFile = "myiotsan-$Mode.yaml"
  $defaultTarget = 'https://host.docker.internal:3003'
  $reportPrefix = "myiotsan-$Mode"
}
else {
  $envFileName = 'target.env'
  $planFile = "$Mode.yaml"
  $defaultTarget = 'https://host.docker.internal:3000'
  $reportPrefix = "$Mode"
}

# --- Load config/<app>.env (KEY=VALUE lines) unless overridden by params -----
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
elseif (-not $Target) {
  Write-Host "No config/$envFileName found. Copy config/$envFileName.example to config/$envFileName, or pass -Target/-User/-Pass." -ForegroundColor Yellow
}

if (-not $Target) { $Target = $cfg['TARGET'] }
if (-not $Target) { $Target = $defaultTarget }
if (-not $PSBoundParameters.ContainsKey('User')) { $User = $cfg['ZAP_AUTH_USER'] }
if (-not $PSBoundParameters.ContainsKey('Pass')) { $Pass = $cfg['ZAP_AUTH_PASS'] }

$Target = $Target.TrimEnd('/')

# --- Build the Basic auth header value (mymatasan only; empty => anonymous) ---
# myseliasan and myiotsan do their own JSON login inside the plan, so they need no
# header here.
$authHeader = ''
if ($User) {
  if ($App -eq 'mymatasan') {
    $bytes = [Text.Encoding]::UTF8.GetBytes("${User}:${Pass}")
    $authHeader = 'Basic ' + [Convert]::ToBase64String($bytes)
    Write-Host "Auth: HTTP Basic as '$User'" -ForegroundColor Cyan
  }
  else {
    Write-Host "Auth: JSON login as '$User' (cookie session)" -ForegroundColor Cyan
  }
}
else {
  Write-Host "Auth: none (anonymous surface only)" -ForegroundColor Yellow
}

# --- Safety gate for active (attack) modes -----------------------------------
if ($Mode -ne 'baseline' -and -not $Yes) {
  Write-Host ""
  Write-Host "  '$Mode' runs ACTIVE ATTACK payloads against $Target" -ForegroundColor Red
  Write-Host "  It can create/modify/delete data and fire notifications." -ForegroundColor Red
  Write-Host "  Only run against a THROWAWAY instance, never production." -ForegroundColor Red
  $ans = Read-Host "  Type 'yes' to continue"
  if ($ans -ne 'yes') { Write-Host "Aborted."; exit 1 }
}

# --- Preflight: docker + reachability ----------------------------------------
try { docker version --format '{{.Server.Version}}' | Out-Null }
catch { Write-Error "Docker is not available. Start Docker Desktop and retry."; exit 1 }

$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$plan = "/zap/wrk/plans/$planFile"
$mount = ($here -replace '\\', '/')

Write-Host "App    : $App"
Write-Host "Target : $Target"
Write-Host "Plan   : $planFile"
Write-Host "Reports: reports/$reportPrefix-$stamp.{html,json}"
Write-Host ""

# --- Run ZAP ------------------------------------------------------------------
# --add-host maps host.docker.internal on engines that don't provide it by default.
# ZAP_AUTH_HEADER feeds mymatasan's Basic replacer; ZAP_AUTH_USER/PASS feed the
# JSON authentication block used by myseliasan and myiotsan. All are passed
# regardless of app; each plan consumes only what it references.
docker run --rm `
  --add-host=host.docker.internal:host-gateway `
  -v "${mount}:/zap/wrk:rw" `
  -e "TARGET=$Target" `
  -e "ZAP_AUTH_HEADER=$authHeader" `
  -e "ZAP_AUTH_USER=$User" `
  -e "ZAP_AUTH_PASS=$Pass" `
  -e "STAMP=$stamp" `
  $Image `
  zap.sh -cmd -autorun $plan

$code = $LASTEXITCODE
Write-Host ""
if ($code -eq 0) {
  Write-Host "Scan complete. Open reports/$reportPrefix-$stamp.html" -ForegroundColor Green
}
else {
  # ZAP exits non-zero when the plan raised warnings/alerts or a job failed.
  Write-Host "ZAP exited $code (findings raised or a job warned). Check the report + console output above." -ForegroundColor Yellow
}
exit $code
