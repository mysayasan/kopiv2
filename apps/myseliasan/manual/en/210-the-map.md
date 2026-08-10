---
title: The fleet map
category: map
categoryLabel: Map & sites
summary: Where your appliances actually are — and how the map works with no internet.
order: 210
---

# The fleet map

**Map** shows your fleet as places rather than rows: every node at the site it stands in, coloured
by how it is doing.

A list tells you a node is lost. A map tells you *which building is now unwatched*, which is the
question somebody actually asks during an incident.

## Placing a node {#placing}

A newly adopted node starts in **Unplaced nodes**. Drag it onto the map to place it.

Until you do, it is a row in a list rather than a place — which is why adoption is not really
finished until the node is on the map.

Where nodes sit close together they collect into a cluster. **Zoom in** to move a single node out
of one; at low zoom you would be dragging the group, not the appliance.

## Reading the markers {#legend}

| Colour | Meaning |
|---|---|
| **Online** | Connected and reporting. |
| **Cert expiring** | Still working, but its certificate is running out. |
| **Lost** | Not connected. |
| **Idle** | Adopted, nothing to report. |

"Cert expiring" earns its own colour because it is the failure you can still prevent — see
[Managing a node](managing-nodes#certificate).

## The basemap, and working with no internet {#basemap}

The map has two independent parts: **your data** (nodes, buildings, placements) and the **basemap**
(the streets and terrain underneath).

Your data always works. The basemap is a **PMTiles** file the control plane serves itself, and
without one the map says *no offline basemap installed — showing node positions only*. That is a
degraded map, not a broken one: markers, buildings and floor plans all still work, you simply have
no streets behind them.

This split is the whole design. An air-gapped control plane cannot fetch map tiles from the
internet on demand the way a web map does, so the tiles have to be **on the appliance**.

## Downloading a region {#region-download}

Where the control plane does have internet access, you can extract a region into the local basemap:
set a remote PMTiles source URL once, then download the area you are looking at.

Two things to know before you try:

- **Downloading reaches that URL over the internet.** On a site that is meant to have no egress,
  this is exactly the thing not to do — install a prepared basemap file instead.
- **The `pmtiles` tool has to be installed on the server.** If it is missing, the page says so and
  downloads fail until it is.

If the download refuses because the area is too large, zoom in and take it in pieces.

## From the map into a building {#indoor}

A placed building opens its **floor plans**, and from there you are looking at cameras pinned to
rooms rather than pins on a street.

That is the drill-down worth learning: site → building → floor → camera, which is how somebody
describes a location out loud, and now also how you navigate to it. See
[Buildings and floor plans](buildings-and-floors).

## Sites {#sites}

A site groups everything at one location. Filter the map by site to work on one place at a time,
and use sites to keep a multi-location fleet legible — reports can be produced per site for the
same reason.
