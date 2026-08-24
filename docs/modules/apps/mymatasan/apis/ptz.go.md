# Module: apps/mymatasan/apis/ptz.go

## Purpose

HTTP surface for PTZ **presets, home and guard tours** (W3-5).

## Routes

| Method | Path | Does |
|--------|------|------|
| GET | `/api/cameras/{id}/ptz/presets` | what the camera has stored |
| POST | `/api/cameras/{id}/ptz/presets` | save the current position (`{name}`), or overwrite one (`{name, token}`) |
| POST | `/api/cameras/{id}/ptz/presets/{token}/goto` | recall one (optional `{speed}`) |
| POST | `/api/cameras/{id}/ptz/presets/{token}/delete` | remove one |
| GET | `/api/cameras/{id}/ptz/status` | where the camera is, and whether it is still moving |
| POST | `/api/cameras/{id}/ptz/home` | go home |
| POST | `/api/cameras/{id}/ptz/home/set` | make here home |
| GET | `/api/cameras/{id}/ptz/tours` | this camera's patrols |
| POST | `/api/cameras/{id}/ptz/tours` | create |
| POST | `/api/cameras/{id}/ptz/tours/{tourId}` | update |
| POST | `/api/cameras/{id}/ptz/tours/{tourId}/start` \| `/stop` | begin or end patrolling |
| POST | `/api/cameras/{id}/ptz/tours/{tourId}/delete` | remove |

## Under `/ptz`, on the camera subrouter, on purpose

The role model already expresses "may move a camera" as `/api/cameras/*/ptz`, granted a rung
above watching. Everything here moves a camera or decides when a camera moves, so hanging it
off the same prefix means the capability an administrator already granted keeps meaning what
its label says — rather than a new top-level path that every role would silently lack, or,
worse, that a broad grant would silently hand out.

They are registered on the subrouter `NewCameraApi` returns, not on a second router with an
overlapping prefix: gorilla matches a `PathPrefix` subrouter first and does not reliably fall
through, so a separate `/cameras/{id}/ptz` router would make *which route matched* depend on
registration order — the kind of thing that works in a unit test and 404s on the appliance.

## The grant is read **and** write

`/api/cameras/*/ptz` is `readWrite` at the Live Views page's `use` level, not `write`.
Everything under `/ptz` worth doing has to be **read** first: the preset list is what a "go to
the loading bay" button is built from, the tour list is what a start button acts on, and a
status read is how the screen knows the camera arrived. Granted POST alone, an operator could
command a camera to a preset they had no way to see the name of — the same half-granted
capability the evidence export shipped with. The live bench checks both roles against both
verbs.

## Deleting is a POST

Same reason as the walls: the appliance role model keeps the DELETE verb out of every
grantable level so that destroying **footage** needs an administrator, and a preset is not
footage. It is audited either way.

## What is audited, and what is not

`ptz.preset_save`, `ptz.preset_delete`, `ptz.home_set`, `ptz.tour_change`, `ptz.tour_delete`,
`ptz.tour_run`.

A preset is **where an alarm will send this camera**, so quietly re-pointing "Front gate" at
the sky is a way to make a rule useless that leaves the rule, the tour and the screen all
looking correct — and nothing else records it. Starting and stopping a patrol is recorded for
the same reason: *why was this camera not looking at the door* is answered by who stopped its
tour, and when.

**Recalling a preset is deliberately not audited.** An operator driving a camera generates one
per press, and a trail that fills with them is a trail nobody reads.
