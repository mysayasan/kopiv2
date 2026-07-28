# Module: infra/mailer/mailer.go

## Purpose

Package `mailer` is a minimal SMTP sender for the OPTIONAL internal-relay features
myidsan needs (today: the self-service password-reset email link,
`apps/myidsan/services/password_reset.go.md`). Pure standard library (`net/smtp`) —
no third-party dependency, and it only ever connects to the operator-configured
relay host. A disabled or unconfigured `Mailer` never reaches for a network, so an
air-gapped install can leave `smtp.enabled` false and nothing changes.

## Responsibilities

- `Config` mirrors the app config `smtp` block (`Enabled`, `Host`, `Port`, `From`,
  `Username`, `Password`, `UseStartTls`) — see `infra/config/config_models.go.md`.
- `New(cfg) *Mailer` constructs the sender; the zero value (or a `nil` receiver) is
  always safe to call `Enabled()`/`Send()` on.
- `Enabled()` is true only when `cfg.Enabled` **and** `cfg.Host` is non-empty — a
  half-configured block (enabled but no host) is treated as off, same fail-soft
  policy the suite uses for OAuth/OIDC/Kerberos.
- `From()` returns the configured envelope sender, falling back to `Username` when
  `From` is blank.
- `Send(to, subject, body)` dials the relay (`net.JoinHostPort`, default port `587`
  when unset), issues `EHLO` with a hostname derived from the sender's domain
  (`smtpHelloName`, falling back to `"localhost"`), optionally upgrades via
  `STARTTLS` (refusing to proceed if the relay doesn't offer it when requested),
  authenticates with `smtp.PlainAuth` only when a username is configured, then sends
  a single-recipient plain-text message (`buildMessage`) and `QUIT`s.
- `buildMessage` assembles a minimal RFC 5322 plain-text message (`From`, `To`,
  `Subject`, `Date`, `MIME-Version`, `Content-Type: text/plain; charset=utf-8`),
  normalising the body to CRLF line endings.
- `stripHeader` strips `\r`/`\n` from every header value (`From`, `To`, `Subject`)
  before it is written — the injection guard: a templated value (e.g. an
  attacker-supplied "To" containing `\r\nBcc: ...`) can never smuggle a new header
  line, only inert inline text on the legitimate line. Covered by
  `TestBuildMessageStripsHeaderInjection`.

## Security

- **Refuses to send credentials over a cleartext link**: if `cfg.Username` is set but
  the connection was never upgraded via `STARTTLS` (either not requested, or the
  relay didn't offer it), `Send` returns an error instead of calling `smtp.PlainAuth`
  — an internal relay that ends up misconfigured (or reachable over an unexpected
  path) can never leak the relay password on the wire.
- No external egress unless explicitly enabled: `Enabled()` gates every code path
  that would otherwise touch the network, and the caller (`apps/myidsan/app/app.go`)
  only constructs a `Mailer` from config, never invents a relay.

## Notes

- Single-recipient only — myidsan only ever emails the one account whose reset was
  requested; there is no batch/bulk send path.
- No retry/queue: a `Send` failure on the reset-email path is swallowed by
  `services/password_reset.go.md`'s `Request` (the operator queue entry it already
  created is the guaranteed fallback), so a transient relay outage never blocks
  account recovery.
