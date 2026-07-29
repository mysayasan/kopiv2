# Module: infra/apphost/shared_state.go

## Purpose

States, once at startup, whether this process's session state is visible to other instances of itself — so a load-balanced multi-instance deployment that is silently single-instance-only reads as a config problem in the boot log, not as "flaky sign-ins" reported by users later.

## Responsibilities

- `warnSharedStateBoundary(cacheProvider, lockProvider string)` — called from `runApp` (`run.go`) right after the transaction-lock provider log line, with the already-resolved cache and transaction-lock provider names:
  - `isSharedCacheProvider(cacheProvider)` true (`redis`/`rediscluster`/`redis-cluster`) — logs that sessions survive a restart and are visible to every instance, confirming the app can run behind a load balancer.
  - Otherwise, `isDistributedLockProvider(lockProvider)` true — logs a `WARNING`: a distributed transaction lock only makes sense with more than one instance, but the session cache is per-process, so a second instance will not recognise this one's sessions and users are signed out whenever the load balancer moves them. Names the fix (`cache.provider` pointed at the same Redis).
  - Otherwise — logs plainly that the cache is per-process, this instance is **single-instance only**, and states the consequences (users signed out on every load-balancer switch; a restart ends every session) and the fix.
- `isSharedCacheProvider(provider)` / `isDistributedLockProvider(provider)` — both do a case-insensitive/trimmed match against `redis`, `rediscluster`, `redis-cluster`. Kept as name checks against the provider string the operator wrote in `config.json`, rather than a capability flag on the built store, because the message has to talk about that same string.

## Notes

- Sessions live in the cache and the cache is the authority on session validity (see `docs/DB_BOOTSTRAP_SPEC.md`'s note that live session validation uses the configured cache provider, not a DB table); with the in-process memory cache that authority is per-process, which is the entire reason this exists.
- The loud (`WARNING`) branch is deliberately a contradiction check, not a guess: there is no reliable way for a process to detect "am I one of several replicas" from the inside, and a warning that fires on healthy single-instance installs is one operators learn to ignore. A distributed transaction lock is the one config signal that already says "more than one instance is expected", so pairing it with a per-process cache is a fact about the configuration, not a hunch about the topology.
- Applies to every app (`mymatasan`, `myseliasan`, `myidsan`, `myiotsan`) — this is shared `infra/apphost` wiring, not identity/myidsan-specific, even though the productization plan that motivated it (`docs/MYIDSAN_PRODUCTIZATION_PLAN.md` §4.2) is myidsan's.
