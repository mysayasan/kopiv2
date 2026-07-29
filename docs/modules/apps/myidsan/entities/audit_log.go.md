# Module: apps/myidsan/entities/audit_log.go

## Purpose

One immutable record of a security-relevant event on the identity server: a sign-in (or a
failed one), a lockout, a second-factor enrolment or admin reset, a role assignment, an
account or SSO-client change, a password reset, a session revocation, or a backup
export/restore.

## Responsibilities

- `Action` is the verb.noun of what happened (`login.success`, `mfa.admin_reset`,
  `session.revoke`, ...) — see the `Action*` constants in `services/audit.go.md`.
- `ActorId`/`ActorEmail`/`ActorRole` attribute the event. `ActorId` is `0` when the actor
  was not authenticated (a failed sign-in, a public forgot-password request) or the event
  was server-raised (a boot-time `RESET_MFA` marker); `ActorEmail` then carries the
  ATTEMPTED identifier, which may not name a real account — that is expected and is exactly
  what an investigation needs. `ActorRole` is captured at event time so a later role change
  does not rewrite what authority the action was taken under.
- `TargetType`/`TargetId` classify what was acted on (`user`, `app`, `role`, `session`,
  `directory`, `backup`, `self`).
- `Outcome` is `success`, `denied`, or `error`.
- `Detail` is a short human summary; `Metadata` is an optional JSON blob of structured
  context (before/after values, sign-in method, restore section counts). **Never** a
  credential, token, TOTP secret, or password hash — this table is readable by every
  superadmin and is exported to CSV.
- `ClientIp`/`UserAgent` are resolved through `middlewares.ClientIP`/`UserAgent`
  (`domain/utils/middlewares/clientip.go.md`), so `ClientIp` cannot be forged by an
  untrusted caller.

## Notes

- The service (`services/audit.go.md`) only ever `INSERT`s during normal operation: there is
  no update path and no way to delete a chosen row, so the trail is append-only. The one
  removal path is age-based retention (`config.audit.retention`, off by default — see
  `infra/config/audit_retention.go.md`), and it is built not to lose anything: expired rows
  are archived to a JSON-lines file on disk before they ever leave the table
  (`services/audit_retention.go.md`'s `PurgeOlderThan`), a run that cannot finish its archive
  deletes nothing, and the purge records itself here (`ActionAuditPurge =
  "audit.retention_purge"`) naming the cutoff, row count, and archive file. That is still
  deliberately stricter than `api_log`, a per-request HTTP access log freely subject to
  retention deletion that carries no action semantics — "someone called
  `PUT /api/user-credential` and got a `200`" does not answer "who granted that role, from
  where, and when".
- Two fields differ from myseliasan's equivalent audit log, because an identity server
  records events for people who are not (yet) authenticated: the actor may be anonymous
  (above), and `UserAgent` is captured — for login, MFA, and session events, the client is
  what distinguishes "signed in from a new laptop" from "someone replayed a cookie".
