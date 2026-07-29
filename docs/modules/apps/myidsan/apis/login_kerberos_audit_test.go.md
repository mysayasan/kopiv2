# Module: apps/myidsan/apis/login_kerberos_audit_test.go

## Purpose

Locks in the fix found by a live bench against a real Samba AD DC (realm `KOPI.TEST`):
`kerberosLogin` recorded a rejected SPNEGO ticket only as a Prometheus counter
(`myidsan_federated_login_total`), leaving no audit-trail entry, while a rejected LDAP
password login was recorded. Mirrors `apis/login_federated_audit_test.go.md`'s shape for
the redirect-provider callback.

## Coverage

- `benchKerberos(t)` builds a real `login.KerberosAuthenticator` over an in-memory keytab
  (`github.com/jcmturner/gokrb5/v8/keytab`, one entry for `HTTP/idsan.kopi.test` /
  `KOPI.TEST`) written to a temp file — exercises the real ticket-verification path, not a
  stub.
- `kerberosRequest(t, negotiateHeader)` builds a `loginApi{kerberos, audit:
  &recordingAudit{}}` and calls `kerberosLogin` directly against a `GET /api/login/kerberos`
  request, optionally carrying an `Authorization: Negotiate <token>` header.
- `TestKerberosRejectedTicketIsAudited` — a syntactically-plausible but invalid Negotiate
  token is refused, and now records exactly one `services.ActionLoginFailure` /
  `OutcomeDenied` entry with `Metadata.method == services.MethodKerberos` and a non-empty
  `Detail` naming why the ticket was rejected.
- `TestKerberosChallengeIsNotAudited` — a request carrying **no** `Authorization` header
  (the first half of every SPNEGO handshake — the server answers with a challenge and the
  browser retries) records **nothing**. This is the reason the fix cannot simply audit every
  path in `Negotiate`: doing so would add one audit entry per unauthenticated page load and
  bury the rejections that matter.
- `TestKerberosChallengeIsStillSent` — confirms the challenge response itself (`401` +
  `WWW-Authenticate: Negotiate`) is unaffected by the audit change; Kerberos SSO would
  otherwise never start.
