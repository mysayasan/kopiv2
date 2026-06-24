# MyMataSan AI dependency installer (Windows).
#
# Installs PyTorch + ultralytics + OpenCV into the Python the app uses, so the
# Train-in-app feature can use the GPU. It:
#   * picks the right CUDA wheel automatically (cu128 for RTX 50-series / Blackwell,
#     cu124 for older NVIDIA cards) -override with -Cuda,
#   * makes sure the target Python is one PyTorch actually ships CUDA wheels for
#     (3.9-3.13). PyTorch publishes NO CUDA wheels for 3.14+, so on a too-new
#     interpreter it finds (or installs, via winget) a supported Python 3.13 and
#     uses that instead -emitting a "MYMATASAN_PYTHON=<path>" marker so the app
#     can repoint vision.detector.command at it.
#
# Usage (from a PowerShell prompt, in this folder or anywhere):
#   powershell -ExecutionPolicy Bypass -File setup.ps1
#   powershell -ExecutionPolicy Bypass -File setup.ps1 -Python "C:\Path\to\python.exe"
#   powershell -ExecutionPolicy Bypass -File setup.ps1 -Cuda cu124   # override CUDA wheel
#
# IMPORTANT: run this with the SAME Python the app launches the detector with
# (the `vision.detector.command` in config.json, usually "python") -pass it via
# -Python if it is not the default `python` on PATH.

param(
  [string]$Python = "python",
  [string]$Cuda = "",  # empty => auto-detect from the GPU
  [switch]$Lpr         # also install the optional license-plate (OCR) dependencies
)

$ErrorActionPreference = "Stop"

function Write-Section($msg) { Write-Host "== $msg ==" -ForegroundColor Cyan }

# PythonCudaIssue returns "" if the interpreter is one PyTorch ships CUDA wheels
# for (CPython 3.9-3.13), otherwise a short human reason. This is the gate that
# catches the common "I'm on Python 3.14 so the GPU build can never install" trap.
function PythonCudaIssue($exe) {
  try {
    $v = (& $exe -c "import sys; print('%d.%d' % sys.version_info[:2])" 2>$null)
  } catch {
    return "could not run '$exe'"
  }
  $v = "$v".Trim()
  if (-not $v) { return "could not read version from '$exe'" }
  $parts = $v.Split('.')
  $maj = [int]$parts[0]; $min = [int]$parts[1]
  if ($maj -ne 3) { return "Python $v (need CPython 3.x)" }
  if ($min -lt 9 -or $min -gt 13) { return "Python $v (PyTorch CUDA wheels exist only for 3.9-3.13)" }
  return ""
}

# PythonExe resolves an interpreter (e.g. "python", "py -3.13") to its real
# executable path, or "" if it cannot be run.
function PythonExe($exe) {
  try { return (& $exe -c "import sys; print(sys.executable)" 2>$null).Trim() } catch { return "" }
}

# FindSupportedPython looks for an already-installed CUDA-capable Python (3.13
# preferred, down to 3.10) via the py launcher; returns its exe path or "".
function FindSupportedPython {
  foreach ($ver in @("3.13", "3.12", "3.11", "3.10")) {
    $exe = $null
    try { $exe = (& py "-$ver" -c "import sys; print(sys.executable)" 2>$null) } catch { $exe = $null }
    $exe = "$exe".Trim()
    if ($exe -and (Test-Path $exe) -and (PythonCudaIssue $exe) -eq "") { return $exe }
  }
  return ""
}

# InstallPython313 installs CPython 3.13 for the current user via winget (no admin
# needed), then returns its exe path or "" on failure / when winget is absent.
function InstallPython313 {
  if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
    Write-Host "winget is not available -cannot auto-install Python 3.13." -ForegroundColor Yellow
    return ""
  }
  Write-Host "Installing CPython 3.13 via winget (user scope, no admin)..." -ForegroundColor Cyan
  # Capture winget's output instead of letting it flow to the pipeline: a PowerShell
  # function returns ALL uncaptured output, so an un-captured 'winget' would make this
  # function return its log lines alongside the path. Echo it to the log explicitly.
  $wingetOut = & winget install --id Python.Python.3.13 --scope user --silent --accept-package-agreements --accept-source-agreements 2>&1
  foreach ($line in $wingetOut) { Write-Host $line }
  # winget reports a non-zero code when the package is already installed; don't treat
  # that as fatal -just try to locate the interpreter afterwards.
  $found = FindSupportedPython
  if ($found) { return $found }
  $guess = Join-Path $env:LOCALAPPDATA "Programs\Python\Python313\python.exe"
  if (Test-Path $guess) { return $guess }
  return ""
}

Write-Section "MyMataSan AI setup"

# --- 1. Detect the GPU and choose the matching CUDA wheel tag -----------------
$gpuName = ""
if (Get-Command nvidia-smi -ErrorAction SilentlyContinue) {
  try { $gpuName = (& nvidia-smi --query-gpu=name --format=csv,noheader 2>$null | Select-Object -First 1).Trim() } catch { $gpuName = "" }
}
$hasGpu = [bool]$gpuName

if (-not $Cuda) {
  if ($gpuName -match "RTX\s*50\d\d") {
    $Cuda = "cu128"   # Blackwell (RTX 50-series) needs CUDA 12.8+
  } else {
    $Cuda = "cu124"
  }
}

if ($hasGpu) {
  Write-Host "GPU: $gpuName  ->  CUDA wheel: $Cuda" -ForegroundColor Green
} else {
  Write-Host "No NVIDIA GPU detected -will install the CPU build (training will be slow)." -ForegroundColor Yellow
}

# --- 2. Resolve a Python PyTorch can actually use a GPU with -------------------
$targetPython = $Python
$switchedPython = $false

if ($hasGpu) {
  $issue = PythonCudaIssue $Python
  if ($issue) {
    Write-Host "Configured Python is unsuitable for GPU: $issue" -ForegroundColor Yellow
    Write-Host "Looking for a supported Python (3.13 preferred)..."
    $alt = FindSupportedPython
    if (-not $alt) {
      Write-Host "None found -attempting to install Python 3.13 automatically..."
      $alt = InstallPython313
    }
    # Coerce to a single trimmed path: a helper that accidentally emits extra output
    # would return an array, and only the last line is the interpreter path.
    if ($alt) { $alt = "$(@($alt) | Select-Object -Last 1)".Trim() }
    if (-not $alt) {
      Write-Host ""
      Write-Host "Could not find or install a CUDA-capable Python." -ForegroundColor Red
      Write-Host "Install Python 3.13 from https://www.python.org/downloads/ (or 'winget install Python.Python.3.13')," -ForegroundColor Red
      Write-Host "then re-run this installer. PyTorch ships no CUDA wheels for Python 3.14+." -ForegroundColor Red
      exit 1
    }
    $targetPython = $alt
    $switchedPython = $true
    Write-Host "Using Python: $targetPython" -ForegroundColor Green
  }
}

$targetExe = PythonExe $targetPython
if (-not $targetExe) { Write-Host "Python '$targetPython' could not be run." -ForegroundColor Red; exit 1 }
Write-Host "Python: $targetExe"
& $targetPython --version

# --- 3. Install PyTorch (+ ultralytics / OpenCV) ------------------------------
Write-Host "Upgrading pip..."
& $targetPython -m pip install --upgrade pip

if ($hasGpu) {
  Write-Host "Installing CUDA PyTorch ($Cuda)..." -ForegroundColor Green
  # pip treats "2.x+cpu" and "2.x+cu128" as the SAME base version, so a plain
  # --upgrade would see it "already satisfied" and keep a CPU build. Removing it
  # first forces a clean install of the CUDA wheels.
  & $targetPython -m pip uninstall -y torch torchvision torchaudio
  & $targetPython -m pip install --force-reinstall torch torchvision --index-url "https://download.pytorch.org/whl/$Cuda"
} else {
  Write-Host "Installing CPU PyTorch..." -ForegroundColor Yellow
  & $targetPython -m pip install --upgrade torch torchvision
}

Write-Host "Installing ultralytics + OpenCV..."
& $targetPython -m pip install --upgrade ultralytics opencv-python

# Optional: license-plate (LPR) OCR backend. Only when -Lpr is passed, since easyocr
# is a heavy extra most installs don't need.
if ($Lpr) {
  Write-Host "Installing license-plate OCR dependencies (easyocr)..." -ForegroundColor Green
  $lprReq = Join-Path $PSScriptRoot "requirements-lpr.txt"
  if (Test-Path $lprReq) {
    & $targetPython -m pip install --upgrade -r $lprReq
  } else {
    & $targetPython -m pip install --upgrade easyocr opencv-python numpy
  }
}

# --- 4. Verify + report -------------------------------------------------------
Write-Section "Verifying"
& $targetPython -c "import torch; print('torch', torch.__version__, '| cuda build:', torch.version.cuda, '| cuda available:', torch.cuda.is_available(), '| device:', (torch.cuda.get_device_name(0) if torch.cuda.is_available() else 'cpu'))"
if ($hasGpu) {
  $cudaOk = & $targetPython -c "import torch,sys; sys.stdout.write('1' if torch.cuda.is_available() else '0')"
  if ($cudaOk -ne "1") {
    Write-Host "WARNING: PyTorch still cannot see the GPU. Check that your NVIDIA driver is up to date, or try a different -Cuda tag (e.g. cu124)." -ForegroundColor Red
  }
}

# Tell the app which interpreter to use when we switched to a different one. The
# Go installer parses this marker and repoints vision.detector.command at it.
if ($switchedPython -and $targetExe) {
  Write-Host "MYMATASAN_PYTHON=$targetExe"
  Write-Host "NOTE: switched to Python '$targetExe' for GPU support -the app will use it after a restart." -ForegroundColor Green
}

Write-Host "Done. Restart the MyMataSan server, then check Models -> Train in-app." -ForegroundColor Green
