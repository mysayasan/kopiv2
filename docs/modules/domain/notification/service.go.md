# Module: domain/notification/service.go

## Purpose

`Service` is the reusable, database-backed notification facade every app publishes through
and reads history from: it owns the hub (`infra/notification.Hub`), the always-on channels
(persistence, log, SSE), a reloadable set of outbound delivery channels
(webhook/telegram/mqtt/email, per destination), and re-exports the engine's core types
(`Notification`, `Attachment`, `Severity`, `Channel`, `Filter`, the `Info`/`Warning`/
`Critical` severities, and the `Category*` constants) so apps depend on this one package
rather than reaching into `infra/notification` directly.

`NewService(repo, opts)` builds the hub with a `StoreChannel` (persistence, registered
first so the row exists before any delivery side effect), a `LogChannel`, an `SSEChannel`
(`opts.SSEClientBuffer`, `infra/notification/sse_channel.go.md`), and a `ReloadableChannel`
for outbound delivery, all pre-registered. `Publish(ctx, n)` sends through the hub, which
invokes every registered channel.

## `Configure(cfg ChannelConfig)` and the destination switch

`Configure` rebuilds the outbound set: one filtered channel per `DestinationConfig` (its own
severity floor + category subscription), addressable by `Id` for tailored per-destination
delivery via `DeliverTo`. `buildDestinationChannel` is the single dispatch point every app
shares — `"webhook"`, `"telegram"`, `"mqtt"`, `"email"` — so a new transport is added in one
place rather than once per app.

**The mail relay is a PARAMETER, not a field on the `Service`.** `ChannelConfig.Smtp` carries
one `SmtpConfig` for the whole install and is passed down to `buildDestinationChannel`
alongside each destination. Stashing it on the receiver would let one `Configure` call read it
while a concurrent one overwrote it — a data race the nightly `-race` job would eventually
catch, and until then a channel built against half of one relay and half of another.

`EmailDestinationConfig` carries only `To`, `SubjectPrefix` and `IncludeSnapshot`: recipients
are routing, the relay is infrastructure, and keeping credentials off every destination row
means one secret to rotate rather than one per recipient list. See
`infra/notification/mail_channel.go.md`.

## `RelayToStream` (Phase 3 — cross-instance event bus)

```go
func (s *Service) RelayToStream(ctx context.Context, n Notification)
```

Pushes an ALREADY-PERSISTED notification to THIS process's live SSE subscribers directly
(`s.sse.Send`) — bypassing the hub entirely, so it never gets persisted again, never gets
logged again, and never gets sent outbound again.

**Why it exists:** a node's events reach exactly one myseliasan instance — the one holding
its control channel — and that instance persists them and pushes them to its own browsers
via the normal `Publish` → hub → `SSEChannel` path. Every other instance behind a load
balancer is serving browsers too, and to them the bell would simply never move. Those other
instances receive the persisted notification over the shared event bus
(`apps/myseliasan/services/node_events.go.md`'s `SubscribeNotifications`) and hand it here.

**Why it deliberately does NOT persist:** the origin instance already wrote the row, so a
second write here would duplicate it in the feed and double-count it in the rollups. The
`Notification` passed in keeps the `ID` it was assigned at the origin, so a browser sees the
same identity regardless of which instance happens to be serving it.

`s == nil || s.sse == nil` is a no-op.

## Notes

- `StreamHandler() http.Handler { return s.sse }` is what `/api/notifications/stream`
  mounts — the same `SSEChannel` `RelayToStream` writes into directly.
- Only myseliasan calls `RelayToStream` today (`apps/myseliasan/app/app.go.md`'s
  cross-instance event bus wiring); other apps' `Service` instances never receive relayed
  notifications because they never subscribe to an event bus.
- See `apps/myseliasan/services/node_events.go.md` for the publish side
  (`NotificationRelayChannel`, registered as a hub channel so the hub's existing
  "invoke every channel on every publish" behavior is what selects the relay set — a node's
  notifications, yes, but equally anything the control plane raises by itself) and
  `infra/eventbus/bus.go.md` for the underlying transport and its at-most-once, unordered
  delivery contract, which `RelayToStream` inherits: a dropped relay message means a
  thinner live feed on the OTHER instances, never data loss, since the origin's own copy is
  already durable in the database.
