# Module: domain/utils/middlewares/revocation.go

## Purpose

Lets a **relying app** notice that the identity server ended a session it is still serving.

`validateSession` (see `auth.go.md`) answers entirely from *this* app's cache, so it cannot
possibly notice a revocation that happened in another process. A live two-process bench measured
exactly that: an administrator revoked every session for an account, the listing said so, the
session went `401` at myidsan — and the same browser cookie kept working at the fleet console,
with full access, indefinitely.

## Key type

`RevocationChecker{Ask, Interval}`, built by
`NewRevocationChecker(ask func(ctx, sessionId) (active, reachable bool), interval)`.

A nil `ask` or a non-positive interval yields **nil**, and a nil checker never refuses anything —
an app that has not wired this behaves exactly as it did before. Attached with
`AuthMidware.SetRevocationChecker`: a setter rather than a constructor argument because apphost
builds the middleware for every app before any app wires its own services, the same ordering
reason `AccessSessionMidware` takes its resolver through `SetResolver`.

## The two judgement calls

- **Ask on a TTL, not per request.** A round trip in front of every API call would make the
  relying app unusable whenever the identity server is slow. The interval is
  `sso.policyCacheTtlSeconds` (30s by default) — a knob that already existed in the config and,
  until this, drove nothing at all. Revocation is therefore **bounded, not instant**: measured at
  30.7s live against the 30s interval, and that window is what an operator revoking access in an
  emergency needs to know about.
- **Fail OPEN when the identity server cannot be reached; fail CLOSED only on a definite "not
  active".** Failing closed would mean every myidsan restart, network blip or certificate hiccup
  signs the entire estate out of the fleet console — the tool people need most during exactly
  those incidents. Failing open means an attacker who holds a stolen cookie **and** can partition
  the relying app from the identity server keeps access until the token expires. The first
  failure happens regularly and hurts every user; the second needs an attacker with substantial
  network control already. Unreachability is logged once per interval.

## Notes

- A "no answer" is **not** cached: the session id is deliberately not marked as checked, so
  recovery is immediate once the identity server responds again rather than delayed a whole
  interval. Pinned by `TestRevocationCheckerDoesNotCacheANonAnswer`.
- On a definite revocation the local session entry is **deleted**, not merely refused for this
  request. Every later request is then refused by the ordinary cache check with no round trip,
  and the verdict cannot flip back if the identity server becomes unreachable a moment later.
- Verdicts are per session id — a revoked session is not kept alive by a different session having
  been checked recently (`TestRevocationCheckerIsPerSession`).
- The `recent` map is pruned past 4096 entries, so it tracks live sessions rather than growing
  once per session id ever seen.
