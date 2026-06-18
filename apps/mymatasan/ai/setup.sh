#!/usr/bin/env bash
# MyMataSan AI dependency installer (Linux / macOS / Raspberry Pi).
#
# Installs PyTorch + ultralytics + OpenCV into the Python the app uses, so the
# Train-in-app feature can use the GPU. It:
#   * picks the right CUDA wheel automatically (cu128 for RTX 50-series / Blackwell,
#     cu124 for older NVIDIA cards) — override with the 2nd arg,
#   * makes sure the target Python is one PyTorch actually ships CUDA wheels for
#     (CPython 3.9-3.13). PyTorch publishes NO CUDA wheels for 3.14+, so on a
#     too-new interpreter it finds (or installs, via pyenv) a supported Python 3.13
#     and uses that instead — emitting a "MYMATASAN_PYTHON=<path>" marker so the
#     app can repoint vision.detector.command at it.
#   * GPU-less hosts (Raspberry Pi / Jetson-without-CUDA-wheels) get the CPU build.
#
# Usage (from anywhere):
#   ./setup.sh                       # auto-detect, default python3
#   ./setup.sh /usr/bin/python3      # use a specific Python
#   ./setup.sh python3 cu124         # override the CUDA wheel tag
#
# IMPORTANT: run with the SAME Python the app launches the detector with
# (the `vision.detector.command` in config.json, usually "python").

set -e

PYTHON="${1:-python3}"
CUDA="${2:-}"

echo "== MyMataSan AI setup =="

# py_minor prints the CPython minor version (e.g. 13) for an interpreter, or "".
py_minor() { "$1" -c 'import sys; print(sys.version_info[1] if sys.version_info[0]==3 else -1)' 2>/dev/null; }

# py_supported is true when an interpreter is one PyTorch ships CUDA wheels for.
py_supported() { local m; m="$(py_minor "$1")"; [ -n "$m" ] && [ "$m" -ge 9 ] && [ "$m" -le 13 ]; }

# find_supported_python echoes the path of an already-installed CUDA-capable
# Python (3.13 preferred, down to 3.10), or nothing.
find_supported_python() {
  for cand in python3.13 python3.12 python3.11 python3.10; do
    if command -v "$cand" >/dev/null 2>&1 && py_supported "$cand"; then
      command -v "$cand"
      return 0
    fi
  done
  return 1
}

# ensure_pyenv_python uses pyenv (the cross-distro, no-root way to install an
# arbitrary CPython) to provide a 3.13, echoing its interpreter path. It will
# bootstrap pyenv itself via the official installer when git+curl are available.
# Building CPython from source needs the usual build deps (make/gcc/openssl/…);
# if that fails the caller falls back to printing instructions.
ensure_pyenv_python() {
  if ! command -v pyenv >/dev/null 2>&1 && [ -x "$HOME/.pyenv/bin/pyenv" ]; then
    export PATH="$HOME/.pyenv/bin:$PATH"
  fi
  if ! command -v pyenv >/dev/null 2>&1; then
    if command -v git >/dev/null 2>&1 && command -v curl >/dev/null 2>&1; then
      echo "Installing pyenv (no root needed)..." >&2
      curl -fsSL https://pyenv.run | bash >&2 || return 1
      export PATH="$HOME/.pyenv/bin:$PATH"
    else
      return 1
    fi
  fi
  command -v pyenv >/dev/null 2>&1 || return 1

  local ver root
  ver="$(pyenv versions --bare 2>/dev/null | grep -E '^3\.13\.' | tail -n1)"
  if [ -z "$ver" ]; then
    echo "Building CPython 3.13 via pyenv (this can take several minutes)..." >&2
    pyenv install -s 3.13 >&2 || return 1
    ver="$(pyenv versions --bare 2>/dev/null | grep -E '^3\.13\.' | tail -n1)"
  fi
  [ -n "$ver" ] || return 1
  root="$(pyenv root 2>/dev/null || echo "$HOME/.pyenv")"
  echo "$root/versions/$ver/bin/python"
}

"$PYTHON" --version

# --- Detect the GPU and (unless overridden) choose the matching CUDA wheel tag --
HAS_GPU=0
GPU_NAME=""
if command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi >/dev/null 2>&1; then
  HAS_GPU=1
  GPU_NAME="$(nvidia-smi --query-gpu=name --format=csv,noheader 2>/dev/null | head -n1)"
fi
if [ -z "$CUDA" ]; then
  if echo "$GPU_NAME" | grep -Eq 'RTX *50[0-9][0-9]'; then
    CUDA="cu128"   # Blackwell (RTX 50-series) needs CUDA 12.8+
  else
    CUDA="cu124"
  fi
fi

# --- Resolve a Python PyTorch can actually use a GPU with ----------------------
SWITCHED=0
if [ "$HAS_GPU" -eq 1 ] && ! py_supported "$PYTHON"; then
  echo "Configured Python ($("$PYTHON" -c 'import sys;print("%d.%d"%sys.version_info[:2])' 2>/dev/null)) has no PyTorch CUDA wheels (need 3.9-3.13)."
  echo "Looking for a supported Python (3.13 preferred)..."
  ALT="$(find_supported_python || true)"
  if [ -z "$ALT" ]; then
    echo "None found — attempting to install Python 3.13 via pyenv..."
    ALT="$(ensure_pyenv_python || true)"
  fi
  if [ -z "$ALT" ] || ! py_supported "$ALT"; then
    echo "ERROR: could not find or install a CUDA-capable Python." >&2
    echo "Install Python 3.13 with your package manager (e.g. 'apt install python3.13')" >&2
    echo "or pyenv ('pyenv install 3.13'), then re-run: ./setup.sh /path/to/python3.13" >&2
    exit 1
  fi
  PYTHON="$ALT"
  SWITCHED=1
  echo "Using Python: $PYTHON"
fi

if [ "$HAS_GPU" -eq 1 ]; then
  echo "GPU: $GPU_NAME  ->  CUDA wheel: $CUDA"
else
  echo "No NVIDIA GPU detected -> installing CPU PyTorch (training will be slow)..."
fi

# --- Install PyTorch (+ ultralytics / OpenCV) ---------------------------------
echo "Upgrading pip..."
"$PYTHON" -m pip install --upgrade pip

if [ "$HAS_GPU" -eq 1 ]; then
  echo "Installing CUDA PyTorch ($CUDA)..."
  # pip treats "2.x+cpu" and "2.x+cu128" as the SAME base version, so a plain
  # --upgrade would see it "already satisfied" and keep the CPU build. Removing it
  # first forces a clean install of the CUDA wheels.
  "$PYTHON" -m pip uninstall -y torch torchvision torchaudio || true
  "$PYTHON" -m pip install --force-reinstall torch torchvision --index-url "https://download.pytorch.org/whl/$CUDA"
else
  "$PYTHON" -m pip install --upgrade torch torchvision
fi

echo "Installing ultralytics + OpenCV..."
"$PYTHON" -m pip install --upgrade ultralytics opencv-python

# --- Verify + report ----------------------------------------------------------
echo "Verifying..."
"$PYTHON" -c "import torch; print('torch', torch.__version__, '| cuda build:', torch.version.cuda, '| cuda available:', torch.cuda.is_available(), '| device:', (torch.cuda.get_device_name(0) if torch.cuda.is_available() else 'cpu'))"
if [ "$HAS_GPU" -eq 1 ]; then
  if [ "$("$PYTHON" -c 'import torch,sys; sys.stdout.write("1" if torch.cuda.is_available() else "0")')" != "1" ]; then
    echo "WARNING: PyTorch still cannot see the GPU. Check your NVIDIA driver, or try a different CUDA tag (e.g. cu124)."
  fi
fi

# Tell the app which interpreter to use when we switched to a different one. The
# Go installer parses this marker and repoints vision.detector.command at it.
if [ "$SWITCHED" -eq 1 ]; then
  ABS="$("$PYTHON" -c 'import sys; print(sys.executable)')"
  echo "MYMATASAN_PYTHON=$ABS"
  echo "NOTE: switched to Python '$ABS' for GPU support — the app will use it after a restart."
fi

echo "Done. Restart the MyMataSan server, then check Models -> Train in-app."
