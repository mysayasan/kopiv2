# Module: infra/pairing/packet.go

## Purpose

Defines the on-wire packet types, HMAC-SHA256 signing helpers, and exported assertion utilities for the LAN discovery + adoption protocol shared by `mymatasan` (node responder) and `myseliasan` (control-plane prober).

## Responsibilities

- Define `Probe` (control-plane → node) and `Announce` (node → control-plane) JSON wire types, each carrying a type tag, nonce, HMAC, and Unix timestamp.
- Define `AnnounceInfo` as the live node-identity struct (NodeID, Name, Version, HTTPSPort) stamped into each announce.
- `NewProbe(key)` — build a freshly-signed discovery probe (random nonce + timestamp + HMAC).
- `NewAnnounce(key, probeNonce, info)` — build a signed announce that echoes the given probe nonce.
- `ParseProbe(data, key, window)` — stateless decode + HMAC verify + freshness check; nonce-replay is the responder's job.
- `ParseAnnounce(data, key, window, expectNonce)` — decode + nonce-echo check + HMAC verify + freshness check.
- `SignAssertion(key, parts...)` / `VerifyAssertion(key, gotHex, parts...)` — the HMAC pair used to authenticate the HTTPS adoption call without transmitting the fleet key.
- `AssertionFresh(ts, window)` — freshness guard for adoption assertion timestamps.

## Constants

| Constant | Value | Notes |
|---|---|---|
| `ProbeType` | `"mymatasan.pairing.discover"` | Type tag on the wire to reject strays before any crypto. |
| `AnnounceType` | `"mymatasan.pairing.announce"` | Type tag on the wire. |
| `DefaultMulticastAddr` | `"239.255.90.21:49531"` | Administratively-scoped IPv4 multicast; stays on the local subnet. |
| `DefaultReplayWindow` | `30s` | Maximum packet age before a probe or announce is rejected as stale. |

## Trust Model

All probes and announces are HMAC-signed with the shared fleet key using HMAC-SHA256. Fields are concatenated with a `NUL` byte separator to prevent field-boundary ambiguity. Both the probe and the announce carry a fresh nonce and timestamp so a captured packet replayed within the freshness window can be rejected by the nonce cache in `replay.go`.

The adoption HTTPS call uses `SignAssertion`/`VerifyAssertion` with `(parentId, nonce, ts)` as the signing inputs. This proves the caller holds the fleet key without ever transmitting it.
