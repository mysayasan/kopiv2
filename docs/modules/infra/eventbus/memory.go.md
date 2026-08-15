# Module: infra/eventbus/memory.go

## Purpose

`MemoryBus` fans out within one process — the default provider (`bus.go.md`'s `New`).

**This is the correct implementation for a single instance, not a stub.** Publisher and
subscriber ARE the same process, so an in-process delivery reaches everyone there is to
reach. It is also what keeps a standalone install free of any configuration: the same
wiring in `apps/myseliasan/app/app.go` runs regardless of provider, and `Distributed()`
reporting `false` lets callers (`apps/myseliasan/services/node_events.go.md`'s publish/
subscribe helpers) skip the cross-instance parts entirely rather than branch on them.

## Behavior

- `NewMemoryBus() *MemoryBus` — an empty `map[string][]*memorySub`.
- `Distributed() bool` — always `false`.
- `Publish(ctx, topic, payload)` — copies the current subscriber list under a read lock,
  then hands each one its own goroutine and its own COPY of `payload` (`append([]byte(nil),
  payload...)`) — a copy per handler so subscribers cannot corrupt each other's view, or the
  publisher's buffer, by retaining or mutating the slice. Matches the Redis provider's
  contract that `Publish` never blocks on delivery: a caller must not be able to stall an
  ingest path because one subscriber is slow (`TestMemoryBusPublishDoesNotBlockOnSlowSubscriber`,
  `memory_test.go`). A closed bus's `Publish` is a silent no-op.
- `Subscribe(ctx, topic, handler)` — appends a `*memorySub` under the topic, then a
  goroutine waits on `ctx.Done()` and removes it from the slice on cancel — this is what
  makes cancelling a subscription actually stop delivery, rather than accumulating dead
  handlers for the life of the process (`TestMemoryBusUnsubscribesOnContextCancel`). A `nil`
  `handler` is a no-op.
- `Ping(context.Context) error` — always `nil` (nothing to reach).
- `Close() error` — marks the bus closed and clears every subscription; further `Publish`
  calls are silent no-ops.

## Notes

- No message ordering or delivery guarantee beyond "every subscriber registered at publish
  time gets its own goroutine call" — the same at-most-once, unordered contract `bus.go.md`
  documents for the package as a whole.
- Covered by `memory_test.go`: single-process delivery, fan-out to multiple subscribers,
  topic isolation, unsubscribe-on-cancel, non-blocking publish under a slow subscriber, and
  `New`'s provider-name resolution (`""`/`"default"`/`"memory"`/`"inmemory"`, case-insensitive).
