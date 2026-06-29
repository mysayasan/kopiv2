# Module: infra/control/frame.go

## Purpose

Defines the `control` package and the wire vocabulary for the persistent, node-initiated control channel between the myseliasan control plane (parent) and an adopted mymatasan node.

## Responsibilities

- Document the channel as a thin transport: one long-lived WebSocket over the fleet-CA mTLS trust established at adoption, multiplexing JSON frames both ways.
- Declare `FrameType` and its values: `hello` (node identity + version), `event` (unsolicited node→parent push), `req` / `res` (parent→node tunneled HTTP request and its correlated reply).
- Define `Frame`, the single message struct, with fields grouped by frame type and `omitempty` so unused fields stay off the wire.

## Notes

- The dial direction is deliberately node→parent: a node behind NAT/firewall (and possibly re-IP'd) gets one consistent direction to secure, survives address changes, and gives the parent an instant liveness signal (socket drop = node lost).
- `Body` is `[]byte`, so `encoding/json` base64-encodes it — any payload (JSON, a snapshot image, form data, or empty) tunnels intact.
- The Phase-2 request/response fields (Method/Path/Role/Actor/Status) are already present, so the command tunnel needed no wire change; Phase 1 uses only Hello + Event plus WebSocket ping/pong keepalive.
