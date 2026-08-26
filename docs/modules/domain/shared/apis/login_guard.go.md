# Module: domain/shared/apis/login_guard.go

## Purpose

The failed-login lockout, shared by every appliance app (`mymatasan`, `myiotsan`) and, since
the clustering fix below, by `myidsan` too (`myseliasan` still builds its own instance without
a shared store — see the Notes on `apps/myidsan/app/app.go.md`'s `loginGuardConfig`). Moved
here from `apps/mymatasan/apis/login_guard.go` (behavior-preserving: mymatasan binds it via
`apps/mymatasan/apis/local_auth.go`'s `NewLoginGuard`/`LoginGuard`/`LoginGuardConfig`
re-exports).

## Key Types

- `LoginGuardConfig` — `Enabled`, `MaxAttempts`, `Window`, `BaseLockout`, `MaxLockout`,
  `FailedDelay`. Zero values are filled with safe defaults by `NewLoginGuard`
  (`withDefaults`), so a partially-specified config still works. Each app maps its own
  `loginSecurity` config block onto this (e.g. `apps/myiotsan/app/app.go`'s
  `loginGuardConfig`).
- `LoginGuard` — a thread-safe, in-memory failed-login tracker, keyed by arbitrary strings.
  The middleware keys by source IP only (`loginGuardKeys`): it throttles a host hammering
  credentials without letting an attacker lock a real user out of their account by spamming
  that username (account-axis lockout DoS). The in-memory half deliberately does no I/O — the
  caller decides how to respond and whether to notify. Optionally, `WithSharedStore` (see
  below) attaches a cache-backed half alongside it for clustered deployments; that half does
  do network I/O, bounded and fail-safe.

## Clustered Deployments (`login_guard_shared.go`)

`LoginGuard` on its own is per-process state — exactly right for a single instance, silently
wrong for a clustered one, where every instance keeps its own counters and an attacker's
effective budget is `MaxAttempts × instance count`. `WithSharedStore(store cache.Store,
namespace string) *LoginGuard` attaches a shared cache **alongside** the in-memory map rather
than replacing it, so:

- a locked verdict is the OR of the two halves — the shared half can only ever lock more,
  never less;
- an unreachable cache degrades every shared call to a no-op, so the guard falls back to
  exactly today's per-process behaviour rather than either disabling the lockout or wedging
  every sign-in;
- a guard nobody calls `WithSharedStore` on is bit-for-bit unchanged.

`SharesState() bool` reports whether lockouts are visible across instances, for a deployment
preflight/checklist to read. The counter itself rides on `cache.Store.AllowSlidingWindow` (a
Lua script on Redis, atomic across instances) rather than a read-modify-write, which would
lose increments under exactly the concurrent load an attack produces. See
`login_guard_shared.go.md` for the key layout, the off-by-one between the two counting
schemes (`sharedWindowLimit`), and the escalation/lock/success bookkeeping.

Wired in `apps/myidsan/app/app.go`, gated on `sharedservices.IsSharedCacheProvider(...)` so an
in-process (`memory`) cache never pretends to share state it cannot.

## Key Methods

- `Locked(keys...) (bool, time.Duration)` — whether any key is currently locked, and the
  longest remaining wait across them.
- `RecordFailure(keys...) (lockedNow bool, retry time.Duration)` — registers one failure per
  key; `lockedNow` is true only on the call that trips the lockout (the transition), so the
  caller can notify exactly once.
- `RecordSuccess(keys...)` — clears failure/lockout state after a successful authentication.
- `FailedDelay()` — the configured per-failure delay, applied by the caller outside the lock.

## Notes

- Escalating backoff: each repeated lockout for the same key doubles `BaseLockout` up to
  `MaxLockout` (`escalatedLockout`), guarded against shift overflow.
- `pruneLocked` drops unlocked, idle entries beyond `MaxLockout + Window`, keeping the map
  bounded while letting `lockoutCount` decay only after a genuinely quiet period — an attacker
  who waits out each lockout still keeps escalating.
- A disabled (`Enabled: false`) or nil guard never locks — every method is a safe no-op.
