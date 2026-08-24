# Module: apps/mymatasan/entities/ptz_tour.go

## Purpose

Declares `PtzTour`: one guard tour (W3-5) — the ordered set of stored positions a PTZ camera
visits on its own, and how long it waits at each.

## Fields

| Field | Type | Notes |
|-------|------|-------|
| `Id` | int64 | Auto-increment primary key. |
| `CameraId` | int64 | The camera this tour patrols. |
| `Name` | string | Required, unique **per camera** (enforced in the service, case-insensitively, so a clash is a sentence rather than a driver error). |
| `Stops` | string | The ORDERED itinerary, `presetToken:dwellSeconds` per stop, comma-separated. A stop with dwell 0 uses `DwellSeconds`. |
| `DwellSeconds` | int | Default hold at a stop that does not name its own. 5–3600. |
| `IsRunning` | bool | Whether the patrol is walking. **Persisted**, see below. |
| `CreatedBy` / `CreatedName` / `CreatedAt` / `UpdatedAt` | int64/string | Who built it and when. |

## What is NOT here is the point

The positions themselves live on the **camera** — ONVIF presets, addressed by a token the
device issues. This table stores only the itinerary.

Mirroring the positions here would create a second answer to "where can this camera point",
and the two part company the first time somebody uses the camera's own web page, which is how
a large share of PTZ cameras in the field get set up. A tour therefore holds tokens it does
not own and has to cope with one having been deleted — the same shape as `WallLayout` holding
camera ids it does not own, and reported the same way (`PTZTourView.Stops[].Missing`).

A camera that cannot be *asked* what presets it has is a third state, distinct from both, and
is reported as `presetsUnavailable` rather than as every stop being missing: "your whole
patrol has been deleted" is a much worse thing to say than "the camera did not answer".

## Why `IsRunning` is a column

An appliance that reboots at 03:00 must come back doing what it was doing. A tour that stops
at every power cut is a tour an operator stops trusting, and nobody is awake to restart it.

Where the patrol had got to is deliberately **not** persisted: resuming a reboot mid-route
buys nothing, and the first tick after a restart sends the camera to stop 0, which is a
defined place rather than wherever the power cut left it.

## Encoding

A join table would need an order column and buy nothing — nothing queries a stop, nothing
joins on it, and the whole itinerary is read and written as one unit every time. A token
containing `:` or `,` is refused rather than encoded into something that decodes differently;
silent corruption of a patrol route is the failure this format could otherwise produce.

New table; the auto-migrator creates it, so no migration is needed. Removed by the
camera-deletion cascade (`ptzService.DeleteToursForCamera`).
