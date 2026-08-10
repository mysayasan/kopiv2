---
title: Buildings and floor plans
category: map
categoryLabel: Map & sites
summary: Draw where your cameras actually are, so an alert names a room instead of a device.
order: 220
---

# Buildings and floor plans

A camera called `cam-07` tells you nothing at 3am. A camera pinned to *Warehouse A → Ground floor →
loading bay* tells you where to send somebody.

That is what this is for.

## Three kinds of thing you can add {#kinds}

| Kind | For | What you get |
|---|---|---|
| **Building** | Head office, warehouse | Floors and rooms to draw |
| **Outdoor area** | Park, yard, car park | One ground plan to draw |
| **Point asset** | Junction, gate, pole | A place to attach an appliance, nothing to draw |

Pick by whether there is an inside to draw. A pole with two cameras on it is a **point asset** — it
has a location and no floor plan, and forcing it to be a building only creates an empty room.

## Areas {#areas}

A building can be one area or several — floors, wings, rooms.

You are asked this when you create it, and it is not a trap: adding areas later is a normal thing
to do. "One area" is right for a single space; use several when people would name the parts out
loud ("second floor", "kitchen"). Areas can be renamed by double-clicking, and added or removed
whenever the building changes.

Name areas the way your staff say them. The name's whole job is to be recognised in an alert.

## Placing it, then drawing it {#placing}

Creating an asset does not place it. **Create & place on map** creates it and then waits for you to
click its spot on the map, which is what gives it a real location.

After that, **Edit plan** opens the area's canvas. You can draw the layout directly, or **upload a
plan image** — an architect's drawing, a screenshot, a photo of the fire-escape diagram on the wall.

Uploading replaces the canvas picture and **keeps your walls and cameras**, and removing the plan
image clears the picture back to a blank canvas while again keeping walls and cameras. So a better
floor plan turning up later is not a reason to redo your work.

Changes save as you make them; there is no save button to forget.

## Pinning cameras {#pinning}

The palette lists the cameras of your adopted appliances. Click one, then click its spot on the
plan — or drag it across.

**A camera has exactly one pin, and cannot be placed twice.** It is in one physical place, so it
belongs to one area; trying to place the same camera a second time tells you where it already is
and asks you to remove it there first. A camera cannot be in two rooms at once.

This is a real constraint rather than a UI restriction: two pins for one camera would mean the
system believes a device is in two rooms, and every location-based answer built on top of it — the
map, the reports, an alert naming a room — would quietly be wrong.

## The 3D layout {#three-d}

**3D layout** lets you paint wall and floor cells over the plan; the 3D view then raises those
walls automatically.

It is an optional flourish and it is genuinely useful for one thing: showing somebody who does not
read floor plans where a camera looks. Skip it entirely if a flat plan already communicates.

## Deleting {#deleting}

Deleting an asset removes **its floor plans and its camera placements** with it, and cannot be
undone.

The cameras themselves are untouched — they live on their appliance, not here. What you lose is the
knowledge of *where they are*, which has to be redrawn and re-pinned by hand. Rename rather than
delete when a building is merely repurposed.
