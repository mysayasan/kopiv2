---
title: Verify control before you actuate
category: Safety
order: 5
---

# Verify control registers before you actuate

The shipped inverter profiles include control commands — curtailment, battery force-charge,
export limits. **They write to real hardware.** A wrong register, sign or scale does not throw an
error; it mis-drives your inverter. This page is the checklist to run before you turn actuation
on.

## Why this matters more for solar than for a relay

A relay is on or off. An inverter command carries a *value* whose meaning depends on firmware:

- **Sign** — is `batt_power` positive when charging or discharging? Sungrow and Deye disagree.
- **Scale** — Sungrow's forced-power setpoint is **watts on one model and percent on another**.
- **Enable sequence** — an export limit often does nothing until a separate "enable" register is
  set; a forced charge needs the EMS mode set to "Forced" first.

Community register maps are excellent but describe *a* firmware, not necessarily *yours*.

## The safety gates already in place

myiotsan does not let a flow write to a device casually. Every command — from a flow, scene or
the API — goes through one guarded path (`CommandService.Issue`) that enforces:

- **Read-only by default** — a device never actuates until you switch **Actuation** on for it.
- **Only declared commands**, with **server-side bounds** on the value.
- **Rate limiting**, **full audit** of every attempt (including refusals), and **never
  auto-retry** (a lost confirmation is never a second physical write).

The pre-declared commands are therefore safe to *ship* — nothing fires until you deliberately
enable it.

## The bench-verification checklist

Before enabling actuation on a production inverter:

1. **Test against a simulator or a spare unit first** if you can. `tools/sunspec-sim` in this
   repo honours writes and reads them back.
2. **Read the register back.** Issue the command with a small, safe value and confirm the device
   reflects it. myiotsan's Modbus writes already read back and only report *confirmed* when the
   value landed.
3. **Verify the sign.** Force a known charge and a known discharge; watch `batt_power` and record
   which is positive. If it is inverted from the profile's label, set that key's scale to −1.
4. **Verify the scale/unit.** Especially Sungrow `batt_force_power` (W vs %). Write 10, see what
   happens, before you ever write 5000.
5. **Verify the enable sequence.** Confirm the dependent "enable" register and mode are set, or
   your setpoint silently no-ops.
6. **Start conservative.** Wide bounds, gentle setpoints, and watch the audit log and the device
   for a few cycles before trusting a flow to do it unattended.

## Turning actuation on

1. Bench-verify per above.
2. On the device's **Settings**, enable **Actuation**.
3. Instantiate the paired flow template, bind the slot, and enable it.
4. Watch the **audit trail** (every command is recorded) for the first real writes.

If in doubt, leave the profile read-only. Monitoring is valuable on its own, and a wrong write
is not.
