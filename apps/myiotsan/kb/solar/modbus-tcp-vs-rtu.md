---
title: Modbus TCP vs RTU-over-TCP
category: Connectivity
order: 51
---

# Modbus TCP vs Modbus RTU-over-TCP

This is the single most common reason a Modbus device "connects but reads nothing." myiotsan
speaks **Modbus TCP**. Some gateways and inverter dongles speak **Modbus RTU-over-TCP** instead.
They are not interchangeable.

## The difference

- **Modbus TCP** wraps each request in an **MBAP header** (transaction id, protocol id, length,
  unit id) and has **no CRC** — TCP already guarantees delivery. This is what myiotsan sends.
- **Modbus RTU-over-TCP** (a.k.a. "transparent" mode) sends the **raw serial RTU frame** —
  address + function + data + a **CRC-16** — straight over the socket, with **no MBAP header**.

A device expecting one framing will silently reject the other. There is no error dialog; reads
simply time out or return garbage.

## How to tell which you have

- If a gateway's menu has a **"Modbus TCP" / "Protocol conversion"** option, selecting it gives
  you real Modbus TCP → works with myiotsan.
- If it only offers **"transparent" / "TCP server"** with no protocol conversion, it is doing
  RTU-over-TCP → **not** compatible directly.
- Inverter WiFi sticks that use a proprietary wrapper (Solarman on **port 8899**, Growatt
  ShineWiFi) are neither — use an RS485→TCP gateway instead.

## Fixing it

1. In the gateway's web UI, set the working mode to **Modbus TCP** (protocol conversion), not
   transparent.
2. If your gateway genuinely cannot do MBAP mode, replace it with one that can (see
   [Choosing a gateway](gateway-rs485-tcp)).

## Quick sanity check

Point myiotsan at the device and watch the readings. If values are all zero/absent while the
endpoint is reachable, suspect the framing before you suspect the register map.
