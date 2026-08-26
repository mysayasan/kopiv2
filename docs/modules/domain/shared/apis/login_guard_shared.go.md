# Module: domain/shared/apis/login_guard_shared.go

## Purpose

Extends `LoginGuard` (`login_guard.go.md`) with an optional shared-cache half so a failed-login
lockout spans every instance of a clustered deployment, instead of tripping — or failing to
trip — per process.

Found by a live two-instance bench (`tools/fleetbench/bench_idsan_lockout.py`) against myidsan
on one shared Postgres and one shared Redis, the shipped configuration (generic rate limiter
on, `loginSecurity` at defaults): instance A locked an account after eight wrong passwords;
instance B then evaluated a ninth normally (`401`) and, worse, accepted the CORRECT password
with `200` — signing in a user that was supposed to be locked out. An attacker's budget was
`MaxAttempts × instance count`, escalating backoff restarted at its base on every instance, and
none of it showed up on the deployment preflight checklist (`dbEngine`/`sharedCache`/
`sharedLock`/`atrestKey`/`jwtSecret`/`dbPool` — nothing about the lockout).

## Key Types

- No new exported type. `WithSharedStore`/`SharesState` are new methods on `LoginGuard`
  (`login_guard.go`); this file adds two unexported fields, `store cache.Store` and
  `namespace string`.

## Key Methods

- `(g *LoginGuard) WithSharedStore(store cache.Store, namespace string) *LoginGuard` — attaches
  the shared half and returns `g` for chaining onto `NewLoginGuard`. `namespace` separates apps
  that share one cache (a myidsan lockout is not a myseliasan lockout, different credentials
  against different stores). Passing a nil `g` or a nil `store` is a safe no-op/pass-through.
- `(g *LoginGuard) SharesState() bool` — true only when the guard is enabled AND a store is
  attached; a deployment checklist reads this to tell an operator the truth about their
  cluster.
- `sharedLocked(keys...) (bool, time.Duration)` — reads the shared lock key per key, OR'd with
  the in-memory verdict by `Locked` (in `login_guard.go`). Any cache error/timeout reports "not
  locked" so the in-memory verdict decides alone — the shared half can only ever add a lockout,
  never remove the in-memory one.
- `sharedRecordFailure(keys...) (lockedNow bool, retry time.Duration)` — advances
  `Store.AllowSlidingWindow` per key and, on threshold crossing, writes a lock key with an
  escalating duration (`escalatedLockout`, continuing the SAME escalation counter across
  instances rather than restarting it on each one) plus a fresh escalation counter TTL'd past
  the lockout it produced.
- `sharedRecordSuccess(keys...)` — clears the window, lock, and escalation keys for these keys
  on a correct credential, mirroring the in-memory guard's `RecordSuccess`.
- `sharedWindowLimit()` — `MaxAttempts - 1` (floored at 1), **not** `MaxAttempts`. The two
  counting schemes trip on different attempts by construction: the in-memory guard increments
  then locks when the total REACHES `MaxAttempts`; `AllowSlidingWindow` refuses when the stored
  count is already `>= limit`, checked BEFORE recording the current attempt. Passing
  `MaxAttempts` straight through would give every instance one extra free guess before the
  shared half ever locks — observed live in the bench this file's comment describes.

## Notes

- Every shared-store call is bounded by `sharedGuardTimeout = 2s`, so a slow/unreachable cache
  cannot hang a sign-in; on timeout the shared half is simply skipped for that call and the
  in-memory half still applies.
- Ordering in `Locked`/`RecordFailure` (in `login_guard.go`) deliberately calls the shared
  lookup BEFORE taking the in-memory mutex — the shared call does network I/O, and holding the
  lock across it would serialise every sign-in on the slowest cache round trip.
- Key layout under `guardSharedPrefix = "loginguard:"`: `<namespace>:<key>:win` (sliding-window
  counter), `<namespace>:<key>:lock` (unix expiry of an engaged lockout — stored rather than
  read back via TTL, since `cache.Store` exposes no TTL-read, and the caller needs the
  remaining wait for `Retry-After`), `<namespace>:<key>:esc` (escalation count, TTL'd past
  `MaxLockout + Window` so a genuinely quiet key eventually forgets, same horizon the in-memory
  guard prunes on).
- Known and deliberate limit, pinned by a test in `login_guard_shared_test.go`: a success on
  one instance clears the SHARED counter but cannot clear another instance's in-memory tally —
  a user who fails almost-to-threshold on instance A, succeeds on instance B, then fails once
  more on A can still be locked by A's own in-memory count alone. Bounded to one base lockout;
  the failure mode errs toward locking, not toward admitting an attacker.
- Wired in `apps/myidsan/app/app.go` via `loginGuard.WithSharedStore(deps.Cache, "myidsan")`,
  gated on `sharedservices.IsSharedCacheProvider(deps.Config.Cache.Provider)` — see
  `apps/myidsan/app/app.go.md`. `myseliasan`, the suite's other Tier A clusterable app, does not
  call `WithSharedStore` yet (no bench covers its login surfaces); its lockout stays
  per-process.
