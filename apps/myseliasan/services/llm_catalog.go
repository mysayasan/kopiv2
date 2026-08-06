package services

import (
	"fmt"
	"runtime"
)

// The pinned LLM sidecar artifacts. PINNED, not "latest": the installer verifies
// SHA-256 against these constants, so a compromised CDN or a repointed release
// cannot slip a different binary onto an operator's control plane. Bumping the
// sidecar version is a deliberate code change that re-pins all three hashes
// (download each asset once, sha256sum it, update here — the values below were
// computed from the artifacts on 2026-08-06).
const (
	// llamaCppReleaseTag is the llama.cpp release the sidecar downloads.
	llamaCppReleaseTag = "b10289"

	llamaAssetWinAmd64 = "llama-" + llamaCppReleaseTag + "-bin-win-cpu-x64.zip"
	llamaSHAWinAmd64   = "da980054e6ce6f27ff292c7d5250e8547cd6a28d5b4abd69a97bf97e4ccb28b8"

	// The Linux asset is a tar.gz (not a zip) with everything nested under a
	// "llama-<tag>/" directory and ~10 symlinks — the extractor handles all of it.
	llamaAssetLinuxAmd64 = "llama-" + llamaCppReleaseTag + "-bin-ubuntu-x64.tar.gz"
	llamaSHALinuxAmd64   = "b86c74a636b94deacc3c5fd94c498c16a3ce85bd312b875a813c5c125f64d931"

	llamaReleaseBaseURL = "https://github.com/ggml-org/llama.cpp/releases/download/" + llamaCppReleaseTag + "/"

	// The default model: Qwen2.5-1.5B-Instruct Q4_K_M (~1.1 GB). Chosen because it
	// is small enough for CPU-only inference at usable speed and genuinely
	// multilingual across the suite's four UI languages (en/ms/zh/ar). Operators
	// can import any other GGUF instead.
	defaultModelFile = "qwen2.5-1.5b-instruct-q4_k_m.gguf"
	defaultModelURL  = "https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF/resolve/main/" + defaultModelFile
	defaultModelSHA  = "6a1a2eb6d15622bf3c96857206351ba97e1af16c30d7a74ee38970e434e9407e"

	// The larger tier: Qwen2.5-7B-Instruct Q4_K_M (~4.7 GB, needs ~6 GB free RAM).
	// Noticeably better prose and instruction-following — including the Arabic
	// replies the 1.5B routinely answers in English. From bartowski's repack
	// because the official Qwen 7B GGUF repo ships the file SPLIT in two parts,
	// which the single-file installer/resolver deliberately does not handle.
	largeModelFile = "Qwen2.5-7B-Instruct-Q4_K_M.gguf"
	largeModelURL  = "https://huggingface.co/bartowski/Qwen2.5-7B-Instruct-GGUF/resolve/main/" + largeModelFile
	largeModelSHA  = "65b8fcd92af6b4fefa935c625d1ac27ea29dcb6ee14589c55a8f115ceaaa1423"

	// Size caps for downloads/imports (io.LimitReader guards, not exact sizes).
	maxBinaryArchiveBytes = 512 << 20 // the b10289 archives are ~18 MB
	maxModelBytes         = 8 << 30   // q4 models up to ~8 GB importable
)

// llamaServerDownload returns (url, sha256, assetName) for the given platform,
// or an error for platforms with no pinned download — the UI then offers the
// import path only (which works everywhere llama.cpp itself builds).
func llamaServerDownload(goos, goarch string) (string, string, string, error) {
	switch goos + "/" + goarch {
	case "windows/amd64":
		return llamaReleaseBaseURL + llamaAssetWinAmd64, llamaSHAWinAmd64, llamaAssetWinAmd64, nil
	case "linux/amd64":
		return llamaReleaseBaseURL + llamaAssetLinuxAmd64, llamaSHALinuxAmd64, llamaAssetLinuxAmd64, nil
	}
	return "", "", "", fmt.Errorf("no pinned llama.cpp download for %s/%s — import a llama-server build instead", goos, goarch)
}

// llamaServerExeName is the base name of the server executable inside an
// extracted release (or an operator's own build).
func llamaServerExeName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}
