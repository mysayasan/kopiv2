# Module: apps/mymatasan/apis/walls.go

## Purpose

The video-wall HTTP surface (W3-3b), mounted under `/api/walls`.

## Routes

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/walls` | every wall, **plus the grids the server accepts** |
| GET | `/api/walls/{id}` | one wall |
| POST | `/api/walls` | create |
| POST | `/api/walls/{id}` | update |
| POST | `/api/walls/{id}/delete` | remove |

## Why the grids ride along with the list

So a client offers exactly what the server will accept. The SPA keeps its own copy for
rendering; shipping the server's list beside the walls is what stops the two drifting into a
picker whose entries are refused on save.

## Why deleting is a POST

A considered exception rather than a copy of the case routes. The appliance role model keeps
the DELETE verb out of every grantable level so that destroying **footage and records** needs
an administrator. A wall is neither: it is a list of camera ids describing how a room is
arranged, rebuilding one takes a minute, and requiring an administrator to tidy away a stale
wall is friction that buys no safety. Spelling it as a POST lets the operator who arranges the
walls also tidy them, without widening a verb that protects something else.

## Grants

`/api/walls` is **read for a viewer** and read+write for an operator. Reading is part of
watching: a viewer who cannot load the wall gets a blank grid and no way to tell that from
"no cameras". Arranging is the same rung as moving a camera — it changes what a room sees.

## Auditing

`wall.change` and `wall.delete` against `TargetWall`. A wall decides what a control room was
looking at, so "who took that camera off the wall, and when" is a question asked after an
incident.

## Related

- `apps/mymatasan/services/wall.go.md`
