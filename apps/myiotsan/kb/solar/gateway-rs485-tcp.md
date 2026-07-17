---
title: Choosing an RS485→TCP gateway
category: Connectivity
order: 50
---

# RS485→Modbus-TCP gateways

Many residential inverters and all cheap meters expose Modbus only over **RS485** (a two-wire
serial bus). myiotsan speaks Modbus **TCP**, so you put a small **RS485→TCP gateway** on the bus.
It bridges serial Modbus onto the LAN.

## Gateways that work

| Model | Approx price | Notes |
|---|---|---|
| **Waveshare RS485-to-ETH / RS485 to POE ETH** | $20–35 | Reliable, has a true Modbus-TCP mode |
| **USR-TCP232-304 / USR-DR302** | $25–40 | Industrial, DIN-rail |
| **Elfin EW11 / EW11A** | $15–25 | WiFi, tiny, popular in DIY; set to Modbus-TCP mode |
| **PUSR / HF5111B** | $20–40 | — |

Any gateway that offers a **"Modbus TCP" (protocol conversion) mode** will do. Avoid the very
cheapest "transparent serial" adapters that only tunnel raw bytes — see below.

## Wiring

- **A/D+ → A/D+, B/D− → B/D−.** If you read nothing, swap A and B (the labelling is
  inconsistent between vendors).
- Match the serial parameters the device expects — commonly **9600 8N1** (Deye, Growatt,
  SDM630). Some devices use 19200; check the device's KB page.
- Set the gateway's slave/unit handling to **pass through** the unit id you configure in
  myiotsan.
- On a multi-drop bus, terminate with 120 Ω at the far end and give each device a **unique
  slave id**.

## The one setting that matters most

Set the gateway to **true Modbus-TCP mode** (it builds the MBAP header). If it is left in
**transparent / RTU-over-TCP mode**, the device will appear reachable but every read fails,
because myiotsan sends Modbus-TCP framing and the gateway forwards Modbus-RTU framing. See
[Modbus TCP vs RTU-over-TCP](modbus-tcp-vs-rtu).

## In myiotsan

Create the device with:

- **Endpoint:** `gateway-IP:502`
- **Unit:** the device's RS485 slave id (e.g. `1`)
