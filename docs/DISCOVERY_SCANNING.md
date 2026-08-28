# Discovery scanning — status & roadmap

myiotsan discovers devices two ways, both feeding the **same** quarantined candidate → adopt
pipeline (`DiscoveredDevice` + `/api/discovery/candidates/{id}/adopt`):

1. **Announce / enroll** (original): an admin opens a time-boxed, secret-gated window; a device
   connects to the embedded MQTT broker with a one-time key and is recorded as a candidate.
2. **Active network scan** (this feature): an admin sweeps the LAN and each found device becomes a
   candidate. A scan never adds a device — it only proposes candidates to adopt.

## Safety posture (all scanners)

Opt-in (admin-triggered), bounded (host cap `1024`, per-scan timeout, concurrency cap), **read-only**
(no scanner writes to a device), LAN-local (subnet sweep or link-local multicast, no cloud), and
audited to the notification feed. Nothing here broadcasts a write or a control command.

**LAN-local is enforced, not just stated** — `discover.Hosts` (the one funnel every sweep target
passes through, including the Modbus sweep's admin-typed CIDR) refuses anything outside private
address space (RFC 1918, RFC 6598 CGNAT, RFC 3927 link-local, IPv6 ULA/link-local), plus loopback,
unspecified and multicast addresses. This was a live-found gap, closed 2026-08-28: before the fix, a
sweep of `192.0.2.0/24` (public TEST-NET-1) or `127.0.0.1/32` (the appliance's own loopback) was
accepted and really ran, making the scan endpoint a general-purpose port scanner pointed wherever the
appliance had a route. A hostname target is resolved and every address it yields must qualify; an
unresolvable target is refused (fail closed), and a list with one bad target is refused whole. See
`tools/fleetbench/bench_iotsan_discovery.py` and `infra/iot/discover/discover_test.go`.

**An honest limit, not a defect**: the scan is synchronous and holds its HTTP request open for its
whole duration. Live-measured: 256 unreachable hosts cost 6.4s at the shipped 800ms/host and 32-way
concurrency, so the 1024-host cap is roughly 26s, and the full cap with all five scanners selected
measured 41.7s. That only completes because myiotsan's shipped `config.json` disables the write
timeout (`writeTimeoutSeconds: 0`); apphost's default of 30s when the key is absent would be
exceeded. The host cap and that timeout are two numbers that must be changed together.

## Shipped

| Phase | Scanner | Transport | How verified |
|---|---|---|---|
| P1 | **Modbus** (`infra/iot/discover/modbus.go`) | TCP + RTU-over-TCP; SunSpec auto-ID, unidentified fallback | Live-boot against a SunSpec mock: scan → identify → candidate (endpoint/unit prefilled, generic-sunspec suggested) → adopt → device |
| — | **Safety posture (all scanners)** | READ-ONLY / LAN-local / bounded / cancellable / proposes-never-adds / opt-in / audited | `tools/fleetbench/bench_iotsan_discovery.py`, 38/38 with the LAN-local fix (36/38 against unfixed main) — see the note above |
| P2 | **mDNS/DNS-SD** (`mdns.go`) | UDP 5353 multicast (`go.bug.st` n/a; uses `hashicorp/mdns`) | Runs in live-boot without error; finds Chromecast/HomeKit/Shelly/printers on a real LAN |
| P2 | **SSDP/UPnP** (`ssdp.go`) | UDP 1900 M-SEARCH | Runs in live-boot; header parser unit-tested |
| P3 | **EtherNet/IP** (`ethernetip.go`) | UDP 44818 ListIdentity broadcast | Identity-reply parser unit-tested (vendor id + product name) |
| P3 | **BACnet** (`bacnet.go`) | UDP 47808 Who-Is broadcast | I-Am parser unit-tested (device instance + vendor id) |

Frontend: onboarding lives under the **Devices** tab as an admin-only **Discover** sub-tab (beside
**Inventory** = the device list + manual add) — a **Scan** panel (per-protocol toggles + CIDR for
the Modbus sweep) alongside the enrollment window and the candidate list;
scan-sourced candidates show how they were found + their address, and adopting a Modbus one carries
its connection through so it polls immediately.

> **EtherNet/IP + BACnet are parser-proven, not hardware-proven** — verified against protocol-mock
> byte frames, not against a real PLC (none available in CI). The request/response encodings follow
> the CIP / BACnet specs; validate against your own hardware before relying on them in production.

## Deferred — and why (not silently dropped)

| Item | Why it is not built here |
|---|---|
| **OPC-UA discovery** (LDS / GetEndpoints) | Belongs with the OPC-UA *driver*, which is itself still on the roadmap (§8 MYIOTSAN_PLAN). Discovery without the driver to then read the device is half a feature; build them together. |
| **Profinet DCP** (Siemens) | Uses raw layer-2 Ethernet (DCP is not IP/UDP), which needs privileged raw sockets and per-OS packet capture — not portable to the single-binary, unprivileged posture, and not testable without a real PLC + NIC in promiscuous mode. |
| **Matter controller** | A real Matter controller is a very large spec — commissioning (PASE/CASE), operational certificates, a full cluster model, operational discovery. It is a multi-month effort in its own right, not a scanner. This is the honest path to controlling consumer devices (TVs, lamps) uniformly, but as its own epic. |
| **Native TV / AV control** | Each vendor (Samsung, LG, Sony, Roku, Cast) is a separate, largely reverse-engineered protocol. Discovery (mDNS/SSDP) *finds* these; driving them needs a per-vendor integration. Surfaced as "found, integration TBD" rather than pretending they are controllable. |

**Discovery ≠ control:** a scanner that *finds* a device (a TV, a PLC) does not mean the device is
drivable — that still needs a matching driver/profile. The families that already speak to us
(Modbus inverters/meters/PLCs; Shelly/Tasmota/Zigbee2MQTT over MQTT) are the ones where
"found → usable" actually closes today.
