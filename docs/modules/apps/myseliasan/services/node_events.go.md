# Module: apps/myseliasan/services/node_events.go

## Purpose

Wires `infra/eventbus` (`docs/modules/infra/eventbus/bus.go.md`) into myseliasan's own
event shapes. A node holds its control channel to exactly one instance, so only that
instance sees what the node reports; this file is what lets every other instance's live
feed and correlator find out anyway. Two topics, deliberately, because the two consumers
want different things from different sources:

- **`node-events`** (`NodeEventTopic`) — the RAW node event, so every instance's correlator
  sees exactly what the origin's did. This is what lets a fleet rule whose conditions span
  nodes attached to DIFFERENT instances finally arm — see `apps/myseliasan/app/app.go.md`'s
  correlator sweep note. Carries the raw event and never the control plane's own
  re-published notification: correlating on our own output would let one fleet rule's alert
  satisfy another rule's clause, and two rules could then trigger each other forever.
- **`notifications`** (`NotificationTopic`) — every notification THIS instance publishes,
  for the other instances' LIVE FEEDS. The hub already invokes every registered channel on
  every publish, so registering `NotificationRelayChannel` on it relays EVERYTHING —
  a node's, yes, but equally a node-lost alert the heartbeat raised, a certificate-expiry
  warning, an anomaly, or the daily digest, none of which ever arrive on a control channel
  at all. An earlier design folded the feed into the node-event message; that would have
  silently dropped every notification the control plane raises BY ITSELF.

## A third topic: `fleet-rules-changed` (Phase 4)

The correlator holds its rules in memory and reloads them after a save or delete — but only on
the instance that served the request. Behind a load balancer an operator's edit lands on an
arbitrary instance while the LEADER is the one that actually fires rules, so a new rule could sit
unused, and a disabled one keep firing, until something restarted. Nothing about that looks wrong
from the screen where the edit was made.

- `RulesChangedTopic = "fleet-rules-changed"`.
- `PublishRulesChanged(ctx, bus, instanceID, logf)` — announces that the rule set moved. A no-op
  when the bus is `nil` or not distributed.
- `SubscribeRulesChanged(ctx, bus, instanceID, reload func(), logf)` — calls `reload` when ANOTHER
  instance announces a change (drops its own echo by `Origin == instanceID`, same convention as
  the other two topics).

**Wiring status**: `app.go` calls `correlator.SetOnRulesChanged(func() { services.PublishRulesChanged(...) })`
and `services.SubscribeRulesChanged(bgCtx, nodeEventBus, instanceID, func() { correlator.Reload(...) }, busLog)`,
so the publish and subscribe halves are both built and connected end-to-end, and
`Correlator.Save`/`Delete` (`correlate_crud.go.md`) both call `announceRulesChanged` after their
own `Reload` succeeds, so an operator's rule edit or delete actually reaches
`PublishRulesChanged` and is announced to the rest of the deployment. Unlike the other two
topics, `RulesChangedTopic`/`PublishRulesChanged`/`SubscribeRulesChanged` are **not** covered by
`node_events_test.go` — there is no bus-level round-trip or echo-suppression test for this topic
— but the call sites that trigger it are guarded separately: see `correlate_crud.go.md`'s note on
`correlate_announce_test.go`, a source-level (go/ast) regression test asserting `Save` and
`Delete` each contain a call to `announceRulesChanged`. That test exists because this exact
callback went uncalled once already during development — a plain unit/behavioral test would not
have caught it (the code compiled and every other test passed; an unused method is invisible to
the Go compiler), so a source-level assertion is what actually guards a feature whose failure mode
is silence rather than an error.

## Key Types and Functions

- `NodeEventMessage{Origin, NodeID, Kind, Body}` — what crosses `node-events`. `Origin`
  identifies the publishing instance so it can ignore the echo of its own message; without
  it every event would be handled twice at its origin (once locally, once off the bus).
- `NotificationMessage{Origin, N notification.Notification}` — what crosses
  `notifications`. `N` is the row exactly as persisted, so every instance's feed shows the
  same entry with the same id rather than a near-copy.
- `NewInstanceID(advertiseURL string) string` — a stable-per-process publisher identity.
  Prefers `cluster.advertiseUrl` (already unique per instance and legible to a human
  debugging a captured bus message); where that is unset (the single-instance default) uses
  an 8-byte random hex value prefixed `"instance-"` — identity only has to be unique, and a
  lone instance has nobody to be confused with.
- `PublishNodeEvent(ctx, bus, instanceID, nodeID, kind, body, logf)` — puts one node event on
  `node-events` for the other instances' correlators. A no-op when `bus == nil ||
  !bus.Distributed()` — nothing to gain from a round trip when nobody else can be listening.
  A publish failure is logged and swallowed: the event has already been persisted and shown
  locally, so failing the ingest path because a peer could not be told would trade a
  degraded feed for a lost event.
- `NotificationRelayChannel` — a `notification.Channel` (`Name() == "cluster-relay"`),
  built via `NewNotificationRelayChannel(bus, instanceID, logf)` and registered on the
  notification service (`notificationService.Register(...)`,
  `apps/myseliasan/app/app.go.md`). `Send` marshals `NotificationMessage{Origin:
  instanceID, N: n}` and publishes it to `notifications`; a no-op (returns `nil`) when the
  bus is `nil` or not distributed. There is no relay loop: a notification arriving FROM the
  bus is handed straight to `domain/notification.Service.RelayToStream`
  (`docs/modules/domain/notification/service.go.md`), which bypasses the hub entirely, so it
  can never come back out of this channel and re-publish.
- `SubscribeNotifications(ctx, bus, instanceID, handle, logf)` — delivers notifications
  published by OTHER instances to `handle`, dropping any message whose `Origin ==
  instanceID` (the echo of its own relay). A no-op when the bus is `nil`/not distributed or
  `handle` is `nil`.
- `SubscribeNodeEvents(ctx, bus, instanceID, handle, logf)` — the mirror for `node-events`,
  same echo suppression by `Origin`.

## Notes

- Wired in `apps/myseliasan/app/app.go` (`app.go.md`'s "Cross-instance event bus (Phase 3)"
  section): the bus is built once via `eventbus.New`, `instanceID` derived from
  `cluster.advertiseUrl`, `NotificationRelayChannel` registered on the notification service,
  `SubscribeNotifications` wired to `notificationService.RelayToStream`, `onNodeEvent`
  (the control server's frame handler) additionally calls `PublishNodeEvent`, and
  `SubscribeNodeEvents` feeds the correlator's `observeForCorrelation` for events from other
  instances. `PublishRulesChanged`/`SubscribeRulesChanged` are also wired in `app.go` (see "A
  third topic" above) and are reachable end-to-end from `Correlator.Save`/`Delete`.
- Standalone (the default in-process `MemoryBus`), `Distributed()` is always `false`, so
  every function in this file that gates on it is a no-op — behavior for a single instance
  is unchanged by this feature existing.
- Covered by `node_events_test.go` (against a fake `eventbus.Bus`): publish/subscribe
  round-trips for the `node-events` and `notifications` topics, an instance never receives its
  own echo on either, and both publish helpers no-op against a non-distributed bus.
  `fleet-rules-changed` (added Phase 4) has no test coverage of its own yet.
