# myiotsan

`myiotsan` is the suite's on-prem IoT device hub — "the NVR, but for sensors." It is the
fourth app in the suite, alongside `mymatasan` (camera NVR), `myseliasan` (fleet control
plane), and `myidsan` (identity/SSO). It is built as an appliance on the same runtime host as
`mymatasan`: a single binary, on-prem, air-gapped-capable, and adoptable into the `myseliasan`
fleet over the existing pairing/control channel.

**P0-P2 are shipped — this is the MVP.** The app boots, authenticates, ingests telemetry from
real devices over an embedded MQTT broker, evaluates alert rules against it, and raises alerts
into a unified notification feed. What remains is discovery/onboarding (P3), actuation (P4),
industrial protocols (P5) and fleet adoption (P6) — see `docs/MYIOTSAN_PLAN.md` for the full
roadmap and, in §8b, exactly what shipped and what was found by live-booting it.

## Connecting a device

1. **Provision it** — `POST /api/devices` with a `deviceKey` (the device's MQTT client id — for
   Zigbee2MQTT/Tasmota/Shelly conventions this is usually the device's own identifier), a
   `profileId` pointing at its device type, and optionally a `password`. Leave the password
   empty and the app **generates** one with real entropy and returns it exactly once in the
   response — nowhere else, ever. There is no default or shipped device password: a shared one
   would be a fleet-wide backdoor the moment it leaked.
2. **Point the device at the broker** — MQTT, plaintext, port **1883** (`mqtt.addr` in config,
   default `0.0.0.0:1883`). Client id = the device's `deviceKey`. Username/password = the
   `deviceKey`/generated password. The device's own inventory row **is** its credential record:
   a device not provisioned here cannot connect at all, and deleting it really does revoke
   access — there is no separate credential store to fall out of sync.
3. **Publish on the profile's topic** — a profile (see below) declares the topic template
   (`{deviceKey}` substituted) and payload shape the device is expected to speak. The shipped
   catalog covers the common Zigbee2MQTT/Tasmota/Shelly conventions out of the box.

## The device-type catalog (profiles)

A `DeviceProfile` declares what a device TYPE publishes: topic template, payload format
(JSON or raw), and its `TelemetryKey`s (name, unit, JSON path, deadband, heartbeat, plausible
range). Without this abstraction, onboarding a hundred identical door sensors means configuring
a hundred devices by hand; with it, the hundredth device is a name and a dropdown. Seven
built-in profiles ship and are re-seeded (idempotently — existing ones are never overwritten)
on every boot: door/window contact, PIR motion, temperature/humidity, smoke/heat detector,
water leak, power/energy meter, access-control reader. A site can author its own profile (or
copy and edit a builtin — builtins themselves cannot be deleted, only used or copied) via
`POST /api/profiles`.

## The deadband — why the database stays small

**An operator provisioning a profile must understand this.** Every telemetry key carries a
`deadband`: the smallest change worth writing a row for. A building sensor is near-static
almost all the time — a room sits at 21.4 degrees for an hour, a door stays closed all
night — so a reading is persisted only when it actually moves by more than the deadband, or
when a `heartbeatSeconds` interval elapses (so a flat line still proves the device is alive
rather than dead). This is not an optimization detail: it is *why the database stays small* at
all. Measured on a live appliance, 20 devices publishing ~30,000 samples in under a second
produced 540 written rows — 98.2% suppressed, zero dropped.

**Setting a chatty key's deadband to `0` is how an operator fills their disk.** `0` means "store
every sample" — correct for a door contact (every open/close transition matters, and there are
only a handful a day) and disastrous for a temperature probe reporting several times a second
(every flicker of sensor noise becomes a permanent row). `GET /api/devices/stats` exposes
`stored`/`suppressed`/`dropped` counters so a mistuned deadband is visible rather than a slow,
silent surprise — `dropped > 0` means the write queue is shedding load because the disk cannot
keep up.

Crucially, **the deadband is a storage decision, not a detection one**: alert rules are
evaluated on every decoded sample, including ones the deadband suppressed. A value sitting
steadily over a threshold without moving does not get a new row, but it still fires an alert —
gating rules behind the deadband would mean a steady overheat is never alerted on.

## Rules and alerts

An `IotRule` watches one device, every device sharing a `tag`, or every device reporting a key.
Conditions: `above`, `below`, `equals`, `delta`, `rate`, `stuck` (a frozen sensor — the one
failure every other condition calls "healthy"), `offline` (silence past a window, driven by a
1-minute sweep since a dead device never reports again to trigger anything itself). Debounce
(`consecutiveSamples`), hysteresis (stops flapping on a value hovering at the threshold), and a
cooldown that **survives a restart** (re-seeded from the alert log, not a rule-level column, so
a tag-scoped rule watching ten devices keeps ten independent cooldowns) are all first-class.
Alerts publish into the same unified notification feed (`GET /api/notifications`) as camera
alerts do in `mymatasan`, tagged `device.alert` so a subscriber can tell "somebody is in the
building" (both) from "the cold store is failing" (this only).

## Authentication

`myiotsan` reuses mymatasan's local-auth stack, extracted to `domain/shared` so both appliance
apps run the same security-critical code instead of two forks:

- DB-backed local users (`local_user` table, shared entity/service), bcrypt password hashing,
  a bcrypt-verification cache.
- **Session-cookie login** (`POST /api/auth/login`) as the primary sign-in path — unlike
  mymatasan, which authenticates by replaying an HTTP Basic header on every request (costing a
  bcrypt verification per request), myiotsan exchanges the credential once for a session
  cookie. HTTP Basic still works for API clients and scripts.
- A forced-password-change gate on first login (`must_change_password`), a failed-login
  lockout with escalating backoff (`loginSecurity` config), and a three-role permission
  matrix (`viewer`/`operator`/`admin`) that decides **every** request, not just writes —
  deny-by-default (`apps/myiotsan/services/rbac.go`).
- On first startup, the bootstrap admin account is seeded from `localAuth.username`/
  `localAuth.password` in config (or `LOCAL_ADMIN_PASSWORD` env, or a generated per-install
  password when neither is set) and is always must-change. The credential is revealed via a
  console banner and a `INITIAL_ADMIN_LOGIN.txt` recovery file written to the data dir —
  delete it after signing in.

## Role model

Three roles, drawing the same line mymatasan draws — **can this person destroy the record?**

- `viewer` — see devices and their current readings, and see that an alert fired. No access to
  historical telemetry.
- `operator` — + review telemetry history, acknowledge alerts. Cannot actuate a device, delete
  readings, or change rules and settings.
- `admin` — everything.

myiotsan draws a **second** line mymatasan does not need: actuation (writing to a device, e.g.
a relay) is admin-only, because a bad write to a physical device is dangerous in a way a bad
camera PTZ move is not. This lands with the command path in P4 and is not to be loosened
without a deliberate decision.

The authorization catalog now covers the shipped device/telemetry/rules/notification surface:
a viewer sees devices and their current readings and that an alert fired; only an operator can
review `/api/devices/*/readings` (the history) or acknowledge an alert — the same evidentiary
line mymatasan draws for its own alert log; creating/deleting devices, editing profiles, and
writing rules stay admin-only (`apps/myiotsan/services/rbac.go`).

## Configuration

- `apps/myiotsan/config.json` / `config.dev.json` — HTTPS port `3003` (myidsan `3001`,
  myseliasan `3002`, mymatasan `3000`), SQLite by default (`./data/myiotsan.db`).
- `apps/myiotsan/certs/` — dev TLS cert/key.
- `mqtt.enabled` / `mqtt.addr` — the embedded MQTT broker, default `0.0.0.0:1883` (**new
  port**, plaintext, LAN). A site running its own Mosquitto/EMQX can turn `enabled` off in a
  future connect mode; the ingest pipeline does not care where a payload came from.
- `telemetry_store` — `batchSize`/`flushMs`/`queueSize` tune the write-behind batcher;
  `rawRetentionDays` (default 30) / `rollupRetentionDays` (default 400) tune how long raw
  readings and their downsampled rollups survive.
- No new fleet ports: when myiotsan is later adopted into a myseliasan fleet (P6), it will
  dial the same discovery/pairing/control ports mymatasan nodes already use.

Run locally:

```bash
go run . -app myiotsan
```

## Frontend

The SPA (`apps/myiotsan/views/react-webpack/`) is built off the shared `@shared` frontend
module the same way myseliasan's is — no per-app copy of `DataTable`/`SideNav`/icons/i18n.

Screens: **Dashboard** (estate health, recent alerts, and the ingest panel), **Devices**
(inventory, live values, telemetry charts, provisioning), **Rules**, **Alerts**,
**Notifications**, and **Device types** (the profile catalog and its deadbands). All four
locales — en, ms, zh, ar.

Two things on the Dashboard deserve an operator's attention:

- The **ingest panel** states the suppression rate in plain English ("the deadband kept 98% of
  readings out of the database"). That ratio is the storage design working. If it ever falls
  toward zero, a deadband has been mistuned and the disk is about to be in trouble.
- **Dropped** readings get their own alarm block rather than a slot in a counter row. A
  non-zero, growing value is the one number that means ingest has outrun the disk and readings
  are being shed.

The alert log has **no delete button**, deliberately: acknowledging is an operator power,
erasing the record is not.
