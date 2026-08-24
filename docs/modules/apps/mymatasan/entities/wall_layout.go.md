# Module: apps/mymatasan/entities/wall_layout.go

## Purpose

Declares `WallLayout`: one named video wall (W3-3b) — which cameras, in what order, in what
grid, and how the wall behaves unattended.

## Fields

| Field | Type | Notes |
|-------|------|-------|
| `Id` | int64 | Auto-increment primary key. |
| `Name` | string | Required and unique (enforced in the service, case-insensitively, so a clash comes back as a sentence rather than a driver error). |
| `Grid` | string | A layout id — `1x1`, `2x2`, `3x2`, `3x3`, `4x3`, `4x4`. Validated on save. |
| `Cameras` | string | The ORDERED camera ids, comma-separated. Order is the arrangement. |
| `CycleSeconds` | int | Auto-advance through the pages every N seconds; 0 = still. |
| `AutoPopSeconds` | int | Pull an alerting camera onto the visible page for N seconds; 0 = never. |
| `IsDefault` | bool | The wall a screen opens with. At most one row may hold it. |
| `CreatedBy` / `CreatedName` / `CreatedAt` / `UpdatedAt` | int64/string | Who built it and when. |

## Why it is in the database at all

Live View remembered its grid and tiles in a **cookie**, which is a per-browser preference.
A video wall is not a preference — it is how a control room is arranged, and it has to
outlive the browser that built it, reach the operator on the next shift, and be openable on a
second monitor without being rebuilt by hand.

## Why the camera list is a string, not a join table

A join table would need an order column and buy nothing: nothing joins on it, nothing filters
by it, and the whole value is read and written as one unit every time.

## Related

- `apps/mymatasan/services/wall.go.md` — the rules.
- `apps/mymatasan/apis/walls.go.md` — the routes, and why deleting is a POST.
