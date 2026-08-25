# Module: apps/myseliasan/apis/failover_api.go

## Purpose

The control plane's HTTP surface for N+1 node failover (W3-7). Design lives in
`services/failover.go.md`.

## Routes (`/api/failover-plans`)

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `` | matrix | Every plan, with both appliances' names/status and the readiness verdict |
| GET | `/{id}` | matrix | One plan, plus the per-camera detail read live from the spare |
| POST | `` | superadmin | Create or update a plan |
| DELETE | `/{id}` | superadmin | Delete a plan (refused while it is carrying cameras) |
| POST | `/{id}/stage` | superadmin | Copy the camera set across now |
| POST | `/{id}/drill` | superadmin | Ask the spare to open every staged camera |
| POST | `/{id}/activate` | superadmin | Hand the cameras to the spare |
| POST | `/{id}/release` | superadmin | Hand them back |

## Authorization

**Reading follows the permission matrix** — "is this site covered" is a health question and
hiding it from the people who would notice a gap helps nobody. **Every write is
superadmin-only**, on the same reasoning as a fleet policy but with more at stake: whoever
can write a plan can point a building's cameras at a different appliance, and whoever can
press activate can start recording forty cameras belonging to another site.

`/{id}/drill` is deliberately a first-class button rather than only a scheduled job. It is
the only thing that turns "we have a plan" into "we have tested it", and it should be
something a person can press on a quiet afternoon.

`activate` passes `automatic: false`, so the audit trail distinguishes a takeover a person
chose from one the sweep performed on its own — otherwise the two would be indistinguishable
afterwards, which is the first question asked about an unexpected handover.

## Errors

`ErrFailoverPlanNotFound` → 404; everything else → 400 carrying the service's own wording.
The service's refusals are written to be read ("failover does not chain", "the hold-down must
be at least 120 seconds: …"), and the SPA renders them verbatim rather than substituting a
generic message.
