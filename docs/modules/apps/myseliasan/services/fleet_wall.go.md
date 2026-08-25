# Module: apps/myseliasan/services/fleet_wall.go

## Purpose

Named, shared camera arrangements that **span appliances** (W3-3d) — the video wall a control
room needs, and the one no single recorder can offer.

## Why this exists when two other things look like it

| | What it is |
|---|---|
| mymatasan's wall (W3-3b) | Cameras on ONE recorder. Right for a recorder; a guard station covers four buildings. |
| myseliasan's Live Views | Already cross-node, and a **scratchpad**: per browser, unnamed, gone when somebody clears their site data, rebuilt by hand every shift. |
| **This** | Named, shared, saved on the control plane, cycling on its own, and pulling up whatever raised the alarm — in any building. |

The tile is `(appliance, camera)` rather than `(camera)`, and everything else in the file is a
consequence of that one change.

## The rules, and what each one is protecting against

- **A wall holds at most `fleetWallMaxTiles` (32)** — lower than the appliance's own 64. Every
  tile here is a stream relayed across the tunnel from a different machine, not a local decode.
  A wall nobody can watch is worse than one that refuses to be built.
- **The same camera twice is a mistake, not an arrangement.** It costs a second relayed stream
  of the same picture and wastes a tile at the moment the space matters. The same camera *id*
  on a different appliance is a different camera and is allowed — anything else would let
  camera numbering on one recorder silently constrain every other.
- **At most one default**, cleared on the others rather than left to row order.
- **Cycle and auto-pop bounds** match the appliance's, because an operator moving between the
  two screens should not have to learn two sets of limits.

## Resolution, and the two ways a tile can be wrong

`view` resolves each tile against the **live** fleet — the appliance's name and status are read
at read time, never stored on the row, so a renamed or newly-offline appliance is right without
anybody re-saving anything.

A tile can be wrong in two different ways and they get two different counts, because they send
somebody to two different places:

- **`OfflineTiles`** — the appliance is in the fleet and unreachable. The picture will not
  arrive.
- **`UnknownTiles`** — the appliance is not in this fleet at all (released, or never adopted).

**Neither is silently dropped.** A tile keeps its place in the arrangement and says what
happened to it: an operator who built a wall of sixteen and now sees fifteen has been told
nothing, and the missing one is the building they will not think to check.

## Auto-pop is the reason to run a wall here

An appliance can only pop a camera it owns. The control plane sees every node's alerts in one
feed, so a wall here can pull up the camera raising the alarm in a building the operator was
not watching — on a different machine from every other tile on the screen. That is the part an
appliance vendor cannot match at all.

The selection lives in the SPA (`components/fleet_wall.js`) because it is driven by the
notification stream the screen is already subscribed to; the server's job is the arrangement.

## `encodeTiles` / `decodeTiles`

The only two places that know how a tile list is stored (`nodeId:cameraId,…`). A node id is a
UUID, so it contains no colon or comma, which is what makes the encoding safe — and is why the
pair is here rather than inlined at four call sites that would each have to remember that.
`decodeTiles` drops anything malformed rather than producing a tile pointing somewhere; there
is a table-driven test for the junk.

## Not claimed

- **No proof the tiles PLAY.** The relay behind them is the existing cross-node WebRTC path.
  This owns the arrangement, not the video.
- **No capacity check.** Nothing stops a wall of 32 relayed streams from being more than a
  browser or a link can carry; the limit is a bound, not a measurement, and the screen says so
  where somebody is choosing.
- **No per-tile permissions.** A wall is visible to anyone who can read it, and it may name
  cameras on appliances the reader could not otherwise browse. Writing is superadmin-only for
  that reason — see `apis/fleet_wall_api.go.md`.
