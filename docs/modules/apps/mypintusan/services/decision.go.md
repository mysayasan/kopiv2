# Module: apps/mypintusan/services/decision.go

## Purpose

The decision path: everything that stands between a badge being presented and a strike firing.
`Decide(Snapshot, PINVerifier) Decision` is a PURE function over a caller-gathered snapshot,
deliberately — a service that queried the database as it went could not be exercised against
the awkward cases (a holiday on a night shift, a cache that expired four seconds ago, a duress
PIN at a critical door running offline) without standing up all of them for real. This shape
makes every one of those a table row, and it means the same code decides identically whether it
is running online against the database or offline against a cached replica. The order of the
gates is fixed by `docs/MYPINTUSAN_DATA_MODEL.md` §5.1 and is not arbitrary: cheap and
security-critical checks come first, so a lockdown or an out-of-service reader is never
overridden by anything later, and an unknown card never reaches a schedule lookup.

## Responsibilities

- `Snapshot` — everything one decision needs, gathered by the caller (`services.Controller.
  snapshot`): the door/reader, the reader's live online/Secure-Channel state, lockdown, offline/
  cache-age, the matched credential/holder (or `nil` for unknown), a presented PIN, the caller-
  joined grant set with resolved schedules/windows, today's holiday, and
  `AntiPassbackViolation` — an INPUT nothing currently computes (P3).
- `Decision` — the outcome: `Granted`, `Reason`, `Detail`, `Duress`, `StrikeSeconds` (`0` when
  denied), `Offline`.
- `PINVerifier` — injected so the decision path never links a password-hashing library and stays
  a pure function; production passes bcrypt, tests pass a stub.
- `Decide` runs ten ordered gates, first failure wins, every outcome shaped for
  `entities.AccessEvent`:
  1. `Door.Enabled`.
  2. Reader online, and (if `Door.RequireSecureChannel`) Secure Channel established. No
     cleartext fallback — the reader goes out of service rather than degrading.
  3. `Lockdown`.
  4. Credential known/`active`/in-date. `lost`/`stolen` are reported as `ReasonCredentialRevoked`
     with the status in `Detail`, distinct from a plain revocation, because a stolen card
     presented at a door is an incident.
  5. Holder known/`active`/in-date.
  6. PIN, where the credential requires one. **Duress is resolved here, not at the end** — a
     duress PIN must produce a GRANT, and everything after this gate runs identically for a
     duress entry (same grant lookup, same schedule check) so a coerced holder is not denied for
     a reason the coercer can see.
  7. At least one `Grant` exists for the holder's groups at this door.
  8. At least one of those grants is in force right now via `scheduleAllows`, accounting for the
     holiday calendar. Grants are additive — ANY passing grant is enough; a conjunction would
     mean adding a group to a holder could remove access.
  9. Anti-passback: soft logs and grants, hard denies (`ReasonAntipassback`).
  10. Offline rules, applied LAST among the denials — deliberately, so a holder who would have
      been denied anyway is denied for the REAL reason rather than `offline-cache-expired`
      masking a stale revocation. `Door.OfflinePolicy == deny` denies outright; past
      `Door.DefaultOfflineTTLSeconds()` denies (`ReasonOfflineCacheStale` — no allow-all exists
      anywhere in this path); a `critical` door additionally requires
      `Holder.OfflineAllowed`.
- `withinValidity(now, from, until)` — `0` at either end means unbounded.
- `scheduleAllows(s, scheduleId)` — resolves a schedule against `Snapshot.Location` (defaults to
  `time.Local`), applying the holiday calendar BEFORE consulting windows (`Holiday.Behaviour`:
  `deny` blocks outright, `follow-sunday` re-maps the weekday, `ignore` is a no-op); a 24/7
  (`Schedule.Always`) schedule short-circuits only after that check, so a deny-holiday still
  closes an otherwise-always-open door. Returns whether the holiday calendar specifically was
  the blocker, so the caller can report `ReasonHoliday` instead of the less useful
  `ReasonOutOfSchedule`.
- `windowCovers(w, weekday, minutes)` — a window whose `EndMin` is BEFORE `StartMin` wraps past
  midnight (the night-shift case); a naive `start <= now <= end` comparison denies such holders
  for their entire shift, and the wrapped tail belongs to the FOLLOWING day, so a Friday
  22:00–06:00 window also covers Saturday 02:00. A window whose `EndMin` EQUALS its `StartMin`
  covers **nothing** — this is the only place in GATE 8 that could fail open, and until it was
  guarded such a window fell into the wrapping branch and matched every minute of every day on
  all seven weekdays. `apis/access_rules.go` refuses to create one; this guard is what protects
  rows already written on installs that accepted them. Both halves are pinned by
  `TestZeroLengthWindowCoversNothing`, which also checks that a window wrapping almost the whole
  way round still grants, so the guard cannot take the night shift with it.

## Notes

- Reachable from both `services.Controller` (online, live `Store`) and — once a cache replica
  exists — an offline path; today only the online caller exists (`services/controller.go.md`).
- Covered by `decision_test.go`, table-driven over the ten gates above. Also carries two tests
  unrelated to the gate table — `TestSecureChannelDefault_OnForTheClassesThatFaceOutward`/
  `_OffForInterior`, over `entities.SecureChannelDefault` (`entities/door.go.md`) — placed here
  alongside `Decide`'s own Secure Channel gate (2) rather than in a new file for one function.
