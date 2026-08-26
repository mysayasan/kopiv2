# Module: apps/myidsan/apis/session_status.go

## Purpose

`POST /api/auth/session-status` — how a relying app learns that a session it is still serving has
been revoked here.

## The defect this exists for

myidsan and a relying app are separate processes with separate caches. At the end of the
authorization-code flow the relying app is handed myidsan's session id, then mints its **own**
token under its **own** signing key and caches its own session entry under its own TTL —
`sso.sessionTtlSeconds`, three days by default. Revoking at myidsan deletes myidsan's cache entry
and nothing else.

A live bench watched an administrator revoke an account's sessions, saw the session go `401` at
myidsan, and then watched the same browser cookie keep working at the fleet console with full
access. "Terminate this person's access" did not.

## Why not `/api/sso/introspect`, which already exists for relying apps

Two reasons, and the first is fatal:

1. **It takes a TOKEN and validates its signature with myidsan's key.** The relying app does not
   hold a myidsan token — it discarded the access token after the exchange and issued its own.
   Asked about the relying app's own cookie, introspect answers
   `{"active":false,"reason":"token signature is invalid"}` — correct, and indistinguishable from
   a revoked session. One bench run believed exactly that answer.
2. **It is gated on `sso.internalToken`**, a shared secret a relying app is not required to hold
   and which the SSO settings bundle does not carry. Requiring it would make revocation
   propagation depend on an extra manual deployment step — the kind that silently does not get
   done.

## Contract

Request `{sessionId, client_id, client_secret}` → `{"active": bool}`.

- Keyed on the **session id**, which both sides already share because the relying app reuses
  myidsan's verbatim.
- Authenticated with the `client_id`/`client_secret` pair the app already used to redeem its
  authorization code (`secretMatches`, the same check `/api/auth/token` makes). **No new
  configuration on either side**, and nothing sensitive for the relying app to store.
- Answers from the **cache**, never the session table. The cache is the authority; the table is
  the half that was already telling operators the right thing while the session kept working.
- A cache read error answers `500`, not `{"active":false}`. A store this server cannot read is not
  evidence that a session ended, and answering "revoked" would sign the estate out during a Redis
  blip. The caller treats any non-200 as "no answer" and fails open.

## What it deliberately does not do

It answers only *is this session live*, never who it belongs to, and an unknown id is reported
exactly like a revoked one — so it is not an enumeration oracle for which sessions exist.

## Related

`domain/utils/middlewares/revocation.go.md` (the calling half, its TTL and its fail-open rule) and
`apps/myseliasan/apis/revocation.go.md` (the wiring).
