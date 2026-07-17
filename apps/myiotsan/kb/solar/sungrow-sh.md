---
title: Sungrow SH (residential hybrid)
category: Inverters
order: 20
---

# Sungrow SH-series hybrid inverter

Profile: **`sungrow-sh-hybrid`** (register map, Modbus TCP).

Applies to the SH residential hybrids: **SH5.0RT / SH8.0RT / SH10RT** (3-phase),
**SH3.0RS / SH5.0RS / SH6.0RS / SH10RS** (single-phase), and the SH*K commercial units.

## Hardware you need

- The inverter's **WiNet-S** dongle (WiFi/LAN/Ethernet communication module) — or a direct
  RS485→TCP gateway on the inverter's COM port. The WiNet-S exposes Modbus TCP on the LAN.
- Modbus TCP: **port 502**, **unit id 1** (default).

> **WiNet-S is single-client.** It accepts roughly one Modbus master at a time and wants a
> **poll interval of ≥ 10 s**. If SolarInfo/iSolarCloud logging is also polling, you may see
> dropped reads. The profile ships with a 10 s poll for this reason.

## Enabling Modbus

1. In the WiNet-S / iSolarCloud local UI, enable **Modbus TCP** (sometimes under "Communication
   → Modbus").
2. Note the dongle's LAN IP. Set the myiotsan device **Endpoint** to `IP:502`, **Unit** `1`.

## What the profile reads

Sungrow keeps measurements in **input registers (Modbus fn 4)** and stores 32-bit values
**word-swapped** (low word first). The profile's telemetry keys already carry the correct
`Input register (fn 4)` flag and `Swap words` setting — you do not need to touch them. Register
numbers in the profile are the **on-the-wire (0-based) PDU addresses** (Sungrow's manual numbers
them 1-based; wire = manual − 1).

Highlights: `inv_ac_power`, `inv_dc_power` (total PV), `batt_soc`, `batt_power`, `grid_ac_power`,
`inv_temperature`, `inv_frequency`, daily/total yield and import/export energy.

### Sign conventions (verify on your unit)

- **`grid_ac_power`** (register 5600): **+ import, − export**.
- **`batt_power`** (register 5213): **+ discharging, − charging**. (Note this is the *opposite*
  of the Huawei profile, which is + charge / − discharge.) Older Sungrow firmware reported an
  unsigned magnitude here — if your value never goes negative, your firmware predates Oct 2024;
  use the running-state charging/discharging bits instead.

## Control (opt-in — read [Verify control](verify-control) first)

The profile pre-declares these holding-register writes. They do nothing until you enable
actuation on the device. **All of them must be bench-verified for your model.**

| Command | Register | Values / range | Notes |
|---|---|---|---|
| `ems_mode` | 13049 | 0 Self-consumption, 2 Forced, 3 External EMS, 4 VPP | Set **Forced (2)** before force-charging |
| `batt_force` | 13050 | 170 Charge, 187 Discharge, 204 Stop | 0xAA/0xBB/0xCC — verify the mapping |
| `batt_force_power` | 13051 | 0–5000 | **Unit is W on SH\*K but PERCENT on SH\*.0RT** — verify! |
| `export_limit` | 13073 | 0–30000 W | Inert until `export_limit_enable` = Enable |
| `export_limit_enable` | 13086 | 170 Enable, 85 Disable | 0xAA / 0x55 |
| `batt_min_soc` | 13058 | 0–50 % | Reserve floor |
| `batt_max_soc` | 13057 | 50–100 % | Reserve ceiling |

### The biggest footguns

- **`batt_force_power` unit** — on the SH*.0RT models it is a **percentage**, on SH*K it is
  **watts**. Ship nothing to it until you confirm which your model uses (write a small value,
  read it back, watch the battery).
- **Enable sequence** — the export limit at 13073 does nothing until you write
  `export_limit_enable` = 170. Forced charge/discharge needs `ems_mode` = 2 first.
- **Command mapping** — confirm 170 really charges (and does not discharge) before trusting it.

## Paired flow templates

- **Battery low-SoC guard + force-charge** — uses `batt_force`.
- **Grid export-limit control** — uses `export_limit`.
- **Inverter overheat & fault alert** — uses `inv_temperature` + `inv_operating_state`.

Register map cross-checked against the Sungrow "Communication Protocol of Residential Hybrid
Inverter" (V1.1.2) and the `mkaiser/Sungrow-SHx-Inverter-Modbus-Home-Assistant` community map.
