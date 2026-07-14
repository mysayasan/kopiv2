# Module: apps/myiotsan/entities/alert_event.go

## Purpose

Defines one firing of an `IotRule`: what tripped, on which device, at what value, when. Carries
the READING CONTEXT (`Key`, `Value`, `Message`) rather than a snapshot path, which is the only
real difference from mymatasan's `alert_event` — an operator asking "why did this fire?" three
weeks later gets the actual number, not a rule name and a shrug.

## Fields

- `Id`, `RuleId` (indexed), `DeviceId` (indexed).
- `RuleName`/`DeviceName` — **denormalized** so an alert stays readable after the rule or the
  device it names is deleted; an alert log with dangling references is not evidence.
- `Key`, `Value` (the reading that tripped the rule), `Message` (the human sentence, e.g. "Cold
  store temperature 8.4C is above 5C"), `Severity`.
- `AckedAt`/`AckedBy` — records the operator acknowledging the alert. Acknowledging is an
  operator power; **deleting is not** — no `DeleteAlert` exists anywhere in the service layer.
  That is the same evidentiary line mymatasan draws.
- `AckedByName` — who `AckedBy` WAS, by name, for the same reason `DeviceCommand.RequestedByName`
  exists: an ack can arrive over the fleet tunnel from a control-plane operator with no account on
  this node (`AckedBy == 0`), and without the name the node's own alert log would say the alert
  was acknowledged by "System". `apis.actorName(r)` supplies it (`cp:<who>` for tunnel callers).
- `CreatedAt`.

## Indexes

`idx:"dev_time"` on `DeviceId`; `CreatedAt` joins it via `idx:"dev_time,time"` — what the alert
log and the device detail page both read.

## Notes

- Written STRAIGHT TO DISK by `services.RuleService.fire` (`services/rules.go.md`), never
  through the batched `ReadingWriter` — an alert lost in a buffer during the very crash it was
  warning about would be worse than useless.
- Doubles as the **durable cooldown record**: `services.RuleService.Reload` re-seeds each
  (rule, device) cooldown in the rule engine from the most recent alert row for that pair,
  rather than from a rule-level column — see `services/rules.go.md`.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB
  engine starts.
