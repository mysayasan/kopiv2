# Module: infra/pairing/responder.go

## Purpose

Implements the node-side UDP multicast listener that answers authenticated discovery probes from the control plane while the node is discoverable (unpaired and fleet-key-set), and goes silent once adopted.

## Responsibilities

- `ResponderConfig` — live-read function fields: `FleetKey()`, `Discoverable()`, `AnnounceInfo()`, `MulticastAddr`, `ReplayWindow`, `Logf`.
- `NewResponder(cfg)` — fills defaults (multicast address, replay window) and builds the nonce cache.
- `Run(ctx)` — blocking loop: binds `0.0.0.0:port` (so unicast announce replies carry a valid source address), joins the multicast group on **every** multicast-capable interface via `golang.org/x/net/ipv4` (`JoinGroup`) with `SetMulticastLoopback(true)`, reads UDP datagrams, calls `reply()` for each, and writes the announce back to the source address. Closes the socket when `ctx` is cancelled. Logs `pairing responder listening on :PORT (group …, joined N interface(s))` and `answered probe from <addr>`.
- `multicastInterfaces()` — returns the up, multicast-capable interfaces to join on (shared with the prober).
- `reply(probeData)` — pure logic: returns `(announce, true)` only when the node is discoverable, the fleet key is set, the probe HMAC and freshness pass, and the nonce has not been seen before. All rejection paths are silent (no response, no error logged to the network) so a scanner without the fleet key learns nothing about the node.

## Silence Guarantees

Every rejection path (paired, no fleet key, wrong HMAC, stale timestamp, replayed nonce) produces no network reply and no logged error on the wire. Diagnostic logs via `Logf` are local only and only emitted when the caller sets that function.

## Notes

- The fleet key is read live on every probe via `FleetKey()`, so setting or rotating the key through the API takes effect without restarting the responder.
- Discoverability is read live via `Discoverable()`, so a node that just accepted an adopt call goes silent on the next probe without a restart.
- A `1s` read deadline keeps the loop responsive to context cancellation even if the socket close races with `ReadFromUDP`.
- Joining on **all** interfaces (rather than the single OS-default interface `ListenMulticastUDP(nil)` picks) is what makes discovery work on multi-homed hosts — common on Windows with Hyper-V/WSL/VPN virtual adapters — and on same-host dev (via multicast loopback). Cross-platform (Linux/macOS/Windows). Operationally, the host firewall must allow inbound UDP on the discovery port, and Docker's default bridge does not pass multicast (use host networking or the manual-IP adopt path).
