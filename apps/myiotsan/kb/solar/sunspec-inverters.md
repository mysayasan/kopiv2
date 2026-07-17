---
title: SunSpec inverters (Fronius / SMA / SolarEdge)
category: Inverters
order: 10
---

# Fronius, SMA and SolarEdge — the SunSpec path

Profile: **`generic-sunspec-solar`** (one profile reads all of them).

Fronius, SMA and SolarEdge implement the **SunSpec** standard: the device describes its own
register layout, so myiotsan walks its model chain and discovers the datapoints. You do **not**
author a register map — you just point the profile at the device and it reads.

Set the myiotsan device **Endpoint** and **Unit** per the vendor notes below.

## Fronius (GEN24, Symo, Primo with Datamanager 2.0)

- **Endpoint:** `IP:502`. **Unit:** 1 (inverter), the meter is a later model in the chain.
- Enable **Modbus TCP** in the inverter web UI under *Communication → Modbus*, and choose the
  **SunSpec** register map (not the legacy "Fronius" map), **int + SF** data format.
- The meter's position in the model chain is **dynamic** — the SunSpec walker handles this
  automatically; do not assume a fixed meter address.
- Fronius exposes the standard SunSpec **immediate control (model 123)**, so curtailment is
  possible — but our generic profile ships read-only; add a control command only after reading
  [Verify control](verify-control).

## SMA (Sunny Boy / Sunny Tripower with Speedwire)

- **Endpoint:** `IP:502`. **Unit id: 126** (SMA's default Modbus unit id — not 1).
- Enable **TCP Modbus Server** in the inverter (Speedwire) settings.
- SMA supports SunSpec model 123 (`WMaxLimPct` + `WMaxLim_Ena`) for curtailment.

## SolarEdge (SE-series with the built-in Modbus)

- **Endpoint:** `IP:`**`1502`** — SolarEdge uses **port 1502**, not 502.
- Enable **Modbus TCP** via SolarEdge's SetApp/installer menu.
- **Single client, ~2-minute idle timeout** — if another logger holds the connection, myiotsan
  cannot read. Keep the poll interval modest.
- SolarEdge reads cleanly over SunSpec, **but its power control and battery are proprietary**
  (registers 0xF3xx / 0xE1xx), *not* SunSpec model 123 — the generic profile will not curtail a
  SolarEdge. That needs a custom register-map command; verify before use.
- Multi-inverter "Synergy" units offset their meter blocks (+50 for 2-unit, +70 for 3-unit) —
  the SunSpec walker follows the chain, so this is transparent.

## Which flow templates work

The monitoring/derived templates (self-consumption, self-sufficiency) and the overheat alert
work with any SunSpec device that reports the corresponding keys. Curtailment control is only
straightforward on Fronius/SMA (model 123); SolarEdge needs a proprietary command.
