# Module: infra/atrest/protector.go

## Purpose

Wraps (encrypts) and unwraps the 32-byte master key ("DEK") with a platform- or
operator-provided key-encryption key ("KEK"), so the on-disk key file can be ciphertext
instead of the bare key. The DEK itself never changes across protectors, so switching
protection modes only re-wraps it — all existing at-rest ciphertext (recordings, snapshots,
training images) stays readable.

## Responsibilities

- `KeyProtector` interface — `Name()`, `Wrap(dek) ([]byte, error)`, `Unwrap(blob) ([]byte, error)`. Wrap output is opaque and self-describing per protector; crypto-erase still works by shredding the wrapped blob on disk (`KeyStore.Destroy`) regardless of protector.
- Key-file framing: a legacy key file is exactly `KeySize` (32) raw bytes (the `file` protector, pre-dating wrapping). Anything else is framed by `encodeKeyFile`/`decodeKeyFile` as magic `"ATRK"` + version byte + name-length byte + protector name + wrapped blob, so a file can be identified and unwrapped without external metadata.
- `ProtectorConfig` — resolved `security.*` config: `Name` (`""`/`auto`, `file`, `passphrase`, `dpapi`, `systemd-creds`) plus `Passphrase`/`PassphraseFile`/`PassphraseEnv` for the passphrase protector.
- `NewProtector(cfg)` — builds the selected protector, resolving `""`/`auto` to `platformDefaultProtectorName()`. Requesting a protector the current OS/build cannot provide is an error, not a silent downgrade, so misconfiguration never quietly writes a weaker key file.
- `fileProtector` — the legacy no-op protector (`Wrap`/`Unwrap` are identity); `KeyStore` special-cases it to write a bare `KeySize` file rather than a framed one, for backward compatibility.
- `passphraseProtector` — derives a KEK from an operator passphrase via Argon2id (time=3, memory=64 MiB, threads=4, 16-byte salt, all stored in the blob so parameters can evolve) and wraps the DEK with AES-256-GCM (`newGCM`). Portable across hosts (nothing machine-bound), so it is the right fit for Docker/portable installs and for the recovery escrow that backs up host-bound protectors. `resolvePassphrase` resolves the secret in order: `cfg.Passphrase` → `cfg.PassphraseFile` → `cfg.PassphraseEnv` → the `ATREST_PASSPHRASE` fallback env var.

## Platform protectors

- `protector_windows.go` (`//go:build windows`) — `dpapiProtector` wraps the DEK with Windows DPAPI at **machine scope** (`CryptProtectData`/`CryptUnprotectData`, UI forbidden so a headless service never blocks on a prompt). `platformDefaultProtectorName` returns `dpapi`. Host-bound: unwrap fails on a different machine.
- `protector_linux.go` (`//go:build linux`) — `systemdCredsProtector` shells out to `systemd-creds encrypt`/`decrypt` (stdin/stdout, `--name=kopiv2-atrest`), TPM2-backed when present. `platformDefaultProtectorName` picks `systemd-creds` when `systemd-creds` is on `PATH`, else falls back to `file` (notably containers, where systemd-creds is absent — Docker deployments should set `keyProtector=passphrase` explicitly). Host-bound like DPAPI.
- `protector_other.go` (`//go:build !windows && !linux`, e.g. darwin/BSD) — no native keystore wired yet; `platformDefaultProtectorName` returns `file`, and requesting any other named protector is an error.

## Notes

- `NewProtector` requesting `dpapi`/`systemd-creds` on the wrong platform, or a `systemd-creds` build without the binary, returns a descriptive error rather than falling back silently.
- Because host-bound protectors can't be unwrapped off-box, the passphrase protector doubles as the disaster-recovery escrow format (`KeyStore.ExportEscrow`/`VerifyEscrow`, `atrest.RestoreFromEscrow`) — see `keystore.go.md` and `startup.go.md`.
- Tests: `protector_test.go` (file/passphrase roundtrip, wrong passphrase, corrupt blob, keyfile framing) and `protector_windows_test.go` (DPAPI roundtrip, Windows-only).
