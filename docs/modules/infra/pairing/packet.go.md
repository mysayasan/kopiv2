# Module: infra/pairing/packet.go

## Purpose

Defines the on-wire packet types, HMAC-SHA256 signing helpers, and exported assertion utilities for the LAN discovery + adoption protocol shared by `mymatasan` (node responder) and `myseliasan` (control-plane prober).

## Responsibilities

- Define `Probe` (control-plane → node) and `Announce` (node → control-plane) JSON wire types, each carrying a type tag, nonce, HMAC, and Unix timestamp.
- Define `AnnounceInfo` as the live node-identity struct (NodeID, Name, Version, HTTPSPort, Kind) stamped into each announce.
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

## `Announce.Kind` / `AnnounceInfo.Kind` — advisory, deliberately unsigned

`Kind` (`"camera"` | `"iot"`, `json:"kind,omitempty"`) is what a node claims to be, carried in
the announce for display purposes only (an icon in `myseliasan`'s discovery/scan list). It is
**deliberately excluded from the HMAC's signing parts** (`Announce.signingParts()` is
unchanged by this field). Two reasons:

1. **Compatibility.** Adding a field to the signature would break every `mymatasan` node
   already in the field the moment a control plane is upgraded — the node signs without it,
   the parent verifies with it, and the whole fleet would silently stop being discoverable.
2. **It doesn't need to be trusted here.** This value only ever decides an icon. The
   AUTHORITATIVE kind travels a completely different, already-trusted path — the node's own
   answer over the adopt call (`fleetnode.AdoptResult.Kind`), which is itself fleet-key-signed
   and gated by a claim code the operator read off the node — and that is the value
   `myseliasan` actually stores (`ManagedNode.Kind`; see
   `docs/modules/apps/myseliasan/entities/managed_node.go.md`).

So: a hostile actor on the LAN can make a fake node look like a sensor hub (or a camera) in a
scan list. They cannot make the control plane adopt it, and they cannot change what an already
adopted node is. An empty `Kind` means camera — every node that predates this field is a
`mymatasan` node.
