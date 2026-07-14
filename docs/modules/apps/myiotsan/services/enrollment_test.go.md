# Module: apps/myiotsan/services/enrollment_test.go

## Purpose

Pins the enrollment window's exact admission rules — the properties `enrollment.go`'s doc
comment calls load-bearing (time-boxed, secret-gated, never re-readable) — down to the byte, plus
the pure helper functions the profile suggester uses.

## Responsibilities

- `TestEnrollment_ClosedWindowVerifiesNothing` — a window that was never opened admits nobody.
- `TestEnrollment_OpenAdmitsOnlyTheMintedKey` — the exact key returned by `Open` verifies; a
  near-miss or empty password does not (no default).
- `TestEnrollment_StatusNeverRevealsTheKey` — `Status()` never carries the key, even right after
  `Open`. An enrollment key readable back out of the API is a permanent credential wearing a
  temporary hat.
- `TestEnrollment_ExpiredWindowStopsAdmitting` — winding the clock past `expiresAt` (directly,
  bypassing any sweeper) makes `VerifyKey` refuse and `Status().Open` report false — expiry is
  enforced on every connect, not by a background sweep that might not have run.
- `TestEnrollment_CloseIsImmediate` — `Close()` invalidates the current key immediately.
- `TestEnrollment_ReopenInvalidatesTheOldKey` — there is only ever one live key; opening a second
  window invalidates the first.
- `TestEnrollment_TTLIsClamped` — a month-long requested TTL is clamped to at most ~1 hour.
- `TestPayloadKeys_ListsTopLevelFields` — sorted top-level JSON field names; non-JSON payload
  yields no keys (not an error).
- `TestTopicPrefix` — extracts the fixed prefix of a topic template up to its first `{`; a
  template with no fixed prefix (e.g. `"{deviceKey}/status"`) has none.

## Notes

- Constructs `Enrollment` with a `nil` db/profiles (`NewEnrollment(nil, nil, nil)`) — none of
  these tests touch the candidate table or the profile suggester, only the window's own state
  machine, so no database is needed.
- `TestEnrollment_ExpiredWindowStopsAdmitting` reaches into the unexported `expiresAt` field
  directly (same package) rather than sleeping in a test.
