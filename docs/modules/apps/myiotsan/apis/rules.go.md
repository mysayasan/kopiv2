# Module: apps/myiotsan/apis/rules.go

## Purpose

Registers the rules and the alert log they produce, under `/api/rules` and `/api/alerts`.

## Responsibilities

- `NewRulesApi(router, rules)` mounts:
  - `GET/POST /rules`, `PUT/DELETE /rules/{id}` — CRUD over `services.SaveRuleRequest`.
  - `GET /alerts` — paged, optionally `deviceId`-filtered alert log.
  - `POST /alerts/{id}/ack` — acknowledge. **There is deliberately no delete route**: an
    operator who was present at an incident must not be able to erase the record of it —
    matches `services.RuleService`'s omission of a `DeleteAlert` method.

## Notes

- Thin HTTP layer over `services.RuleService`; validation (`validConditions`, required
  `Key`/`WindowSeconds` per condition) lives in the service, not here.
- `POST /alerts/{id}/ack` passes both `actorId(r)` and `actorName(r)` (`apis/devices.go.md`)
  through to `AckAlert` so an ack arriving over the fleet tunnel is attributed to the
  control-plane operator by name, not recorded as "System".
