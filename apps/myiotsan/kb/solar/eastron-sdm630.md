---
title: Eastron SDM630 (3-phase meter)
category: Meters
order: 40
---

# Eastron SDM630 energy meter

Profile: **`eastron-sdm630-meter`** (register map, Modbus TCP, **read-only**).

The SDM630 is a cheap, ubiquitous DIN-rail 3-phase meter. Add it when the inverter cannot see
the grid connection point (no built-in CT, or you want an independent measurement). A meter has
nothing to actuate, so this profile ships no commands.

## Hardware you need

- An **SDM630 Modbus** variant (the "Modbus" model, not the pulse-only one).
- The meter is **RS485 only** → an [RS485→TCP gateway](gateway-rs485-tcp). RS485 default:
  **9600 8N1**, slave id configurable on the meter's display (commonly **1**).
- Set the myiotsan device **Endpoint** to `gateway-IP:502`, **Unit** = the meter's slave id.

Set the gateway to **true Modbus-TCP mode** — the SDM630 answers only on the standard
function codes ([details](modbus-tcp-vs-rtu)).

## What the profile reads

The SDM630 reports every measurement as an **IEEE-754 float32** in **input registers (fn 4)**,
**big-endian (no word swap)**. The profile uses register kind **`f32`** with the *Input register
(fn 4)* flag set. Register numbers are the on-the-wire (0-based) PDU addresses; Eastron's manual
numbers them 30001+ (wire = 3xxxx − 30001).

| Key | Register (wire) | Unit |
|---|---|---|
| `grid_ac_power` (total) | 52 | W |
| `grid_l1/l2/l3_power` | 12 / 14 / 16 | W |
| `grid_l1/l2/l3_voltage` | 0 / 2 / 4 | V |
| `grid_frequency` | 70 | Hz |
| `grid_power_factor` | 62 | — |
| `grid_energy_imported` | 72 | kWh |
| `grid_energy_exported` | 74 | kWh |

### Gotchas

- **Energy is native kWh** (scale 1). Do not multiply by 1000.
- **If you read ~1.4e-42 instead of ~230 V, the word order is wrong.** The SDM630 is big-endian
  (ABCD, no swap) — leave *Swap words* off.
- **Total power sign follows CT orientation.** The profile labels `grid_ac_power` as
  + import / − export, but if your clamp is reversed the sign flips. Confirm against a known
  import/export condition and, if reversed, flip the CT (or set the key's scale to −1).

## Using it with a flow template

Any template that reads `grid_ac_power` can bind its `$meter` (or `$inverter`) slot to the
SDM630 instead of the inverter — useful when the inverter's own grid reading is unavailable.

Register map confirmed verbatim against the Eastron SDM630 Modbus Protocol (V2) PDF.
