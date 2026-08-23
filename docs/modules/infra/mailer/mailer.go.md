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
- `SendMessage(msg Message)` is the full send: one SMTP conversation carrying EVERY
  recipient in `msg.To`. It dials the relay (`net.JoinHostPort`, default port `587`
  when unset), issues `EHLO` with a hostname derived from the sender's domain
  (`smtpHelloName`, falling back to `"localhost"`), optionally upgrades via
  `STARTTLS` (refusing to proceed if the relay doesn't offer it when requested),
  authenticates with `smtp.PlainAuth` only when a username is configured, then sends
  the assembled message (`Message.build`, `infra/mailer/message.go.md`) and `QUIT`s.
- `Send(to, subject, body)` is now a thin wrapper over `SendMessage`, kept because
  myidsan's password-reset path has no need of recipient lists or attachments.
- `normalizeRecipients` splits comma-separated entries, trims, drops blanks, and
  de-duplicates case-insensitively while preserving order. Operators paste these lists from
  a directory or a spreadsheet, where the same address appearing twice is routine — and the
  same alert arriving twice reads as a bug in the alerting.
- Message assembly (headers, MIME, attachments, the injection guard) moved to
  `Message.build` — see `infra/mailer/message.go.md`.
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
  `TestSendMessageRefusesCleartextCredentials` points the sender at a relay that OFFERS
  `AUTH` and would happily take the password, then asserts the relay saw **no `AUTH`
  command at all** — not merely that the call returned an error. (An earlier version of that
  test set `UseStartTls: true`, which trips the "STARTTLS not offered" check first and never
  reaches this guard; a mutation pass found it passing with the guard disabled.)
- No external egress unless explicitly enabled: `Enabled()` gates every code path
  that would otherwise touch the network, and the caller (`apps/myidsan/app/app.go`)
  only constructs a `Mailer` from config, never invents a relay.

## Partial delivery is a SUCCESS

When the relay accepts some recipients and rejects others at `RCPT` time, `SendMessage`
delivers to the accepted ones and returns a `*RecipientError` with `AllRejected` false —
the message **was** sent. Only a message that reached NOBODY is a failure
(`AllRejected` true).

Failing the whole send on one bad address would mean a single stale entry in a distribution
list silences the alert for everybody else on it. Worse, because the notification channel
retries transient failures, it would do so on **every** alert, forever. The `Delivered()`
distinction is what lets `infra/notification/mail_channel.go.md` stop retrying a partial
send while still retrying a relay that is genuinely down. The `To` header is rewritten to
name only the accepted addresses, so the message a recipient reads does not claim it went
somewhere it did not.

Asserted end to end against a real in-process SMTP server (`smtptest_test.go`) by
`TestSendMessagePartialRejectionStillDelivers` and `TestSendMessageAllRejectedIsAFailure`.

## Notes

- Multi-recipient since W2-7; myidsan's reset path still sends to exactly one address
  through the `Send` wrapper.
- No retry/queue: a `Send` failure on the reset-email path is swallowed by
  `services/password_reset.go.md`'s `Request` (the operator queue entry it already
  created is the guaranteed fallback), so a transient relay outage never blocks
  account recovery.
