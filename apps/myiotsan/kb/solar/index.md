---
title: Solar systems — start here
category: Overview
order: 0
---

# Connecting a solar system to myiotsan

myiotsan reads (and, when you allow it, controls) solar inverters, batteries and meters
over **Modbus TCP**. This guide covers the shipped sample device profiles and the reusable
flow templates that pair with them.

## The two ways an inverter connects

1. **SunSpec-native (zero register mapping).** Fronius, SMA and SolarEdge speak the SunSpec
   standard. The single `generic-sunspec-solar` profile reads any of them by walking the
   device's model chain — no per-model register map. See
   [SunSpec inverters (Fronius / SMA / SolarEdge)](sunspec-inverters).

2. **Vendor register map.** Sungrow, Huawei, Deye/Sunsynk/Sol-Ark, Growatt, Solis, GoodWe and
   the like expose a flat vendor block with no SunSpec marker. Each needs an explicit profile
   that names which register holds which value. Shipped examples:
   - [Sungrow SH (residential hybrid)](sungrow-sh)
   - [Deye / Sunsynk / Sol-Ark hybrid](deye-hybrid)
   - Huawei SUN2000 (the `huawei-sun2000` builtin profile)

## Meters

When the inverter cannot see the grid connection point, add a dedicated meter:
[Eastron SDM630](eastron-sdm630).

## The connection reality (read before you buy anything)

- Many residential inverters expose Modbus only over **RS485** — you need an
  [RS485→TCP gateway](gateway-rs485-tcp).
- "Modbus TCP" and "Modbus RTU-over-TCP" are **not the same** and myiotsan speaks only the
  former. Getting this wrong means the device connects but never reads. See
  [Modbus TCP vs RTU-over-TCP](modbus-tcp-vs-rtu).

## Before you let a flow control anything

Every shipped inverter profile includes control commands (curtailment, battery), but they are
**inert** until you explicitly enable actuation on the device *and* verify the register.
**Sign and scale genuinely differ by firmware.** Read
[Verify control before you actuate](verify-control) first — a wrong write goes to real hardware.

## Flow templates

Under **Flows** you will find reusable **Solar** templates. Each references a device *slot*
(`$inverter`, `$meter`); instantiate a template, bind the slot to your adopted device, and
enable it:

| Template | What it does | Actuates? |
|---|---|---|
| Solar self-consumption (derived) | Persists on-site solar use (W) as a series | No |
| Self-sufficiency % (derived) | Persists solar share of consumption | No |
| Battery low-SoC guard + force-charge | Alerts at 15%, force-charges below 8% | Yes (opt-in) |
| Grid export-limit control | Clamps feed-in when it exceeds ~4.5 kW | Yes (opt-in) |
| Inverter overheat & fault alert | Alerts on high temp / abnormal state | No |
