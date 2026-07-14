# Module: apps/myseliasan/apis/fleet_rules_api.go

## Purpose

Registers the HTTP surface for cross-domain correlation rules — the rules that span NODES
rather than living on one. See `apps/myseliasan/services/correlate.go.md` for the engine and
`apps/myseliasan/entities/fleet_rule.go.md` for the data model.

## Endpoints

`NewFleetRulesApi(router, auth, session, correlator)` mounts on `/fleet-rules`, behind
`auth.Middleware` + `session.Middleware`:

| Method | Path | Access | Notes |
|---|---|---|---|
| GET | `/api/fleet-rules` | Any authenticated session | Lists all rules with their clauses (`Correlator.List`). |
| POST | `/api/fleet-rules` | **Superadmin only** | Create or update a rule (`SaveFleetRuleRequest` body, 256 KiB cap, `DisallowUnknownFields`). Validation failures return `400` with the human-readable reason from `validateFleetRule`. |
| DELETE | `/api/fleet-rules/{id}` | **Superadmin only** | Deletes a rule and its clauses. |

## Why writes are superadmin-only

**Writing a fleet rule is a superadmin power.** A correlation rule is a security control that
spans the whole estate — it decides what counts as an intrusion across every adopted node —
and whoever can write one can also write one that never fires (or one that fires on
everything). `requireSuper` checks `session.IsSuperadmin(r)` and returns
`controllers.ErrLimitedAccess` otherwise; this is a hard gate independent of the general
accessrbac permission matrix.

## Notes

- `list` is available to any authenticated session (not superadmin-gated) so an operator can see what rules exist and read their plain-English clauses, even though only a superadmin may change them.
- Reading is deliberately more open than writing for the same reason it is in `myiotsan`'s command history/twin: seeing what a rule does is not the same power as authoring one.
