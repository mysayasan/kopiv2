# Module: infra/mfa/qr.go

## Purpose

Renders an `otpauth://` provisioning URI as a scannable PNG QR code, entirely
on-box, so myidsan's enrollment screen never needs a client-side QR library or CDN
fetch — required by the suite's air-gapped-intranet constraint.

## Responsibilities

- `QRPNG(otpauthURI, px)` — encodes `otpauthURI` at `px` pixels square (defaults to
  256 when `px <= 0`) using `github.com/skip2/go-qrcode` at `qrcode.Medium`
  recovery level — enough redundancy for a phone camera scan without bloating the
  module count for a short otpauth URI. Returns the raw PNG bytes.

## Notes

- This is the one new third-party dependency the whole MFA feature adds
  (`github.com/skip2/go-qrcode`, pure Go, no cgo, no network at runtime).
- The caller (`apps/myidsan/services/mfa.go`'s `BeginEnroll`) base64-encodes the PNG
  into `MfaEnrollChallenge.QrPngBase64`; the manual-entry base32 secret is always
  offered alongside as a fallback for a device that cannot scan.
