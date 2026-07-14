# Module: infra/pairing/prober.go

## Purpose

Implements the control-plane side of LAN discovery: broadcasts a signed probe to the fleet multicast group and collects authenticated announces from unpaired nodes.

## Responsibilities

- `ProbeResult` — a discovered, signature-verified node: NodeID, Name, Version, IP, HTTPSPort, and the advisory `Kind` hint copied straight through from the announce (see `docs/modules/infra/pairing/packet.go.md` "`Announce.Kind`" — safe to render, unsafe to trust for anything else).
- `Discover(ctx, fleetKey, multicastAddr, timeout)` — opens a unicast UDP socket (ephemeral port) for replies, then sends the signed probe out **every** multicast-capable interface via `golang.org/x/net/ipv4` (`SetMulticastInterface` + `WriteTo`) with `SetMulticastLoopback(true)` so a node on the same host hears it; falls back to a single default-route send if no interface send succeeds. Reads announces until the deadline or `ctx` cancellation. Only announces that echo the probe's nonce and carry a valid HMAC for `fleetKey` are accepted. Results are deduplicated by NodeID and sorted for deterministic output.

## Notes

- Requires a non-empty `fleetKey`; returns an error immediately if none is provided.
- Default timeout is 4 seconds when `timeout ≤ 0`. The effective deadline is the earlier of `time.Now()+timeout` and `ctx.Deadline()`.
- The prober does not join the multicast group (it is a sender/receiver from a unicast ephemeral port), which is why the socket is opened with `net.ListenUDP("udp4", &net.UDPAddr{})`.
- Sending out every interface (rather than relying on the OS default route) lets a multi-homed control-plane host reach nodes regardless of which adapter routes to them, and makes same-host dev work via loopback. Duplicate probes are harmless: the responder de-duplicates by nonce and `Discover` de-duplicates announces by NodeID.
- Stale, malformed, or wrong-HMAC announces are silently dropped; no error is returned for individual bad packets.
