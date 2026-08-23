# Module: apps/myseliasan/apis/replay_horizon_api.go

## Purpose

Registers the HTTP surface for the replay horizon monitor (flagship hardening plan W2-6, F-11) —
how a node's current disconnect stands against the window inside which its missed events can
still be replayed on reconnect. See `apps/myseliasan/services/replay_horizon.go.md` for the
monitor itself.

## Endpoints

`NewReplayHorizonApi(router, auth, monitor)` mounts on `/nodes`, behind `auth.Middleware`:

| Method | Path | Access | Notes |
|---|---|---|---|
| GET | `/api/nodes/replay-horizon` | Any authenticated session | Sweeps the fleet on request (`ReplayHorizonMonitor.Sweep`) and returns a `ReplayHorizonReport`: `{nodes: [...], checkedAt, approaching, lapsed}`. |

## Why it sweeps on request rather than serving the last background pass

The sweep is a node list plus arithmetic — no tunnel calls, nothing that touches an appliance —
so it is cheap enough to run on a page load, and an operator asking "is this node past the point
of no return yet" deserves the answer as of now rather than as of up to fifteen minutes ago (the
background `leaderTicker` cadence, `app.go.md`). The monitor's own transition dedup
(`ReplayHorizonMonitor.raise`) means refreshing the page cannot spam anyone with repeat warnings
regardless of how often this endpoint is polled.

## Access

Mounted under `/nodes` so the fleet grant every role already holds covers it: this is health
information (which nodes are quietly losing events), and hiding it helps nobody. No superadmin
gate, unlike the write side of fleet policy/rollout/rules — there is nothing to write here.

## Notes

- Registered in `apps/myseliasan/app/app.go`, immediately after the monitor and its `leaderTicker`
  sweep are wired, right before "Staged version rollout (W2-5, F-07)".
- Returns a `500` via `controllers.SendError` if `monitor` is `nil` (should not happen once wired)
  or if `Sweep` itself errors (a registry list failure).
