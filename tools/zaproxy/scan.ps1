<#
.SYNOPSIS
  Run an OWASP ZAP security scan against a running mymatasan instance.

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
#>
[CmdletBinding()]
param(
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

# --- Load config/target.env (KEY=VALUE lines) unless overridden by params ----
$envFile = Join-Path $here 'config\target.env'
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
  Write-Host "No config/target.env found. Copy config/target.env.example to config/target.env, or pass -Target/-User/-Pass." -ForegroundColor Yellow
}

if (-not $Target) { $Target = $cfg['TARGET'] }
if (-not $Target) { $Target = 'https://host.docker.internal:3000' }
if (-not $PSBoundParameters.ContainsKey('User')) { $User = $cfg['ZAP_AUTH_USER'] }
if (-not $PSBoundParameters.ContainsKey('Pass')) { $Pass = $cfg['ZAP_AUTH_PASS'] }

$Target = $Target.TrimEnd('/')

# --- Build the Basic auth header value (empty => anonymous scan) -------------
$authHeader = ''
if ($User) {
  $bytes = [Text.Encoding]::UTF8.GetBytes("${User}:${Pass}")
  $authHeader = 'Basic ' + [Convert]::ToBase64String($bytes)
  Write-Host "Auth: HTTP Basic as '$User'" -ForegroundColor Cyan
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
$plan = "/zap/wrk/plans/$Mode.yaml"
$mount = ($here -replace '\\', '/')

Write-Host "Target : $Target"
Write-Host "Plan   : $Mode.yaml"
Write-Host "Reports: reports/$Mode-$stamp.{html,json}"
Write-Host ""

# --- Run ZAP ------------------------------------------------------------------
# --add-host maps host.docker.internal on engines that don't provide it by default.
docker run --rm `
  --add-host=host.docker.internal:host-gateway `
  -v "${mount}:/zap/wrk:rw" `
  -e "TARGET=$Target" `
  -e "ZAP_AUTH_HEADER=$authHeader" `
  -e "STAMP=$stamp" `
  $Image `
  zap.sh -cmd -autorun $plan

$code = $LASTEXITCODE
Write-Host ""
if ($code -eq 0) {
  Write-Host "Scan complete. Open reports/$Mode-$stamp.html" -ForegroundColor Green
}
else {
  # ZAP exits non-zero when the plan raised warnings/alerts or a job failed.
  Write-Host "ZAP exited $code (findings raised or a job warned). Check the report + console output above." -ForegroundColor Yellow
}
exit $code
