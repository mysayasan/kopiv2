# Module: apps/mymatasan/services/backoff.go

## Purpose

Tiny context-aware sleep + duration-cap helpers left behind in `apps/mymatasan/services` when
the control channel (which used to own them) moved to `domain/shared/fleetnode`. The camera-only
`MediaChannelManager` (which stays here — see `docs/MYIOTSAN_PLAN.md` §6/P6) still needs them for
its own reconnect-with-backoff loop.

## Responsibilities

- `sleepCtx(ctx, d) bool` — sleeps for `d`, or returns `false` immediately if `ctx` is cancelled first. Used to make a backoff wait cancellable rather than blocking shutdown.
- `minDuration(a, b) time.Duration` — the smaller of two durations, used to cap a doubling backoff at a ceiling.

## Notes

- `domain/shared/fleetnode/control_channel.go` has its own private copies of the same two helpers (it cannot depend back on `apps/mymatasan`); this is intentional duplication of two trivial, stateless functions rather than a shared-package addition for something this small.
