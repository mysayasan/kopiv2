# Module: apps/myseliasan/services/node_availability.go

## Purpose

Turns the node state history (`services/node_state_history.go.md`) into the number a
customer's contract is written in: availability per node, per site and per calendar month
(flagship hardening plan W2-2, F-08). Served by `GET /api/nodes/availability`
(`apis/nodes.go.md`) and rendered into the fleet health PDF (`services/reports.go.md`).

## The three decisions

Each exists because the easy alternative produces a number that is flattering rather than
true.

1. **Time we were not watching is NOT uptime.** A monitoring gap and the period before a
   node's history begins are both subtracted from the denominator and reported separately. A
   report that quietly credits its own downtime to the fleet is worse than no report.
2. **A node with no measured time is NOT 100% available.** It is "no data". Dividing by zero
   and calling the result perfect is how an appliance that has never once been seen ends up
   at the top of the fleet health table.
3. **Availability is FLOORED, never rounded.** A single second of downtime in a year is
   99.999997%, and rounding prints 100.00% — a breach rendered as a clean sheet. 98.999%
   reads as 98.99, not 99.00: an SLA written at 99% is either met or it is not, and rounding
   decides that question in the vendor's favour. The only input that prints 100.00% is zero
   downtime.

## `tallyAvailability(events, gaps, from, to)`

A pure function — no database, no clock — so the arithmetic is unit-testable in isolation.

Builds contiguous segments from the (ascending) events, with a leading empty-state segment
for the time before the first record. Clips each to the window, subtracts any overlap with
the merged gap intervals as `Unmonitored`, and files the remainder by state:

| State | Bucket | Why |
|---|---|---|
| `online` | `UpSeconds` | |
| `lost` | `DownSeconds` (+1 outage) | |
| `self-dropped` | `NotInFleetSeconds` | Out of the fleet: neither up nor down. Counting a decommissioned appliance as downtime lets one retired box bury a site's figure for a year. |
| anything else | `UnmonitoredSeconds` | A state written by a future build. Counting it as up would invent uptime out of a string nobody here can read. |

The four buckets **partition** the window — their sum is exactly `to-from`. Every tally test
asserts it (`assertPartitions`), because a classification bug that loses time instead of
misfiling it shifts every percentage and nothing else notices.

Gaps are merged before subtraction (`mergeIntervals`) so two overlapping gaps do not remove
the same second twice, which would take more time out of the window than the window contains.

`LongestOutageSeconds` is reported alongside the total because two hours in one go and two
hours in ninety pieces are the same availability and very different operational facts.

## Rollups

Site and month rollups aggregate **node-seconds**, not the mean of per-node percentages, so a
node adopted yesterday cannot weigh as heavily on a site's monthly figure as one that ran all
month.

Site attribution follows the node's **current** building. A node that moved sites mid-window
has its whole history counted where it lives now; the alternative (stamping the site onto
every event) makes a past report un-correctable when somebody discovers the node was filed
under the wrong building all along. Nodes with no building form an explicitly named
"Unassigned" group rather than vanishing.

`monthBuckets` splits the window on calendar boundaries in **UTC** — not the operator's
timezone. The boundary this report is compared against is a contractual month, the figure has
to be reproducible by two people in different offices, and a browser-supplied offset would
make the same window yield two different answers. First and last buckets are partial when the
window does not start on the first, and carry their real from/to so a reader is not invited to
compare a 9-day bucket with a 31-day one.

## Sorting

Worst first — a report is read from the top, and the top is where the problem should be. A
node with **no data sorts below** any node with real downtime: an absent record is a gap in
our knowledge, not the worst appliance in the fleet.

## Reading the output

`HasData` must be read BEFORE `Availability`. `Availability` is 0 when `HasData` is false, and
that means "unknown", not "never up". `formatAvailability` renders that case as the words "no
data" rather than "0.00%" — the two mean opposite things and a percent sign on the first is a
lie a reader has no way to detect.

`Coverage` is the share of the window that was measured at all. An availability of 100 over a
coverage of 4 is not a good month, and the PDF always prints them together.

`MonitorGapSeconds` / `MonitorGaps` are surfaced separately because they are the one figure in
this report that is about us rather than about the fleet.

## Related

- `services/node_state_history.go.md` — where the events come from.
- `services/reports.go.md` — the Availability section of the fleet health PDF.
- `apis/nodes.go.md` — `GET /api/nodes/availability`.
