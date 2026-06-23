# Module: infra/atrest

## Purpose

Reusable **encryption-at-rest** module that makes the Secure Wipe & Reset's wipe guaranteed and device-independent via **crypto-erase**: data is encrypted with a master key; to wipe, the key is destroyed and all ciphertext becomes unrecoverable instantly, regardless of size or storage medium (overwrite alone can't reliably erase SSD/NVMe). Generic over any datatype/file/stream so future features can reuse it. Default **ON**.

## Files & responsibilities

- `cipher.go` — `Cipher` built from a 32-byte key. AES-256-GCM **chunked streaming AEAD**: header `magic "ATR1" + version + 16-byte fileID`; a per-file subkey via stdlib `crypto/hkdf`; 64 KiB chunks; per-chunk nonce = counter; AAD = counter + final-flag to defeat truncation/reordering. `EncryptStream`/`DecryptStream`, `IsEncrypted([]byte)`, `HeaderLen`.
- `files.go` — `EncryptBytes`/`DecryptBytes` (DecryptBytes passes legacy plaintext through), `EncryptFile`/`DecryptFile`, `EncryptFileInPlace` (temp + rename), `DecryptToTempFile` (for ffmpeg/exporters that need a real file), `MaybeDecryptingReader` (peeks the magic → decrypts or streams raw; used for HTTP serving — whole-file, no range requests).
- `keystore.go` — `LoadOrCreate(path)` (32-byte key file, `0600`, dir `0700`; a corrupt-but-present key is an error, never silently regenerated), `Cipher()`/`Enabled()`/`KeyPath()`, and `Destroy()` (overwrite 3× + remove = crypto-erase). The key lives **outside** the media roots so the reset destroys it explicitly.

## Wiring & config

- Config block `security { encryptAtRest *bool (default true), keyPath string (default "secret/atrest.key") }` (`infra/config/config_models.go`).
- `app.go` builds `atrest.LoadOrCreate(keyPath)` → `atrestKeyStore` + `atrestCipher`, threaded into the recorder, vision monitor/snapshots, and training image store. A nil cipher = plaintext write + legacy-passthrough read, so the feature toggles cleanly on/off with no migration.
- Crypto-erase in factory reset: `services/system_reset.go` calls `KeyStore.Destroy()` as the first irreversible step (see `system_reset.go`).

## Applied to

- **Recordings** (`infra/recording`): `remuxSegment` encrypts the finished `.mp4` in place (size accounted as ciphertext); `extractClip` decrypts segments to temp for the ffmpeg concat and encrypts the output; download serves via `MaybeDecryptingReader` (omits Content-Length when encryption is on). The live `.ts` and in-progress remux are briefly plaintext (covered by delete + TRIM during a wipe).
- **Snapshots / alert images**: vision monitor encrypts on save; the snapshot API decrypts on read; alert *notifications* use the in-memory frame (no disk read).
- **Training images**: dataset images are encrypted on store and decrypted for serving/auto-label/export. **Model `.pt` weights stay plaintext** (the Python worker reads them directly); exports write plaintext for the trainer.

## Notes

- AES-NI gives ~GB/s on x86 (ARM could add ChaCha20 later via the `Cipher` abstraction).
- The one residual caveat is the key file itself (tiny; shredded on destroy + covered by TRIM). Pre-encryption files stay plaintext and are read via magic-detect passthrough — no migration step.
- Tests in `atrest_test.go` cover roundtrip, tamper, truncation, wrong-key, legacy passthrough, and destroy.
