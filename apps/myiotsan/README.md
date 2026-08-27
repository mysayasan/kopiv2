# myiotsan

`myiotsan` is the suite's on-prem IoT device hub — "the NVR, but for sensors." It is the
fourth app in the suite, alongside `mymatasan` (camera NVR), `myseliasan` (fleet control
plane), and `myidsan` (identity/SSO). It is built as an appliance on the same runtime host as
`mymatasan`: a single binary, on-prem, air-gapped-capable, and adoptable into the `myseliasan`
fleet over the existing pairing/control channel.

**P0-P4, P6 and P7 are shipped.** The app boots, authenticates, ingests telemetry from real devices
over an embedded MQTT broker, evaluates alert rules against it, raises alerts into a unified
notification feed, (P3) onboards unknown devices through a time-boxed enrollment window instead
of requiring every device to be typed in by hand, (P4) can command an actuation-enabled
device — read-only by default, admin-only, only what the device's own profile declares,
server-side bounds, rate-limited, fully audited, and never auto-retried — (P6) is now an
adoptable node in the `myseliasan` fleet, which is what lets its alerts be correlated against a
`mymatasan` camera's (see "Fleet" below), and (P7) ships as an installable product with its own
release pipeline, and can now be managed remotely from `myseliasan`'s fleet UI the way an adopted
camera node already is (see "Install & release" and "Fleet" below). **Industrial protocols (P5)
have now partially landed**: the Modbus/SunSpec **driver foundation** (`infra/iot/modbus`,
`infra/iot/sunspec`, plus a standalone simulator at `tools/sunspec-sim`) is now **wired into this
app** — a device profile can declare `Transport: "modbus"` (self-describing SunSpec discovery, or
an explicit vendor register map), a `services.ModbusPoller` dials out to every such device on its
own poll cadence, and five built-in profiles ship: `generic-sunspec-solar` (reads any compliant
inverter/meter/battery, no per-model work), `huawei-sun2000` (the register-map worked example for
the world's most-installed inverter), and, for the budget/high-volume hardware most installers
outside that world actually buy, `sungrow-sh-hybrid`, `deye-hybrid` (covering Deye/Sunsynk/Sol-Ark
rebadges), and the read-only `eastron-sdm630-meter` — the last needing two driver additions
(reading Modbus INPUT registers, fn 4, and decoding IEEE-754 float32 values) alongside the
original holding-register/integer path. **Guarded Modbus control writes have also landed**: a
device profile can declare a command as a Modbus holding-register write instead of an MQTT
publish, and it is commanded through the identical actuation gates — read-only-by-default,
admin-only, server-side bounds, rate-limited, audited, never auto-retried — see "Actuation" below;
`sungrow-sh-hybrid`/`deye-hybrid` are the first built-in profiles to actually declare Modbus
commands, every one inert until bench-verified (see the Help page below). Five more built-in
**Flow Engine** solar templates and an **in-app knowledge base** (a Help page with setup guides
for every solar profile, compiled into the binary so it works fully air-gapped) shipped alongside
these. **RTU (serial) and RTU-over-TCP transports have since shipped**: a Modbus device is no
longer necessarily Modbus TCP — its profile-independent `transport` can instead be `rtutcp` (RTU
framing over a plain TCP socket, for a transparent RS485→TCP gateway) or `serial` (RTU over a real
serial port, e.g. `COM3`/`/dev/ttyUSB0`, with baud/parity/data-bits/stop-bits fields, defaulting to
9600 8N1), sharing a per-port lock so several unit ids multi-dropped on one RS485 bus are polled
one at a time. SunSpec discovery, register-map reads, and guarded control writes all work
unchanged over any of the three transports. What remains of P5 is OPC-UA; the solar "system
workspace" (P8) is still design-only. **Home automation (richer command kinds, scenes, schedules) has also shipped**: a
device can now be dimmed, positioned, colour-tuned, or set to one of a named list of modes, not
just switched or given a plain setpoint; commands can be grouped into a named **scene** and run
together; and a scene or a single command can be fired on the clock or at sunrise/sunset via a
**schedule**. Every one of these still fires through the exact same actuation gates a manual
command does — see "Home automation" below. **Rule-driven actuation (a rule triggering a scene or
command automatically, with no human in the loop) is deliberately NOT built** — see
`docs/MYIOTSAN_PLAN.md` §8h for why. **A Flow Engine — a Node-RED-style visual, executable
data-flow canvas — has also shipped**: an admin can wire telemetry inputs through transforms
(including sandboxed JavaScript), logic and outputs on a drag-and-drop canvas; every flow's
actuation still goes through the identical guarded command path — see "Flow Engine" below.
**Active network discovery scanning has also shipped**: alongside waiting for a device to
announce itself, an admin can now sweep the LAN (Modbus, mDNS, SSDP, EtherNet/IP, BACnet) and
have what it finds land in the same quarantined candidate list the enrollment window fills — see
"Onboarding a device" below and `docs/DISCOVERY_SCANNING.md`. See `docs/MYIOTSAN_PLAN.md` for the
full roadmap and, in §8b/§8c/§8d/§8e/§8f/§8g/§8h/§8i, exactly what shipped and what was found by
live-booting it.

## Onboarding a device

The broker's security model is that **a device not in the inventory cannot connect at all** —
that is what makes the device table the credential store, and what makes deleting a device
actually revoke it. But a building's two hundred door contacts cannot be onboarded by typing two
hundred device keys by hand, and a device that does not exist yet cannot announce itself. The
resolution is a deliberate, time-boxed enrollment window, and this is the primary onboarding
path — manual provisioning (below) is for the cases where you already know exactly what you are
adding.

1. **Open a window** — `POST /api/discovery/window` (admin-only), `{"minutes": N}` (clamped to
   at most 60, default 10 if omitted). The response carries a one-time enrollment key —
   **returned exactly once, never readable again** — and closes on its own; there is no way to
   leave it open by forgetting about it.
2. **Point unprovisioned devices at the broker** using the enrollment key as their MQTT
   password and their own identifier as client id, same wire settings as below. An unknown
   client presenting a valid window key is admitted, but **quarantined**: its payloads are
   recorded as a candidate and go NOWHERE else — no telemetry row is stored, no rule is
   evaluated. Somebody who slips into the window can leave junk candidates for an admin to
   decline; they cannot forge a sensor reading, move a chart, or trigger or suppress an alert.
3. **Review candidates** — `GET /api/discovery/candidates`, chattiest first. Each one carries
   what it actually sent (topic + a sample payload) and, when the observed fields match a
   profile well enough (a 0.6 floor — below it the app says nothing rather than guess wrong),
   a suggested device type.
4. **Adopt or reject** — `POST /api/discovery/candidates/{id}/adopt` (optionally overriding the
   suggested `profileId`, plus name/tag/location) mints the device its own real, permanently
   generated credential, shown exactly once. **The enrollment key stops working for that device
   the moment it is adopted** — it is now a known device with its own password, so a leaked
   window key cannot later impersonate anything it let in.
5. **Close the window early** if needed — `DELETE /api/discovery/window`.

A device profile is portable: `GET /api/profiles/{id}/export` / `POST /api/profiles/import` let
an integrator who tuned a deadband for a sensor at one site carry that tuning to the next,
without retyping it. An imported profile is never marked builtin, and a slug collision is
reported rather than silently overwriting the existing profile's decoding rules.

### Scanning the network — the active counterpart to waiting

Not every device announces itself. `POST /api/discovery/scan` (admin-only, same page) sweeps the
LAN instead of waiting, and feeds the exact same quarantined candidate list — a scan never adds a
device, it only proposes candidates you then adopt through the same review step above:

- **Modbus** — a gentle subnet sweep (a cheap `:502` connect probe before it ever walks unit ids
  on a host that answers), given a network range as CIDR. A responding unit is auto-identified via
  SunSpec (vendor/model/serial, suggesting the `generic-sunspec-solar` profile) or, if it answers
  Modbus but isn't SunSpec, filed as an "unidentified Modbus" candidate for you to assign a vendor
  register-map profile to. Adopting a Modbus candidate carries its endpoint/unit/transport
  straight into the new device — it polls immediately, no re-typing.
- **mDNS** and **SSDP/UPnP** — find consumer/AV gear already on the LAN (Chromecast, Sonos,
  HomeKit, printers). These are found for visibility, not necessarily control: a TV showing up
  here means the app can *see* it, not that it can *drive* it, unless a matching profile/driver
  exists.
- **EtherNet/IP** and **BACnet** — broadcast the CIP/BACnet standard "who are you" probes
  (ListIdentity, Who-Is) for industrial PLCs and building-automation controllers.

**Every scan is read-only, admin-only, LAN-local, and bounded** (a 1024-host cap, a per-scan
timeout, a concurrency cap) — nothing here ever writes to a device, and a scan is audited to the
notification feed the same way opening an enrollment window is. See
`docs/DISCOVERY_SCANNING.md` for the full safety posture, what each scanner has actually been
verified against (Modbus/mDNS/SSDP live-booted end to end; EtherNet/IP and BACnet are
parser-verified only — no real PLC was available to test against), and what was deliberately left
out (OPC-UA discovery, Profinet DCP, a Matter controller, native TV/AV control) and why.

## Connecting a device manually

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

**A POLLED (Modbus) device is provisioned the same way, with `endpoint`/`unit` instead of a
password.** When the chosen profile's `transport` is `modbus`, `POST /api/devices` (and the
device's Settings form) asks for `endpoint` (the device's `host:port`, Modbus TCP is usually port
502) and `unit` (the Modbus unit/slave id, often `1` — a gateway can host several units behind one
endpoint) instead of a broker password. The app dials OUT to the device on the profile's poll
cadence; there is nothing to point at the app the way an MQTT device points at the broker. The
device's own `transport` field then picks HOW the app reaches it, independent of the profile:
`tcp` (Modbus TCP/MBAP, the default), `rtutcp` (RTU frames over a plain TCP socket — a transparent
RS485→TCP gateway), or `serial` (RTU over a real serial port). For `serial`, `endpoint` instead
holds the port name (`COM3`, `/dev/ttyUSB0`), and the form asks for baud/parity/data-bits/stop-bits
too (defaulting to 9600 8N1) — the same fields `ModbusFields` in the device editor shows only when
`serial` is selected.

## The device-type catalog (profiles)

A `DeviceProfile` declares what a device TYPE reports: for a PUSH (MQTT) device, its topic
template and payload format (JSON or raw); for a POLLED (Modbus, P5) device, its `Transport:
"modbus"` plus `ModbusMode` (`"sunspec"` self-describing, or `"regmap"` an explicit vendor
register map) and poll cadence — and either way, its `TelemetryKey`s (name, unit, deadband,
heartbeat, plausible range, plus a JSON path or a Modbus register binding depending on
transport). Without this abstraction, onboarding a hundred identical door sensors means
configuring a hundred devices by hand; with it, the hundredth device is a name and a dropdown.
Fourteen built-in profiles ship and are re-seeded (idempotently — existing ones are never
overwritten) on every boot: nine PUSH profiles (door/window contact, PIR motion,
temperature/humidity, smoke/heat detector, water leak, power/energy meter, access-control
reader, smart relay, smart lamp) and five POLLED (Modbus) profiles — `generic-sunspec-solar`
(a self-describing SunSpec inverter/meter/battery that needs no per-model map), `huawei-sun2000`
(an explicit register map for the world's most-installed inverter), and three more for the
budget/high-volume hardware an installer outside that world actually buys:
`sungrow-sh-hybrid` (Sungrow SH residential hybrids — input registers, word-swapped 32-bit
values, and its own set of pre-declared Modbus commands), `deye-hybrid` (the same map answers
for Deye, Sunsynk, and Sol-Ark rebadges), and `eastron-sdm630-meter` (a read-only 3-phase meter,
IEEE-754 float32 values). A site can author its own profile (or copy and edit a builtin —
builtins themselves cannot be deleted, only used or copied) via `POST /api/profiles`. See the
in-app **Help** page (below) for a setup guide per profile.

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

## Actuation

Every other section above is about reading a device. This one is about writing to one, and it is
built to be hard to use by accident: a camera is read-mostly, but an IoT device gets **written
to**, and a bad write is dangerous in a way a bad camera PTZ move is not — it opens a door, trips
a breaker, sets a thermostat to 200°C.

**The gates, all enforced server-side:**

1. **Read-only by default.** A device cannot be commanded until `actuationEnabled` is explicitly
   turned on for it (per device, in its Settings). Adoption never turns it on.
2. **Admin only.** Not an operator power, and this rule predates the command path itself.
3. **Only what the device's profile declares.** There is no "publish an arbitrary payload to any
   topic" endpoint anywhere in the app — and that is now enforced rather than merely intended. The
   flow canvas's `mqtt_out` node publishes through the server's own broker handle, which is subject
   to no ACL; pointed at a device's command topic it moved a real relay whose actuation was switched
   off, outside the declared bounds, past the rate limit, with nothing written down. A topic a real
   device would act on as a command is now reserved to the guarded path, refused at save and again
   at run time, and the refused attempt is recorded in that device's command history like any other.
4. **Bounds are server-side.** A setpoint outside its declared `min..max` is refused in the
   service, never merely blocked in the UI. A setpoint that declares no range at all (`min` and
   `max` both `0`) refuses every value — an omission is read as "no", not "anything goes".
5. **Rate-limited** (2 seconds between commands to the same device — a relay has a duty cycle)
   **and audited** — every attempt, including every refusal, is a `device_command` row naming
   who tried, plus a notification. "Somebody tried to unlock the front door at 03:00 and was
   refused" is not thrown away just because it failed. A command (or an alert acknowledgement)
   issued from `myseliasan`'s fleet UI (see "Fleet" below) arrives over the control channel from
   an operator who has no account on this node — the row still names them by name (e.g.
   `cp:admin`), not just a local user id that would otherwise read as `0`/"System".

**Two transports, one set of gates.** A device profile's command declares a `transport`: `mqtt`
(default) publishes the payload template below to the broker, same as always; `modbus` WRITES a
holding register on the polled device instead, using a guarded write-then-read-back
(`infra/iot/modbus.WriteConfirm`) so the write is confirmed by the device itself, not assumed. Both
transports pass through every gate above unchanged — Modbus actuation is not a separate, looser
path, it is the write half of the same poller a Modbus profile already reads with. A Modbus command
never auto-retries either: `WriteConfirm` has a 5-second timeout and only ever re-*reads* to
confirm, never re-writes, so a lost confirmation cannot become a second physical write. A Modbus
command's value is encoded to the register's `regKind` (`u16`/`i16` — single-register writes only;
a multi-register kind is refused rather than half-written) after applying the register's
`scaleFactor` in reverse (`raw = round(value / scaleFactor)`, the same scale the read side uses).

### Declaring a command on a device profile

A profile (see "The device-type catalog" above) declares zero or more commands alongside its
telemetry keys, via `POST/PUT /api/profiles`:

- `name` / `label` — the command's identifier and display text.
- `kind` — decides how the value is validated and rendered:
  - `"switch"` — accepts only `0`/`1`.
  - `"setpoint"` — accepts a number within `min..max`.
  - `"dimmer"` / `"position"` — a percentage, fixed `0..100` (brightness, blind travel).
  - `"cct"` — colour temperature in Kelvin, a setpoint bounded by `min..max`.
  - `"mode"` — one of the integer values enumerated in `options` (below), turning the command
    into a named dropdown rather than a raw number.
  - `"color"` — an RGB colour packed into one integer (`0xRRGGBB`).
  - **An unrecognised `kind` is refused, not silently passed** — a command declaring a typo'd or
    unknown kind cannot be issued at all.
- `transport` — `mqtt` (default) or `modbus`. Decides which of the two field groups below applies.
- **MQTT fields** — `topicTemplate`: where the command is published, `{deviceKey}` substituted.
  `payloadTemplate`: the message body. `{value}` is substituted for every kind (e.g.
  `{"method":"Switch.Set","params":{"id":0,"on":{value}}}`); a `"color"` command additionally
  substitutes `{r}`/`{g}`/`{b}` with the unpacked 0..255 channels. An empty template sends the
  bare value, for a device whose topic itself is the instruction.
- **Modbus fields** — `register`: the holding register this command writes. `regKind`: `u16` or
  `i16` (single-register writes only; a `u32`/`i32` command is refused rather than half-written).
  `scaleFactor`: the same multiplier the read-side telemetry binding uses, applied in reverse
  (`raw = round(value / scaleFactor)`). A Modbus command needs no `confirmKey` — the write is
  confirmed inline by reading the register back.
- `min` / `max` — the safe range for a `setpoint` or `cct`. **Required for either to be usable at
  all** — leaving both at `0` means the command declares no safe range and every value will be
  refused.
- `options` — for a `mode` command, a JSON list of `{"value":<int>,"label":<string>}` naming its
  allowed values; a value not in the list is refused, and an empty/malformed `options` refuses
  every value.
- `confirmKey` (MQTT only) — the telemetry key the device reports the resulting state back on.
  Without this, an MQTT command can only ever reach `sent`, never `confirmed`. A `color` command
  typically declares none: a bulb that reports colour back per-channel cannot be equality-confirmed
  against one packed float, so "sent, never confirmed" is the honest status for it.

Of the built-in profiles, `smart-relay` (Shelly/Tasmota conventions — `output`, a switch,
confirmed by the device's own `output` telemetry key) and `smart-lamp` (Zigbee2MQTT conventions —
`power`/`brightness`/`color_temp`/`color`, the worked example for the newer kinds) ship MQTT
commands declared. `sungrow-sh-hybrid` and `deye-hybrid` are the Modbus counterparts — Sungrow
pre-declares seven (`ems_mode`, `batt_force`, `batt_force_power`, `export_limit`,
`export_limit_enable`, `batt_min_soc`, `batt_max_soc`) and Deye four (`work_mode`, `solar_sell`,
`grid_charge`, `max_sell_power`). **Every one of these ships INERT**: a command declared on a
profile is not the same as a command you can send — it only becomes usable once you turn on
`actuationEnabled` for the specific device AND have verified, on the bench, that the register's
sign/scale match your model's firmware (see the in-app Help page, "Verify control before you
actuate"). Every other shipped profile (including the read-only `eastron-sdm630-meter`) stays
read-only, which is the correct default: a sensor that cannot be commanded cannot be commanded
wrongly.

### Sent, confirmed, failed — what an operator should read into each

- **`sent`** means the app successfully published the command to the broker (MQTT transport only).
  It does **not** mean the physical thing happened — a relay could have missed it, or the device
  could be about to act. Do not treat `sent` as "done". A Modbus command never passes through
  `sent` at all — it goes straight to `confirmed` or `failed`, since the write is confirmed inline.
- **`confirmed`** is the only status that means the device physically acted: for an MQTT command
  it is set the moment the device reports the state back on the command's `confirmKey`; for a
  Modbus command it is set the moment the guarded write reads the register back and sees the
  value land — no separate reported reading is needed. This is the status to wait for before
  believing a door is locked or a breaker is open.
- **`failed`** means either the command was refused (a gate rejected it — the reason is given
  verbatim, e.g. "outside the safe range 5..30") or, for MQTT, it was sent but never confirmed
  within 30 seconds, or, for Modbus, the guarded write itself was not confirmed within 5 seconds.
  **A failed-by-timeout command is never automatically resent, on either transport** — re-sending
  a relay write or a register write is a second physical action, and if the first one actually
  landed but its confirmation was lost in transit, a retry would fire the relay (or write the
  register) again (the door opens twice). If a command times out, check the device and re-issue it
  yourself if it is still needed.

The device twin (`GET /api/devices/{id}/twin`) shows desired vs. reported state per key. A
desired value that was asked for more than 5 minutes ago and never got confirmed is shown as
disagreeing with the reported value, but it is **not** re-applied automatically when the device
reconnects — a door controller that was offline for a month must not spring back to life and
apply a stale "unlock" nobody is around to see. Re-issue the command if the state still needs
changing.

`GET /api/devices/{id}/commands` lists what a device can be told to do (its profile's declared
commands) and whether `actuationEnabled` is on — the response the "Actuate" panel in the device
page is built from. `GET /api/devices/{id}/commands/history` is the full audit trail, readable
by viewer and operator as well as admin (see "Role model" below).

## Home automation: scenes and schedules

Two more layers over the same gated actuation path above, for grouping and scheduling commands
rather than issuing them one at a time by hand:

- **Scenes** (`POST/GET/PUT/DELETE /api/scenes`) group an ordered set of device commands under one
  name — "movie night", "all off", "goodnight". `POST /api/scenes/{id}/run` fires them in order.
  **Running a scene is not a new authority**: each action goes through the exact same
  `CommandService.Issue` a manual command does — actuation-enabled, admin-only, declared-commands-
  only, server-side bounds, rate limit, audit, all apply per action. A scene never rolls back and
  never stops early on a refusal: it runs every action and reports each outcome, so a partial
  failure (e.g. two actions hitting the same device inside the 2-second rate limit) is visible,
  not hidden. Running a scene is admin-only; reading one is not.
- **Schedules** (`POST/GET/PUT/DELETE /api/schedules`) fire a scene or a single command at a time
  — the automation a rule cannot express, because its trigger is the clock, not a reading. A
  trigger is a fixed time of day (optionally restricted to certain weekdays) or sunrise/sunset ±
  an offset (computed locally from the site's latitude/longitude, `GET/PUT
  /api/settings/location` — no external API call, appropriate for an air-gapped install).
  `POST /api/schedules/{id}/run` test-fires one immediately. A schedule fires through the identical
  gated path as a scene or a manual command, attributed in the audit trail to a synthetic
  `schedule:<name>` actor rather than "System". Authoring, test-firing, and setting the site
  location are all admin-only; reading schedules is not.

**Rule-driven actuation is deliberately not built.** An `iot_rule` (see "Rules and alerts" above)
can only raise an alert — it cannot trigger a scene or a command on its own. That is a considered
gap, not an oversight: a rule that can write to a device with no human in the loop is a materially
different risk than one that raises an alert, and it is deferred to a later, security-reviewed
change. See `docs/MYIOTSAN_PLAN.md` §8h.

## Flow Engine

A visual, executable data-flow canvas — the myiotsan equivalent of a Node-RED flow — for the
composite computations scenes/schedules cannot express: combining two telemetry streams into a
derived value (self-consumption, net grid), or wiring a bespoke transform-then-alert chain without
it deserving its own first-class entity. `GET/POST/PUT/DELETE /api/flows`, plus
`POST /api/flows/import`, `GET /api/flows/{id}/export`, `GET /api/flows/{id}/slots`,
`POST /api/flows/{id}/instantiate`, `POST /api/flows/{id}/run` (test-fire), and
`GET /api/flows/{id}/debug` (the live per-node inspector). **The whole area is admin-only, unlike
scenes/schedules where reading is open to every role** — even seeing a flow's graph reveals what it
could do, and test-firing one can actuate a real device.

A flow is a graph of NODES (an input that emits on a new reading; transforms — including a
`function`/`expression` node running arbitrary JavaScript, and a `switch` node gating on a JS
predicate — plus a plain `scale`/`threshold`/`deadband`/`throttle` (rate-limit: at most once per N
seconds); and outputs: `debug` for the inspector, `notify` to raise an alert, `command` to actuate,
`derived_metric` to persist a computed value as a new telemetry series, `mqtt_out` to publish the
payload to an MQTT topic on the embedded broker) joined by wires, drawn and saved as one document.
`mqtt_out` publishes data outward — a processed value fed to another system or a home-automation
subscriber — and it **may not publish a device command**: the topics this hub's own devices act on
are reserved to the guarded path, so an `mqtt_out` node aimed at one is refused when the flow is
saved and refused again at run time, with the attempt written into that device's command history.
Any other topic stays publishable, which is what the node is for.

**The safety design is the point.** A `function`/`expression`/`switch` node runs in an embedded,
sandboxed JavaScript interpreter with **no host bindings at all** — no filesystem, no network, no
`require`, no `os` — fenced by a hard 100ms watchdog per call, so neither an escape attempt nor an
infinite loop can reach outside the flow or wedge it. But the real guarantee is this: **nothing in
a flow can actuate except the dedicated `command` output node, and that node routes through the
exact same guarded `CommandService.Issue` chokepoint every manual command, scene, and schedule
already uses** — actuation-enabled, admin-only, declared-commands-only, server-side bounds, rate
limit, full audit, never auto-retried. An arbitrary-JavaScript node can shape a *value*; it cannot
skip a gate, retry a refused write, or reach a device the command layer would otherwise refuse. A
flow is convenience and computation layered on the existing actuation path, never a new authority.

**Templates via device-role slots.** A flow becomes reusable simply by naming a device with a
placeholder (`"$inverter"`) instead of a concrete key — `GET /api/flows/{id}/slots` reports what a
flow declares, `POST /api/flows/{id}/instantiate` binds every slot to a real device and stamps out
a concrete, disabled-by-default copy for review. The shipped "Solar system" sample flow is exactly
this: it derives on-site self-consumption from grid + PV power behind an `$inverter` slot, plus a
high-grid-import alert — instantiate it against your adopted inverter to get a ready-to-enable
flow. A flow document is portable (`.iotflow`, export/import), mirroring `.iotprofile`; an import
is never builtin and always arrives disabled.

See `docs/MYIOTSAN_PLAN.md` §8i for the full design rationale, including why this deliberately
reverses an earlier "no visual node-graph editor" scope line.

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
camera PTZ move is not. This rule was written into the catalog in P0, before the command path
that would exercise it existed, and it is not to be loosened without a deliberate decision.

The authorization catalog now covers the shipped device/telemetry/rules/notification/actuation
surface: a viewer sees devices and their current readings and that an alert fired; only an
operator can review `/api/devices/*/readings` (the history) or acknowledge an alert — the same
evidentiary line mymatasan draws for its own alert log; creating/deleting devices, editing
profiles, and writing rules stay admin-only (`apps/myiotsan/services/rbac.go`).
**`/api/discovery` (opening an enrollment window, and adopting or rejecting a candidate) is
admin-only too** — it is the one act in the whole app that lets an unknown thing talk to the
broker at all, so an operator does not get to do it either. **`POST /api/devices/{id}/commands`
(issuing a command) is admin-only**, but `GET /api/devices/{id}/commands/history` and
`GET /api/devices/{id}/twin` are readable by viewer and operator — seeing what was done to a
device, and whether it actually happened, is not the same power as doing it; an audit trail
visible only to the people who could have written to it is not an audit trail. The same line is
drawn for the two home-automation surfaces: reading scenes and schedules (`GET /api/scenes`,
`GET /api/schedules`) is open to viewer/operator, but **running a scene, test-firing a schedule,
authoring either, and setting the site location are all admin-only** — running one commands real
devices through the identical actuation path a manual command takes. **The Flow Engine draws the
line differently again: the WHOLE `/api/flows` area is admin-only**, including merely reading a
flow's graph — unlike a scene or a schedule, a flow's graph itself can reveal an actuation path
(and reading a flow's slots is meaningful only to someone who can also instantiate/enable it), so
there is no viewer/operator-readable tier here at all.

## Settings

One tabbed, admin-only page for everything that configures the hub itself, as opposed to the
devices it watches: **Users**, **Location**, **Notifications**, **Telemetry**, **Connectivity**,
and **System**. The whole page is hidden from non-admins in the SPA nav, and every route below is
gated admin-only server-side too (`services.Policy()`).

- **Users** — `POST/GET /api/settings/users` (+ `PUT`/`DELETE /api/settings/users/{id}`,
  `POST /api/settings/users/{id}/password`) and `GET /api/settings/roles` (previously named in
  the authorization catalog but 404'd — see `docs/MYIOTSAN_PLAN.md` §8d — so `viewer`/`operator`
  were unassignable and the appliance was effectively single-admin). They run on the same shared
  local-user service `mymatasan` uses, so an edit that would remove the last administrator is
  refused the same way in both apps — an appliance nobody can administer is a bricked appliance.
- **Location** — `GET`/`PUT /api/settings/location`, the site latitude/longitude a sunrise/sunset
  schedule needs (see "Home automation: scenes and schedules" above).
- **Notifications** — `GET`/`PUT /api/settings/notification` + `POST /api/settings/notification/test`,
  now a list of **delivery destinations** (`PUT /api/settings/notification/destination`,
  `DELETE /api/settings/notification/destination/{id}`) rather than a single webhook+telegram
  pair, mirroring mymatasan's per-destination model. A destination is a webhook (any `http(s)`
  URL), a Telegram chat (bot token + chat id), or an MQTT publish target (broker URL, topic,
  QoS/retain, optional username/password or TLS client cert) — each with its own minimum severity
  floor and a category filter (`device.alert` / `system`; ticking none means every category).
  Saving one destination never clobbers another's stored config, and the UI presents them as an
  accordion, one row per destination. **This is the first time myiotsan's alerts ever leave the
  box at all** — before this page, every alert (a rule firing, a device going offline, a refused
  command) landed only in the in-app notification feed; saving a destination (or simply booting
  with destinations saved in a previous run) is what actually wires the shared notification hub's
  outbound channels. The delivery engine itself is unchanged shared infra (`domain/notification`,
  `infra/notification`) — nothing new was built there, only the myiotsan side that finally calls
  into it. Use the test button to confirm a channel actually delivers; "saved" is not "reaches my
  phone". An older config saved under the original single webhook/telegram fields migrates
  forward automatically into the destination list the first time it is read.
- **Telemetry** — `GET`/`PUT /api/settings/telemetry`: raw/rollup retention days, the
  write-behind batcher's batch size/flush interval/queue size, and the embedded MQTT broker's
  listen address. **Unlike Notifications, saving here does not apply live** — every one of these
  values is read once when the app constructs its ingest pipeline at boot, so an edit takes
  effect only after a restart; the tab says so and links straight to the System tab's restart
  button.
- **Connectivity** — fleet pairing (see "Fleet" below): fleet key, claim code, adoption status,
  unpair.
- **System** — app/core version, health, and `POST /api/system/restart` (responds first, then
  restarts ~500ms later so the browser can show a "restarting…" overlay). This is also the tab an
  operator uses after editing Telemetry settings, since those need a restart to take effect. The
  tab also renders the shared **Deployment** panel (deployment mode / Phase 1 multi-instance
  safety), fixed here: `GET /api/deployment/preflight` answers `Appliance: true` because the
  Modbus RTU pollers hold serial ports (`COM3`, `/dev/ttyUSB0`) that only one process on one
  machine can open — the panel states the reason rather than offering a choice, since an operator
  who does not know that would otherwise go looking for the setting and find nothing. A
  **Danger zone** on the same tab offers **Reset to factory settings** (`GET
  /api/system/reset/state`, `POST /api/system/reset`, `GET /api/system/reset/progress`, built on
  the shared `domain/shared/services.SystemResetService`) — myiotsan previously had no factory
  reset at all. Confirmation is GitHub-style: type `myiotsan` exactly before the button enables,
  and the server independently re-checks the typed phrase rather than trusting the dialog. A
  confirmed reset erases file storage and the entire at-rest key **directory** (not just the key
  file — myiotsan's cipher is only built inside the fleet/pairing block, so removing the whole
  directory, marker included, is what makes the next boot read as a clean first run rather than
  tripping the fail-closed recovery gate), drops and rebuilds the database, and restarts into
  first-run setup behind a blocking progress overlay. **Enrolled devices are not told** — each
  keeps its provisioned broker password and reconnects to a hub that no longer knows it, landing
  back in quarantine as a candidate; if this hub is itself adopted by a control plane, the reset
  also drops its fleet enrollment. Unlike myseliasan/myidsan, myiotsan ships `bootstrap.allowReset:
  true` by default, so this panel is visible out of the box. See
  `docs/modules/domain/shared/services/system_reset.go.md`.

## Fleet

`myiotsan` is adopted into a `myseliasan` fleet exactly the way a `mymatasan` node is, on the
same shared node stack (`domain/shared/fleetnode`): LAN discovery (authenticated UDP
multicast), single-parent adoption by a claim code an operator reads off this app's Settings
page, and mTLS enrollment against `myseliasan`'s on-prem fleet CA. No new fleet ports are
needed — it dials the same discovery/pairing/control ports mymatasan nodes already use (see
"Configuration" below).

**Two channels, not three.** Once adopted, `myiotsan` dials the mTLS **enrollment/heartbeat**
listener and the **control channel** (tunnelled commands down, events up). It does *not* open
`mymatasan`'s third, camera-only **media** channel — a sensor hub has no video to stream, and
an unused listener is an unused attack surface.

**Its alerts flow into the control plane's feed.** Everything this node raises — a rule alert, a
device gone silent, a relay command, a sign-in lockout — is pushed up the control channel into
`myseliasan`'s unified notification feed, the same way a `mymatasan` node's alerts already are.
That is what makes cross-domain correlation possible: `myseliasan` can now express a rule like
*motion on a camera AND a door contact opening AND no badge swipe → intrusion*, something
neither this app nor `mymatasan` can see on its own. See `myseliasan`'s README ("Fleet rules")
and `docs/MYIOTSAN_PLAN.md` §8e for how that correlation works and how it was verified live.

`myseliasan`'s fleet UI shows this app's nodes as "Sensor hub" (as opposed to a `mymatasan`
node's "Camera node") — the node reports its kind over the fleet-signed adopt call, not over
the unsigned discovery announce, so a hostile host on the LAN can at most make a fake node show
the wrong icon in a scan list; it cannot make the control plane adopt anything or change what an
already-adopted node is.

**An adopted node is now fully manageable from `myseliasan` (P7), not just visible.** The fleet UI
embeds this app's own devices/rules/alerts/commands pages directly (mirroring how it already
embeds a `mymatasan` node's camera pages) — the browser never talks to this node directly, every
call is proxied browser → `myseliasan` → control channel → node, which is what lets a node sit
behind NAT with no inbound firewall rule. See `myseliasan`'s README ("Managing an adopted device
node") for the operator-facing side of this.

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
- No new fleet ports: adoption into a `myseliasan` fleet (see "Fleet" above) dials the same
  discovery/pairing/control ports mymatasan nodes already use.

Run locally:

```bash
go run . -app myiotsan
```

## Runtime Metrics

Prometheus is enabled by default (see root `README.md` → Telemetry) and scraped from `/metrics`.
Before this, myiotsan exposed 0 app-specific series — a live scrape confirmed it. The ingest
pipeline is deliberately arranged so a publish never touches the database, so its failures never
raise an error a human sees; these metrics instrument exactly those silent failure modes.

| Metric | Type | Labels | What it tells you |
|---|---|---|---|
| `myiotsan_ingest_received_total` | gauge (sampled counter) | — | Payloads accepted from devices — the denominator for everything else below. |
| `myiotsan_ingest_stored_total` | gauge (sampled counter) | — | Readings actually written to the database (passed the deadband). |
| `myiotsan_ingest_suppressed_total` | gauge (sampled counter) | — | Readings the deadband dropped as unchanged. This ratio *should* sit high (90%+ on real building sensors) — that's the storage design working. Falling toward zero means a deadband has been mistuned. |
| `myiotsan_ingest_dropped_total` | gauge (sampled counter) | — | **The headline metric.** Readings shed because the write queue was full — ingest has outrun the disk. Silent data loss: the broker keeps accepting and the UI keeps rendering, so without this, nothing else shows it. Verified live at `86` against a torrent that outran the disk. Alert on any increase. |
| `myiotsan_ingest_queue_depth` | gauge | — | Current write-queue depth — the leading indicator of drops, before readings are actually lost. |
| `myiotsan_ingest_series` | gauge | — | Distinct `(device, key)` series the deadband gate is tracking. |
| `myiotsan_devices_online` / `myiotsan_devices_offline` | gauge | — | Fleet health at a glance. Offline is the one to alert on — a sensor gone silent is a monitoring blind spot, and a smoke detector gone silent is worse. |
| `myiotsan_commands_total` | counter | `outcome` (`confirmed`/`failed`/`refused`) | Actuation command outcomes. A rising `failed` is devices not acting; a rising `refused` is somebody repeatedly trying something they aren't allowed to. |
| `kopiv2_control_events_forwarded_total` | counter | `kind` | Node events (notifications, going-offline) successfully pushed up the fleet control channel to `myseliasan`. Only meaningful next to the drop counter below — a drop count with no total is a number nobody can size. |
| `kopiv2_control_events_dropped_total` | counter | `kind`, `reason` (`disconnected`/`write_failed`) | Node events that could **not** be forwarded — the control channel was down, or the write itself failed mid-flight. Both paths used to return silently with no record. The running count since the last successful hello also rides upstream on the node's next control-channel hello, so `myseliasan` sees it too (`myseliasan_node_events_dropped_total`). |
| `myiotsan_task_panics_total` | counter | `task` | Recovered panics in `infra/safego`-supervised background tasks. A supervised task is restarted automatically on panic, but that alone leaves no other trace than one log line. |

The ingest gauges are **sampled** off ingest's own atomic counters every 10 seconds, not
instrumented on the publish path directly — that path arrives thousands of times a second and
must stay off any shared lock. `myiotsan_commands_total` is counted directly (commands are rare).

What's worth alerting on:
- Any increase in `myiotsan_ingest_dropped_total` — telemetry is being lost right now.
- `myiotsan_ingest_queue_depth` climbing toward its configured cap — act before drops start.
- `myiotsan_devices_offline > 0` sustained for a device that should be reporting.
- A rising `myiotsan_task_panics_total` for any `task`.
- Any increase in `kopiv2_control_events_dropped_total` while this node is adopted into a fleet — a rule alert or health event that never reached `myseliasan`'s unified feed live.

## Install & release

`myiotsan` ships as an installable product, not just a `go run` target: portable archives,
`.deb`/`.rpm` packages (nfpm), a Windows Inno Setup installer, multi-arch Docker images, and a
release workflow (`.github/workflows/release-myiotsan.yml` + `.goreleaser.myiotsan.yaml`). See
`deploy/README-myiotsan.md` for full install/upgrade/uninstall instructions per platform.

- **Its own release, never GitHub's "latest".** Releases publish under a `myiotsan-v<ver>` tag
  namespace, not `v<ver>`, and `--latest=false`. `mymatasan`'s in-app updater reads
  `releases/latest` — if a `myiotsan` release ever became "latest" it would find no
  `mymatasan_`-prefixed asset there and every deployed camera node would silently stop updating.
- **No shipped default credential.** The packaged config's `localAuth.password` is empty; on
  first boot the app generates a random admin password and writes it once to
  `INITIAL_ADMIN_LOGIN.txt` in the data directory (see "Authentication" above). A relay-capable
  appliance with a documented default password is a fleet-wide backdoor.
- **MQTT `1883/tcp` needs a firewall rule.** It is a new listening port no other product in this
  suite opens. The Windows installer adds the Windows Firewall rule for it automatically; on
  Linux/Docker, allow it explicitly or devices will never connect (this looks like "broken
  devices", not "blocked port", so it is easy to misdiagnose).
- **No self-updater.** Unlike `mymatasan`, `myiotsan` does not check GitHub releases from inside
  the running app. Updates are manual, or via your package manager (`apt`/`rpm`) or container
  registry — see `deploy/README-myiotsan.md`.
- **Load/security testing:** `tools/k6 -App myiotsan` drives the HTTP console only — the real
  throughput risk is MQTT→SQLite ingest, which a k6 run cannot exercise; watch
  `GET /api/devices/stats` (`suppressed`/`dropped`) for that instead. `tools/zaproxy -App myiotsan`
  scans the HTTP API but **deliberately excludes** `/api/devices/*/commands`, device-password
  rotation, pairing, and enrollment — an active scanner firing an actuation endpoint is a
  physical-hardware risk, not a data one.

## Frontend

The SPA (`apps/myiotsan/views/react-webpack/`) is built off the shared `@shared` frontend
module the same way myseliasan's is — no per-app copy of `DataTable`/`SideNav`/icons/i18n.

Screens: **Dashboard** (estate health, recent alerts, and the ingest panel), **Devices** — two
sub-tabs: **Inventory** (the device list, live values, telemetry charts, manual provisioning, and
— on a per-device **Control** tab shown to every role, since its history/twin are readable by
everyone and only issuing a command is admin-only — the Actuation panel: available commands
rendered as the appropriate widget per `kind` — a switch toggle, a bounded number, a slider for
`dimmer`/`position`/`cct`, a dropdown for `mode`, a colour picker for `color` — command history,
and the desired/reported twin; a separate tab rather than a strip on the readings page, because
reading a sensor and firing a relay are different acts) and, admin-only, **Discover** (the
enrollment window, the network scan, its candidates, and adoption — see "Onboarding a device"
above); onboarding lives here rather than as its own nav entry because "add a device" is where an
operator looks for it — **Rules**, **Alerts**, **Notifications**, **Device types** (the
profile catalog, its deadbands, its declared commands — including authoring a `mode` command's
`options` — and import/export), and, under the **Automation** nav group, **Scenes** (author and
run a named, ordered group of commands — the Run action hidden for anyone but an admin),
**Schedules** (author a clock or sunrise/sunset trigger, test-fire it, and set the site location
the sun triggers need), and **Flows** (an admin-only entry — the whole tab is hidden from every
other role — showing the flow list and the SVG canvas editor: a node palette, drag-and-drop wiring,
a per-node config panel, and a "run" action that test-fires the flow and lights up the canvas with
the resulting per-node debug values). **Help** is a new nav entry: a read-only, dependency-free
Markdown-rendered setup guide (`GET /api/kb`), covering every solar/Modbus profile, gateway/
transport choices, and how to verify a control register before enabling actuation — visible to
every role, since it is reference content with nothing to misuse. A first-run onboarding wizard
leads a new install straight to opening its first enrollment window; its enrol/ready steps now
also read live hub state (`GET /api/discovery/window`, `GET /api/discovery/candidates`) so they
report whether a window is open right now and how many candidates are already waiting, instead
of describing the mechanism in the abstract. Dismissal used to be a `localStorage` key, which
made it per-**browser** — the same admin met the wizard again from a second machine, or after
clearing site data, on a hub that had been running for months. It is now the same server-side
`setup.state` flag (`GET/POST /api/setup/state`, `/complete`) the rest of the suite uses, so
dismissal sticks per install. All four locales — en, ms, zh, ar.

Two things on the Dashboard deserve an operator's attention:

- The **ingest panel** states the suppression rate in plain English ("the deadband kept 98% of
  readings out of the database"). That ratio is the storage design working. If it ever falls
  toward zero, a deadband has been mistuned and the disk is about to be in trouble.
- **Dropped** readings get their own alarm block rather than a slot in a counter row. A
  non-zero, growing value is the one number that means ingest has outrun the disk and readings
  are being shed.

The alert log has **no delete button**, deliberately: acknowledging is an operator power,
erasing the record is not.
