# Module: apps/myidsan/apis/session_index.go

## Purpose

Indexing an issued session, for **every** path that issues one — so it can be listed and revoked.

A session that is not indexed cannot be listed and cannot be revoked. That is not cosmetic:
`/api/session-admin` is the surface an administrator uses when a laptop is stolen or somebody
leaves, and against an unindexed session it answers `{"ok":true,"revoked":0}` — success, having
done nothing.

## What was wrong

Three call sites issue a session on this server. Until a live two-process bench went looking
(`tools/fleetbench/bench_idsan_session_revoke.py`), only **one** indexed what it issued:

| path | what it serves | indexed? |
|---|---|---|
| `loginApi.issueSessionCookies` | JSON local login, LDAP, Kerberos, MFA/WebAuthn completion | yes |
| `federatedAuthApi.issueProviderSession` | the **server-rendered login page** at `/api/auth/login` — where every relying app's SSO redirect lands | **no** |
| `loginApi.setOAuthSession` | the Google/GitHub callback | **no** |

The middle one is how nearly everybody actually signs in, so in practice session administration
was blind to almost every real session. The bench signed a user in through the full
authorization-code flow and found both the user's own session list and the administrator's
listing for that account EMPTY while the user was demonstrably signed in at the relying app.
`apps/myidsan/README.md` had meanwhile claimed the table was populated "on every session issued".

## Exports (package-internal)

- `mintSessionId(claims, sessions) error` — pre-generates the session id. `IssueAuthCookies`
  mints one itself when the claim is empty and **never reports it back**, so a caller that leaves
  it empty cannot know which session it just created. Pre-minting is what makes the session
  indexable at all. A no-op when `sessions` is nil or the id is already set, which is what keeps
  tests that wire no index behaving exactly as before.
- `indexIssuedSession(r, sessions, userId, sessionId, expiresAt, trustedProxies)` — records it.
  Best-effort by design, and the ordering matters: the session is already live by the time this
  runs, so a failure here must not undo it. Refusing a sign-in because the index is unavailable
  is worse than an unlisted session.

## Notes

- One helper rather than a third copy of the same six lines. The rule it encodes — *pre-mint the
  id, then index what you issued* — is the part that was missing, not the formatting.
- Indexing is only half of revocation. Ending a session in **this** server's cache does not end it
  at a relying app, which validates against its own — see `apps/myidsan/apis/session_status.go.md`.
