# Module: domain/shared/apis/login_guard_keys.go

## Purpose

The keys a credential surface throttles on, and the `429` it writes, in ONE place.

`myidsan` grew these first as unexported helpers next to its login handlers. `myseliasan` then
needed the identical pair — it is the other Tier A clusterable app, and a live bench found it
had no lockout at all — and a second copy is how the audit trail came to be written three times
before anyone noticed the copies had drifted (only one truncated hostile input, only one had
retention, only one recorded the user agent). What is encoded here is a security decision, not a
formatting convention, so there is one of it. `apps/myidsan/apis/login.go`'s `loginGuardKey`,
`loginGuardAccountKey`, `loginGuardKeys` and `writeLockout` are now one-line wrappers over these,
kept as names so its call sites read unchanged.

## Exports

- `LoginGuardSourceKey(r) string` — `"ip:" + RemoteAddr`'s host. **Never** a forwarded header.
  `X-Forwarded-For` is attacker-controlled, so believing it would let anyone dodge their own
  lockout by changing a string. A deployment behind a reverse proxy must terminate at this
  process — the documented posture for the whole suite (`clientIP` in `local_auth.go` says the
  same), and the audit trail records the forwarded address separately when the proxy is trusted.
  A `RemoteAddr` with no port still yields a usable key rather than an empty one, which would
  collapse every source onto a single counter.
- `LoginGuardAccountKey(identifier) string` — `"user:" + lowercased, trimmed identifier`, or
  `""` for an empty one. Case-folded so `Admin` and `admin` share a counter; empty means "no key"
  and callers skip it, because a failure with no username attached (a malformed body) must not be
  attributed to some arbitrary account.
- `LoginGuardKeys(r, identifier) []string` — the pair, skipping the account key when empty.
- `WriteLockoutJSON(w, retry, message)` — the `429`. Carries the remaining wait **both** as
  `Retry-After` and as `retryAfterSeconds` in the body, and rounds a sub-second remainder UP to 1.

## Why the account key exists, and what it costs

A lockout keyed only on the source never sees a spray distributed across many addresses, which is
the shape credential stuffing actually takes. The tradeoff is deliberate and was **measured**, not
assumed: someone who knows a username can hold that account locked. That is a nuisance the owner
recovers from by waiting; unlimited guessing against a known account is a compromise they do not
recover from. Any successful sign-in clears both keys, so a legitimate user who still knows their
password is not held out by someone else's failures once the window passes.

## Why the wait is in the body

A browser SPA cannot read a response header cross-origin without an explicit
`Access-Control-Expose-Headers`, and a countdown that silently shows nothing is how an operator
concludes the app is broken rather than that they are locked out.

It is also what tells this refusal apart from the **generic rate limiter's**, which answers the
same status on the same endpoints. A bench that could not tell them apart passed two throttle
checks on rate-limit refusals while the lockout it was measuring did not exist at all
(`tools/fleetbench/README.md`).

## Notes

- The guard is consulted **before** the credential, so a locked caller costs no bcrypt work and
  cannot clear the lockout by supplying the right password. The consequence, which surprised a
  bench: the attempt that *crosses* the threshold has already been evaluated and still receives
  its credential verdict — the lockout applies from the next request on.
- `local_auth.go` keeps its own `writeLoginLockout`, which adds a `WWW-Authenticate` realm header
  the appliance Basic-auth flow needs. It is not a duplicate of `WriteLockoutJSON`.
