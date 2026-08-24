# Module: apps/mymatasan/services/wall.go

## Purpose

`wallService` / `IWallService`: named, shared video walls (W3-3b) — which cameras, in what
order, in what grid, and how the wall behaves while nobody is touching it.

Live View already remembered a grid and a set of tiles, **in a cookie**. Everything this file
adds follows from that one word: a cookie is a per-browser preference, and a wall is how a
control room is arranged. It could not be handed to the next shift, could not be opened on a
second monitor without being rebuilt by hand, and did not survive somebody clearing their
browser.

The three behaviours on top of "remember it properly" are what a guard station actually
needs — **cycle** through more cameras than fit the grid, **pop** an alerting camera onto the
visible page, and open the same wall on **another monitor** — and all three are properties of
the wall rather than of the browser showing it, which is why they are columns.

## Not per user

A control room's walls are shared furniture ("Perimeter", "Loading bays", "Night"). An
operator arriving for a shift needs the same wall the last one was watching, not their own
copy, and an administrator has to be able to fix the wall everybody is looking at.

## Rules the tests pin

| Rule | Why |
|------|-----|
| Name required and unique, compared case-insensitively | "Perimeter" and "perimeter" are two rows nobody can tell apart in the picker that chooses between them. Uniqueness is in the service, not a unique index, so a clash is a sentence rather than a driver error. |
| The uniqueness check excludes the row being saved | Otherwise a wall can never be edited without renaming it. |
| Grid must be one this server knows, and the refusal NAMES the set | The list is deliberately duplicated from the SPA's `liveViewLayouts`: the server has to refuse a grid it cannot describe rather than store it and render an empty screen. Drift fails loudly and in the right direction. |
| At most one wall is the default | "The default" with two answers is a screen that opens differently depending on which row the database hands back first. Enforced *after* the write, so a failure leaves the wall saved rather than half-applied. |
| Cycle 0 or 3–600s, pop 0 or 5–300s, **refused** not clamped | A wall silently cycling at 3s when the operator asked for 1s is a wall nobody can explain the behaviour of. 0 means off. |
| Camera order survives, duplicates are dropped | Order IS the arrangement; sorting it would rearrange somebody's wall on save. The same camera twice is a mis-click, and refusing the save loses the rest of the arrangement. |
| At most 64 cameras | Not storage: sixty-four live tiles is already more than a browser decodes comfortably. |

## Missing cameras are reported, not dropped

`WallView.MissingCameras` lists ids on the wall that no longer name a camera. A wall that
quietly renders five tiles where six were arranged tells an operator they are watching
everything, and the camera that went missing is the one nobody is watching.

It **fails quiet, not wrong**: when the camera list cannot be read at all, nothing is claimed
missing, because reporting every camera on every wall as deleted because one query failed is
a worse answer than saying nothing. And it encodes as `[]`, never `null` — same rule as the
export manifest's gap list.

## Why the camera list is a string

`Cameras` is comma-separated ids. A join table would need an order column and buy nothing:
nothing joins on it, nothing filters by it, and the whole value is read and written as one
unit every time. It is a display arrangement, not a relation.

## Related

- `apps/mymatasan/entities/wall_layout.go.md`
- `apps/mymatasan/apis/walls.go.md`
