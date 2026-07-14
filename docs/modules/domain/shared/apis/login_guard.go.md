# Module: domain/shared/apis/login_guard.go

## Purpose

The failed-login lockout, shared by every appliance app (`mymatasan`, `myiotsan`). Moved here
from `apps/mymatasan/apis/login_guard.go` (behavior-preserving: mymatasan binds it via
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
  that username (account-axis lockout DoS). Deliberately does no I/O — the caller decides how
  to respond and whether to notify.

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
