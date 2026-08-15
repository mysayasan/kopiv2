# Module: infra/eventbus/redis.go

## Purpose

`RedisBus` fans out across instances with Redis publish/subscribe — the distributed
provider (`bus.go.md`'s `New`, selected by `"redis"`/`"rediscluster"`/`"redis-cluster"`).

## Why publish/subscribe, not a stream or a queue

Both consumers want the SAME event delivered to EVERY instance: the notification bell
renders it to whichever browsers that instance is serving, and the correlator folds it into
its own matching state. A queue would hand each event to exactly one consumer — the
opposite of what either side needs. A stream would add durable retention for data whose
durable copy is already the database (`bus.go.md`'s delivery-contract note).

## Behavior

- `NewRedisBus(cfg Config) *RedisBus` — `ConnectTimeout`/`CommandTimeout` default to 2s when
  `<= 0`. TLS (`cfg.RedisUseTLS`) uses `tls.VersionTLS12` minimum. The underlying
  `redis.Client`'s read/write timeouts are DELIBERATELY left at go-redis's own defaults
  rather than `cfg.CommandTimeout` — a subscription is a long-lived read that must stay open
  between events, and a read deadline sized for one command would tear it down every quiet
  period.
- `Distributed() bool` — always `true`.
- `Publish(ctx, topic, payload)` — `PUBLISH` on `topicKey(cfg.KeyPrefix, topic)`, wrapped in
  a `cfg.CommandTimeout` context.
- `Subscribe(ctx, topic, handler)` — opens a Redis `SUBSCRIBE`, then a goroutine reads
  `sub.Channel()` until `ctx` is cancelled or the channel closes, calling `handler` with each
  message's raw payload. **go-redis reconnects a dropped subscription on its own**, so a
  Redis restart costs only the events published while it was down — at-most-once, as
  documented, and NOT the subscription itself. Letting the subscription silently die instead
  would be far worse: the instance would keep serving with a bell that never updates and
  rules that never match, with nothing visibly wrong.
- `Ping(ctx)` — `PING` wrapped in `cfg.CommandTimeout`; nil-safe (a `nil` receiver or client
  returns `nil`).
- `Close() error` — closes the underlying `redis.Client`; nil-safe.

## Notes

- `apps/myseliasan/app/app.go` calls `nodeEventBus.Ping` once at boot when
  `Distributed()` is true and fails startup on error — an unreachable configured Redis is a
  hard boot failure, not a silent fall-through to a bus that reaches nobody (see
  `bus.go.md`'s `New` note on the same principle).
- Shares its Redis connection settings (`Address`/`Password`/`DB`/`UseTLS`/timeouts) with
  `infra/cache`'s own Redis provider, since `apps/myseliasan/app/app.go` resolves the bus's
  provider and connection details from `deps.Config.Cache.Redis` by default (see
  `infra/config/config_models.go.md`'s `ClusterConfigModel.EventBusProvider`) — the same
  Redis a deployment already pointed the cache at is what the bus uses too, unless
  `cluster.eventBusProvider` overrides it.
