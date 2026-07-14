# Module: apps/myseliasan/app/correlate_bridge.go

## Purpose

`observeForCorrelation` flattens one node event into what a fleet rule can match on
(`services.NodeEvent`) and hands it to the correlator. Wired into `app.go`'s `onNodeEvent`
callback alongside `ingestNodeEvent` (see `app.go.md`), so every node-pushed event both lands in
the unified notification feed AND is offered to the correlator.

## Responsibilities

- Parses `body` as a `notification.Notification`; if it has a `Title` or `Body`, uses its `Category`/`Title`/`Body`.
- Otherwise (a frame that doesn't parse as a notification) still passes something through rather than dropping it: `Category`/`Title` are set to the raw event `kind`, so a rule that matches on a node going offline still works.
- Leaves `NodeEvent.Kind` **empty on purpose**. `Correlator.Observe` resolves it itself from the injected `nodeKind` function (the adopted node's own record — `registry.List`) rather than trusting anything in the event body. A node claiming its own kind in the payload would let a door sensor assert it is a camera and satisfy a camera-scoped clause.

## Why this reads the NODE's event, not the control plane's republished copy

This is fed the raw event a node pushed up the control channel — the same input
`ingestNodeEvent` consumes — **not** the notification the control plane republishes into its own
feed afterward. Correlating on the control plane's own output would let one fleet rule's alert
satisfy another fleet rule's clause, and two rules could then trigger each other indefinitely.
Events come from nodes; conclusions come from the correlator; the two must not be mixed.

## Notes

- Called synchronously from `onNodeEvent` in `app.go`, with `context.Background()` (not the request context — the control channel's event ingestion has no HTTP request behind it).
- See `apps/myseliasan/services/correlate.go.md` for what happens to the event once observed (arm / disarm / fire), and `apps/myseliasan/app/app.go.md` for the sweep ticker that fires the correlator's grace-period decisions.
