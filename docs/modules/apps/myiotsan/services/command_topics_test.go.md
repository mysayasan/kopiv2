# Module: apps/myiotsan/services/command_topics_test.go

## Purpose

Pins the reserved-topic guard — the rule that keeps every gate in `CommandService.Issue` from being
decoration by making sure there is no *other* way to reach a device.

The guard exists because there was one. The flow canvas's `mqtt_out` node publishes through
`broker.Publish`, the server's own broker handle, which answers to no ACL. Pointed at a device's
command topic it moved a real relay whose actuation was switched off, with a value outside the
declared safe range, past the duty-cycle limit, and wrote nothing in the trail. Found by driving a
real device on a real broker: `tools/fleetbench/bench_iotsan_actuation.py`.

## Responsibilities

- `TestReservedTopic_MatchesTheTopicARealDeviceActsOn` — a real device's own command topic is
  reserved, and the answer names the device and the command so the refusal can say which.
- `TestReservedTopic_LeavesOrdinaryBridgeTopicsAlone` — the negative cases, pinned as carefully as
  the positive one, because the node exists to bridge a value OUT and a guard that blocked that
  would have replaced one bug with another: the device's own key under a different topic, the same
  prefix with a different suffix, the right shape with **no such device**, an empty key, an
  unrelated topic, and the empty string.
- `TestReservedTopic_FixedTemplateReservesTheTopicItself` — a template with no `{deviceKey}`
  addresses every device of its type on one topic, so the topic itself is reserved, and matched
  **exactly** rather than by prefix.
- `TestReservedTopic_PlaceholderAtTheStart` — `{deviceKey}/cmd` leaves an empty prefix; an
  off-by-one there would either miss the match or slice out of range, and this is a safety check,
  so both directions are pinned.
- `TestMqttOutNode_RefusesACommandTopicAndRecordsIt` — the node's own behaviour, through a compiled
  flow: a reserved topic publishes nothing and records the attempt naming `flow:<name>`, and an
  ordinary bridge topic still publishes. The recording half matters as much as the refusal — a
  refusal nobody can see afterwards is half a fix.

## Notes

- `matchReservedTopic` is deliberately free of the database (device existence arrives as a
  callback) so the matching can be pinned here at all. The *wiring* — that production actually
  passes `*CommandService` as the runtime's `topicGuard` — cannot be pinned by a unit test, which
  is exactly why the live bench exists and why it drives the upgrade case: a flow saved while no
  device answered to that key, then the device created.
- See `commands.go.md` ("The reserved-topic guard"), `flow_runtime.go.md` (`doMqttOut`) and
  `flows.go.md` ("Save-time topic check").
