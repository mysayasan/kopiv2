# Module: apps/myiotsan/entities/iot_device.go

## Purpose

Defines the persisted device inventory record — myiotsan's `camera`. One row per physical
sensor or actuator.

## Fields

- Identity: `Id`, `Name`, `DeviceKey` (unique — the device's MQTT client id and the variable
  segment of its topic; what makes the inventory record ALSO the credential record — a device
  not in this table cannot connect to the broker, see `infra/iot/mqtt`).
- Wiring: `Protocol` (`"mqtt"` or `"http"`), `ProfileId` (points at the `DeviceProfile` that
  declares this device's telemetry keys — without one, a device can connect but nothing it
  publishes can be decoded, indexed via `idx:"profile"`).
- **`Endpoint`/`Unit` (P5)** — address a POLLED device, one whose profile has `Transport ==
  "modbus"`. A Modbus device does not dial into the broker; the app dials OUT to it, so unlike an
  MQTT device its network location is its own per-instance property (two identical inverters
  share a profile but not an IP). `Endpoint` is `"host:port"` for Modbus TCP/RTU-over-TCP, or a
  serial port name (`COM3`, `/dev/ttyUSB0`) for a serial device; `Unit` is the Modbus unit/slave
  id. Both are empty/zero for an MQTT device, which is addressed by its `DeviceKey` instead. Read
  by `services.ModbusPoller` (`modbus_poller.go.md`) to build each device's `modbus.DeviceConf`.
- **`Transport`/`Baud`/`Parity`/`DataBits`/`StopBits`** — HOW the app reaches a Modbus device,
  independent of the profile: `""`/`"tcp"` (Modbus TCP/MBAP, the default), `"rtutcp"` (RTU frames
  over a plain TCP socket — a transparent RS485→TCP gateway), or `"serial"` (RTU over a serial
  line). Only meaningful for a Modbus device; ignored otherwise. For `"serial"`, `Endpoint` holds
  the port name instead of `host:port`, and `Baud`/`Parity`/`DataBits`/`StopBits` set the line
  (all zero-valued defaults to 9600 8N1 — see `infra/iot/modbus.SerialParams`). Mapped to the
  driver's `modbus.Transport` by `services.modbusTransportOf` (`modbus_poller.go.md`), used by
  both `services.ModbusPoller` (polling) and `services.CommandService.sendModbus` (a guarded
  write, `commands.go.md`), so a poll and a control write on the same device always agree on how
  to reach it.
- Credential: `PasswordHash` (bcrypt, `json:"-"`, never returned).
- Grouping/metadata: `Tag` (indexed; groups devices — "floor-2", "cold-store" — so a rule can
  scope to a set rather than naming each device, the replacement for mymatasan's per-camera
  zone polygon), `Location`, `Vendor`, `Model`, `Serial`, `Firmware`, `Enabled`.
- `ActuationEnabled` — the per-device capability toggle. Devices are read-only by default; a
  command is refused unless this is explicitly on, on top of the admin-only RBAC rule. A bad
  relay write is physically dangerous in a way a bad PTZ move is not, so it takes two
  deliberate acts to enable one. Adoption (`services/enrollment.go`) never sets it. The command
  path itself is `services.CommandService` (`services/commands.go.md`, shipped P4).
- Liveness: `LastSeenAt` (unix seconds, updated on every publish but NOT through the deadbanded
  reading path — a device faithfully reporting an unchanged value must still look alive to the
  offline detector), `Health` (`"online"`/`"offline"`/`"unknown"`).
- Audit fields: created/updated user and timestamps.

## Notes

- Bootstrap creates this table from the registered entity when SQLite or another supported DB
  engine starts.
- `services.DeviceService` (`apps/myiotsan/services/device.go.md`) owns CRUD, credential
  provisioning/rotation, and is also the broker's `mqtt.Authenticator`.
