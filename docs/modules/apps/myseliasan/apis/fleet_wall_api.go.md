# Module: apps/myseliasan/apis/fleet_wall_api.go

## Purpose

HTTP surface for fleet video walls (W3-3d). Design lives in `services/fleet_wall.go.md`.

## Routes (`/api/fleet-walls`)

| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `` | matrix | Every wall, tiles resolved against the live fleet |
| GET | `/grids` | matrix | The layouts the server will accept |
| GET | `/{id}` | matrix | One wall |
| POST | `` | superadmin | Create or update |
| DELETE | `/{id}` | superadmin | Delete |

## Why reading and writing differ

**Reading follows the matrix.** A wall is what a control room watches, and the people who watch
it are exactly the people who should not need an administrator to open it.

**Writing is superadmin-only.** A wall is SHARED: changing one changes what everybody on that
screen sees, including the person who is mid-shift and did not change anything. The default
wall decides what a guard station opens with when nobody chooses. That is an estate decision,
on the same reasoning as a fleet policy.

`GET /grids` exists so a client offers exactly the layouts the server will accept, instead of
discovering the answer through a 400.
