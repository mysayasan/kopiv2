# Module: apps/myiotsan/services/flows_test.go

## Purpose

Exercises the flow engine in isolation — compile a graph, drive a message through it, and inspect
the debug snapshot and the fake outputs. No database, no broker: the CRUD/transfer paths and the
builtin catalog are covered by the live boot against the simulator. The high-value, high-risk
behaviour is here — propagation, the JS sandbox, the watchdog, and the guarantee that actuation
only ever leaves through the guarded issuer.

## Responsibilities

- `fakeNotifier`/`fakeDevices`/`fakeSink` — the `flowNotifier`/`deviceResolver`/`readingSink`
  (`flow_runtime.go.md`) test doubles a compiled flow's output nodes are driven against.
- **Propagation + declarative nodes**: `TestFlow_ScaleThenThresholdPasses`,
  `TestFlow_ThresholdBlocksBelow`, `TestFlow_DeadbandSuppressesSmallMoves` — scale/threshold/
  deadband nodes transform and gate a message correctly.
- **The JavaScript nodes**: `TestFlow_ExpressionComputes`, `TestFlow_FunctionCombinesTwoStreams`
  (pins the per-flow `flow.get`/`flow.set` context surviving across two separate input messages —
  the mechanism the solar self-consumption sample depends on), `TestFlow_FunctionReturningNullDrops`
  (Node-RED semantics).
- **The sandbox cannot escape, and cannot hang the worker**: `TestFlow_SandboxCannotReachHost`
  (`require`/`process`/`readFileSync` all fail to resolve), `TestFlow_WatchdogKillsInfiniteLoop`
  (a `while(true){}` node is interrupted within the 3s test bound and never reaches downstream).
- **Actuation is contained**: `TestFlow_CommandRoutesThroughGuardedIssuer` — the safety invariant:
  even arbitrary JS upstream can do nothing but shape the value the guarded issuer then receives;
  `TestFlow_NotifyPublishes`.
- **Validation at the door**: `TestFlow_ParseGraphRejectsBadGraphs` — unknown type, dangling wire,
  cycle, duplicate id all refused; a well-formed and an empty graph both parse.
- **Derived metric output (P3)**: `TestFlow_DerivedMetricWritesReading`.
- **Templates (P3)**: `TestFlow_SlotDetectionAndSubstitution`, `TestFlow_InstantiateBindsAllSlots`,
  `TestFlow_BuiltinSolarIsATemplate`.
- **Transfer**: `TestFlow_ExportImportDocVersionGuard` — a document from a future format version is
  refused rather than half-understood.

## Notes

- `compileForTest`/`graphJSON`/`seed` are small helpers so each test reads as graph-in,
  snapshot-out.
