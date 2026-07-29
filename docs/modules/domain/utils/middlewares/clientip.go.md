# Module: domain/utils/middlewares/clientip.go

## Purpose

Shared client-address resolution: the one answer to "what is the caller's source
address?" used by both the rate limiter and myidsan's audit trail, so a security-relevant
decision cannot silently fork into two different implementations.

## Responsibilities

- `ParseTrustedProxies(entries []string) []*net.IPNet` — turns configured IP/CIDR strings
  into networks. Bare IPs become `/32` (v4) or `/128` (v6). An unparseable entry is skipped
  with a log line rather than failing startup.
- `PeerAddr(r *http.Request) string` — the immediate TCP peer's IP with any port stripped.
  This is the only address a caller cannot forge.
- `PeerIsTrustedProxy(r *http.Request, trusted []*net.IPNet) bool` — reports whether the
  request's immediate peer is one of the configured reverse proxies. An empty `trusted`
  list means nothing is trusted, the safe default for a directly-exposed instance.
- `ClientIP(r *http.Request, trusted []*net.IPNet) string` — the caller's resolved source
  address: the left-most `X-Forwarded-For` entry (the original client the trusted proxy
  saw) or `X-Real-IP`, but **only** when the peer is trusted; the raw peer address
  otherwise. When `X-Forwarded-For` has several comma-separated hops, only the left-most is
  used.
- `UserAgent(r *http.Request) string` — a length-capped (512 bytes) `User-Agent`, since
  clients control this string and it otherwise flows straight into a database column
  (`AuditLog.UserAgent`, `UserSession.UserAgent`) or a log line.

## Notes

- Extracted from `(*RateLimitMidware).clientIP`/`parseTrustedProxies`/`peerIsTrustedProxy`,
  which were private to `rate_limit.go` (`domain/utils/middlewares/rate_limit.go.md`) before
  this file existed. `rate_limit.go`'s versions now delegate here unchanged in behavior.
- The rule that matters, and the reason this is one function instead of two: headers are
  attacker-controlled unless the immediate peer is a reverse proxy the deployment put
  there. Trusting them unconditionally lets a caller choose the address recorded against
  their own actions — quietly destroying an audit trail's value and letting a per-IP
  lockout be evaded by rotating a header. Before this file existed, myseliasan's audit log
  had its own copy that trusted the headers unconditionally; this module is what myidsan's
  new audit trail (`apps/myidsan/apis/audit.go.md`) uses instead, so the two apps' auditing
  cannot drift on this point again.
- Consumed by: `rate_limit.go` (bucketing identity), `apps/myidsan/apis/audit.go`
  (`auditContext`/`newAuditRecorder`), `apps/myidsan/apis/login.go` (recorded login/session
  events), `apps/myidsan/apis/user_login.go`, `apps/myidsan/apis/session.go`,
  `apps/myidsan/apis/stepup.go`.
