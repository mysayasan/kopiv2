# Module: infra/eventbus/bus.go

## Purpose

Fire-and-forget fan-out between instances of ONE app. It exists for the things a single
instance learns but every instance needs to know: a node holds its control channel to
exactly one myseliasan instance, so that instance is the only one that sees the node's
events. Two features break on that — the live notification bell only shows what reached
the instance a browser is connected to, and a fleet rule whose conditions span nodes
attached to different instances never sees them together and never fires — and this
package is what closes both (`apps/myseliasan/services/node_events.go.md`).

## Key Type: Bus

```go
type Handler func(payload []byte)

type Bus interface {
    Publish(ctx context.Context, topic string, payload []byte) error
    Subscribe(ctx context.Context, topic string, handler Handler) error
    Distributed() bool
    Ping(ctx context.Context) error
    Close() error
}
```

- `Publish` never blocks on delivery.
- `Subscribe` registers `handler` for `topic` until `ctx` is cancelled; `handler` runs on
  the bus's own goroutine, so a slow handler delays later events on that subscription —
  callers do the minimum inline and hand off anything expensive.
- `Distributed()` reports whether this bus reaches OTHER instances. `false` for the
  in-process provider, which lets a caller skip work that only matters in a cluster (both
  `node_events.go`'s publish/subscribe helpers no-op when this is false).

## Delivery contract: at-most-once, unordered

This is the honest contract for a publish/subscribe primitive, and deliberately not
strengthened, because both consumers already tolerate it: the notification feed's durable
copy is the database (a browser refresh reconciles from history), and the correlator's
absent-clause sweep is driven by the passage of time, not by any single message arriving.
Anything that must not be lost belongs in the database, not on this bus.

## `New(cfg Config) (Bus, string, error)`

Selects and builds a provider, mirroring `infra/cache` and `infra/coordination`'s own
`New`-plus-config-struct shape:

- `""` / `"default"` / `"memory"` / `"inmemory"` → `NewMemoryBus()` (`memory.go.md`).
- `"redis"` / `"rediscluster"` / `"redis-cluster"` → `NewRedisBus(cfg)` (`redis.go.md`).
- Anything else → `nil, "", &UnsupportedProviderError{Provider: cfg.Provider}`.

**An unrecognised provider is an error, not a silent fall-back to in-process.** Degrading
quietly to a bus that reaches nobody would leave a clustered deployment with a dark
notification bell and rules that never fire, while looking configured — exactly the
failure this package exists to remove. The caller
(`apps/myseliasan/app/app.go.md`) fails boot on this error.

`Config` fields: `Provider`, `KeyPrefix` (the deployment-wide prefix, typically `kopiv2`),
`AppName`, and the Redis dial/command settings (`RedisAddress`, `RedisPassword`, `RedisDB`,
`RedisUseTLS`, `ConnectTimeout`, `CommandTimeout`).

`topicKey(prefix, app, topic)` produces `"<prefix>:<app>:bus:<topic>"`, omitting either
segment when it is empty. **The app segment is what actually separates two apps sharing one
Redis** — `KeyPrefix` does not, because every app in the suite is configured with the same
prefix. This mirrors `infra/coordination`'s `RedisLocker.key()` deliberately: keying on the
prefix alone is what made myseliasan's and myidsan's leader leases collide on a single
`kopiv2:tx:lock:leader`. There is no live collision here today (myseliasan is the only
caller), so this is pre-emptive; the failure it forecloses would be a silent cross-delivery
of one app's events to another app's subscribers, visible only as events arriving that
nobody published.

## Notes

- `apps/myseliasan/app/app.go` resolves `Provider` from
  `deps.Config.Cluster.EventBusProvider(deps.Config.Cache.Provider)`
  (`infra/config/config_models.go.md`) — the bus provider follows the cache provider by
  default, so pointing `cache.provider` at Redis is what enables cross-instance delivery;
  there is no separate required setting to forget.
- Covered by `memory_test.go` (in-process fan-out, multiple subscribers, unsubscribe on
  context cancel) and, indirectly, by `apps/myseliasan/services/node_events_test.go` via a
  fake `Bus`.
