# Module: infra/pairing/replay.go

## Purpose

Implements an in-memory nonce cache that prevents replay of captured probes within the freshness window, complementing the timestamp-based staleness check in `packet.go`.

## Responsibilities

- `nonceCache` — mutex-protected `map[string]time.Time` with a configurable eviction window.
- `seenBefore(nonce)` — atomically record a nonce and report whether it was already seen. Evicts all entries older than the window on every call, bounding the map to one window's worth of traffic.
- Defaults to `DefaultReplayWindow` when constructed with a non-positive window.

## Notes

- The cache is owned by the `Responder` and is not shared across processes; each node restart starts with an empty cache, which is safe because a fresh nonce is generated per probe.
- Eviction is lazy (on each check call) rather than on a timer, which is sufficient for the low probe frequency expected on a LAN discovery channel.
