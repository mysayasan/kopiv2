# Module: apps/myseliasan/apis/revocation.go

## Purpose

Wires myseliasan — a relying party — to ask myidsan whether a session it is serving is still live.

At the end of the authorization-code flow this app is handed myidsan's session id and then mints
its **own** token, signed with its **own** key, cached under its **own** TTL
(`sso.sessionTtlSeconds`, three days by default). Nothing in that path can notice a revocation
that happens at myidsan. A live two-process bench watched an administrator revoke every session
for an account, saw the session go `401` at myidsan, and then watched the same browser cookie keep
working here with full access to the fleet.

## Export

`NewSessionRevocationChecker(cfg) *middlewares.RevocationChecker`, returning **nil** — meaning
"behave exactly as before" — whenever this install is not a relying party. It needs all three of
`sso.providerBaseUrl`, `sso.clientId` and `sso.clientSecret`; a standalone install with local
accounts only is completely unaffected.

Wired in `app.go` with `deps.Auth.SetRevocationChecker(...)`, before the auth API is mounted.

## Two things that would have made it silently useless

- **It reuses `providerHTTPClient(cfg)`**, the same CA-trusting client the token exchange uses.
  Building a plain `http.Client` instead would fail every check against a self-signed intranet
  identity server — which is precisely the deployment this suite targets — and, because
  unreachable fails open, the feature would look configured and do nothing at all.
- **A non-200 is reported as "no answer", not as a revocation.** A `4xx` here means this app's own
  client credentials are wrong, or myidsan is too old to serve `/api/auth/session-status` and
  answers `404`. Both are misconfiguration rather than evidence a user's session ended, so a
  mixed-version estate keeps behaving exactly as it does today instead of signing everybody out.

## Notes

- The request carries its own 4-second deadline, derived with `context.WithoutCancel` from the
  request context: it runs inside a request a user is waiting on, so a slow identity server must
  not become a slow fleet console — and a client that gives up must not leave the answer uncached
  and force the next request to ask again.
- The interval is `sso.policyCacheTtlSeconds`, defaulting to 30s when unset. See
  `domain/utils/middlewares/revocation.go.md` for the fail-open reasoning and what happens on a
  definite negative.
