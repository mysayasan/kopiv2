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

## Shipped

| Phase | Scanner | Transport | How verified |
|---|---|---|---|
| P1 | **Modbus** (`infra/iot/discover/modbus.go`) | TCP + RTU-over-TCP; SunSpec auto-ID, unidentified fallback | Live-boot against a SunSpec mock: scan → identify → candidate (endpoint/unit prefilled, generic-sunspec suggested) → adopt → device |
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
