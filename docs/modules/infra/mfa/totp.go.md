# Module: infra/mfa/totp.go

## Purpose

Package `mfa` implements the second-factor primitives myidsan uses for its own
credentials: RFC 6238 TOTP and the shared secret/otpauth-URI plumbing it rides on.
Hand-rolled on the standard library (`crypto/hmac` + `crypto/sha1`) — the algorithm
is protocol-frozen and tiny, so no third-party dependency is needed for it. The only
external package in the whole feature is the QR encoder (`qr.go`), and even that
runs entirely on-box so an air-gapped install never reaches a CDN.

## Responsibilities

- `GenerateSecret()` — returns a fresh 160-bit (`secretBytes = 20`) random shared
  secret, base32-encoded without padding (`b32`), ready to seal at rest and hand to
  an enrolling authenticator app.
- `TimeStep(unix)` — the RFC 6238 step counter for a unix timestamp (`stepSeconds =
  30`).
- `generateCode(key, step, digits)` — unexported RFC 4226 HOTP / RFC 6238 TOTP
  truncation, digit-parameterised so the test suite can drive it with the published
  8-digit RFC 6238 vectors while production uses 6 (`codeDigits`).
- `Validate(secretB32, code, now, lastStep)` — checks a user-supplied code within
  `±1` step (`skewSteps`) of `now` and enforces a monotonic replay guard: any
  candidate step `<= lastStep` is refused even if the HMAC matches (checked earliest
  step first, so under clock skew `LastStep` still advances to the newest matching
  step). Returns `(ok, acceptedStep)` — the caller **must** persist `acceptedStep` as
  the factor's new `LastStep` so the same or an earlier step can never be replayed.
  Comparison is constant-time (`crypto/subtle`).
- `OtpauthURI(issuer, account, secretB32)` — builds the standard
  `otpauth://totp/<issuer>:<account>?secret=...&issuer=...&algorithm=SHA1&digits=6&period=30`
  provisioning URI an authenticator app consumes; both label components are
  percent-encoded.

## Notes

- Verification deliberately allows only a `±1` step window — a wider window
  multiplies the online brute-force surface for no usability gain.
- Consumed by `apps/myidsan/services/mfa.go` (see
  `docs/modules/apps/myidsan/services/mfa.go.md`), which owns persistence, at-rest
  sealing of the secret, and recovery-code fallback; this package has no knowledge of
  storage or of myidsan's entities.
- See `recovery.go.md` for the single-use recovery-code generator and `qr.go.md` for
  the QR PNG renderer.
