# Module: domain/entities/user_session.go

## Purpose

Shared entity for persisted SSO session audit and revocation workflows.

## Notes

- The current runtime hot path stores and validates live sessions in cache under `sso:session:<sid>` — that remains the AUTHORITY on validity.
- The table was declared and created long before anything wrote to it. It is now populated by `apps/myidsan/services.ISessionService` (`services/session.go.md`) as an INDEX over the cache, because the cache is keyed by session id alone and cannot answer "which sessions does this user have?" — the question both an administrator revoking access and a user reviewing their own devices need answered. A row whose cache entry is gone is already dead regardless of what the row says.
- `IpAddress`, `UserAgent`, and `LastSeenAt` were added when the table was finally populated:
  - `IpAddress` is the source address at sign-in, resolved through the trusted-proxy rules (`domain/utils/middlewares/clientip.go.md`) so it cannot be forged by an untrusted caller.
  - `UserAgent` is the signing-in client, length-capped at capture.
  - `LastSeenAt` is refreshed as the session is used (`ISessionService.Touch`), so a stale entry is distinguishable from one in active use.
- `SessionId` is `ukey1`; `UserLoginId` is `fkey1`. Listing a user's sessions filters on `UserLoginId` with an explicit `Equal` rather than `GetByForeign`, since that helper is hardcoded to a single child row.
