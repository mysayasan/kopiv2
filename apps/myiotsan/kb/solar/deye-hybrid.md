---
title: Deye / Sunsynk / Sol-Ark hybrid
category: Inverters
order: 30
---

# Deye / Sunsynk / Sol-Ark hybrid inverter

Profile: **`deye-hybrid`** (register map, Modbus TCP).

Deye is the OEM behind a large slice of the budget hybrid market. **Sunsynk and Sol-Ark are
rebadged Deye units** and answer the same register map (~99% identical), so this one profile
covers all three brands.

- **Single-phase** SG01/02/03 LP1 and **split-phase** Sol-Ark 8K/12K/15K (120/240 V) use the
  low-address map this profile ships.
- **Three-phase** SG04LP3 uses a *different, higher* register block — this profile does not
  cover it out of the box (copy it and shift the addresses per the SG04LP3 map).

## Hardware you need

Deye inverters do **not** expose native Modbus TCP. You have two options:

1. **RS485→TCP gateway** (recommended) on the inverter's **BATTERY/BMS RJ45 port** — *not* the
   port labelled "Modbus", which is unreliable on many firmwares. RS485 params: **9600 8N1**,
   **slave/unit id 1**. See [RS485→TCP gateway](gateway-rs485-tcp), and be sure to set the
   gateway to **true Modbus-TCP mode** ([why](modbus-tcp-vs-rtu)).
   - Battery-port pinout: Pin1 = RS485-B, Pin2 = RS485-A, Pin3 = GND.
2. The Solarman WiFi stick speaks a proprietary wrapper on port 8899 — **not** standard Modbus
   TCP, and not supported directly. Use a gateway instead.

Set the myiotsan device **Endpoint** to `gateway-IP:502`, **Unit** `1`.

## Register convention — verify first

Two numbering conventions circulate for Deye ("A" and "B"). This profile ships **Convention A**
(work-mode 142, grid power 169, battery SoC 184 …), which matches most tooling. **Confirm your
unit answers Convention A** by reading one known register (e.g. work mode) and checking the value
is sane. If not, your firmware uses Convention B (add 102 to the control registers).

## What the profile reads

All Deye registers are **holding registers** (read fn 3 / write fn 6) — no input-register or
word-swap settings needed. Highlights: `inv_ac_power`, `grid_ac_power`, `load_power`,
`inv_pv1_power` / `inv_pv2_power`, `batt_soc`, `batt_power`, daily yield & grid import/export.

### Sign conventions (verify on your unit)

- **`grid_ac_power`** (169): **+ import, − export**.
- **`batt_power`** (190): **+ discharging, − charging**. Undocumented in Deye's own material —
  read it while the battery is knowingly charging and confirm the sign before trusting it.

## Control (opt-in — read [Verify control](verify-control) first)

| Command | Register | Values / range | Notes |
|---|---|---|---|
| `work_mode` | 142 | 0 Selling First, 1 Zero-Export-To-Load, 2 Zero-Export-To-CT | |
| `solar_sell` | 145 | 0 / 1 | Enable before an export limit matters |
| `grid_charge` | 130 | 0 / 1 | Allow charging the battery from the grid |
| `max_sell_power` | 143 | 0–15000 W | Register 143 semantics vary by firmware — verify |

### Using the shipped flow templates with a Deye

- **Battery low-SoC guard + force-charge** — the template targets Sungrow's `batt_force`.
  For a Deye, edit the command node to `grid_charge` = 1 (and set a charge current if your
  firmware needs one).
- **Grid export-limit control** — the template targets `export_limit`. For a Deye, retarget the
  command node to **`max_sell_power`**.

Register map cross-checked against `githubDante/deye-controller`, `kbialek/deye-inverter-mqtt`
and `kellerza/sunsynk`.
