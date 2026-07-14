# MyIotSan — Implementation Plan

Status: **P0 SHIPPED** (2026-07-14). **P1 (ingest spine) + P2 (rules & alerts) SHIPPED
(2026-07-14) — P0-P2 is the shippable MVP.** P3-P7 not yet started.

`myiotsan` is the fourth app in the suite, alongside `mymatasan` (camera NVR), `myseliasan`
(fleet control plane) and `myidsan` (identity/SSO). It is built on the same platform as
`mymatasan`, but its devices are **building and security IoT sensors** instead of cameras.

---

## 1. Product definition

> **An on-prem IoT device hub — "the NVR, but for sensors."**

Single Go binary. SQLite by default. Runs air-gapped on an intranet. Encryption-at-rest,
RBAC and audit. Adopted into the `myseliasan` fleet over the existing pairing/control
channel. SSO via `myidsan`.

Target devices (first release): door/window contacts, PIR motion, temperature/humidity,
smoke and heat detectors, water-leak sensors, power/energy meters, and access-control
readers. Predominantly MQTT (Zigbee2MQTT / Tasmota / Shelly / ESPHome conventions), with
Modbus TCP for meters and panels.

### What it is not

It is **not** a ThingsBoard/Magistrala-style multi-tenant cloud platform, and it is **not**
a Home Assistant-style hobbyist automation hub. Those are the two directions scope creep
will pull, and both should be refused. `myiotsan` is for devices that must be *monitored,
alerted on, audited and fleet-managed* — the same product promise as `mymatasan`, sold to
the same customer.

### The differentiator

`myseliasan` already adopts nodes. Once it adopts **both** camera nodes and IoT nodes, the
suite can express **cross-domain rules** that no competitor can:

> *Motion on Camera 3* **and** *door contact opened* **and** *no badge swipe* → alert.

ThingsBoard cannot see your cameras. `mymatasan` cannot see your door sensors. This
correlation is the reason the fourth app exists. It lands in Phase 6, but every earlier
decision should keep the path to it open.

---

## 2. Structural mapping from mymatasan

`mymatasan`'s spine is not camera-shaped. It is:

    device → signal → detector → rule → alert → notify → historize → dashboard

Cameras are one signal type. Sensors are another. The mapping is close to mechanical:

| mymatasan | myiotsan | Reuse |
|---|---|---|
| `camera` entity (host/port/vendor/model/serial/firmware/health/lastSeen/discoveryMethods) | `iot_device` + `protocol` + `profile_id` | Rename + trim |
| RTSP stream source (`infra/rtsp`, `infra/stream`) | MQTT / Modbus / HTTP telemetry connector | New |
| Vision detectors (object/motion/line/crowd) | Threshold / delta / rate / stuck / offline / anomaly evaluators | New logic, same seam |
| `detection_rule`: `Threshold`, `MinFrames`, `CooldownSeconds`, `SchedulePolicy` | `iot_rule`: threshold, consecutive-sample debounce, cooldown, schedule | **Near 1:1 port** |
| `alert_event` + snapshot path | `alert_event` + reading context | As-is |
| Recording segments + retention + purge | Telemetry readings + rollups + retention | Same shape, new store |
| Recording timeline / playback UI | Telemetry chart timeline with alert markers | Same shape |
| Notification hub (webhook/telegram/MQTT/SSE/store) | Identical | **Verbatim** |
| Dashboard analytics (rollup/baseline/anomaly/heatmap/noise/reliability) | Identical, sourced from telemetry | **Verbatim** |
| Camera capacity estimator | Device/datapoint capacity estimator | Direct analogue |
| PTZ command → device | **Actuation** (relay, setpoint) | Precedent exists; new risk surface |
| `infra/pairing` + `infra/control` + `infra/fleetca` | Identical | **Verbatim** |
| First-run wizard, secure wipe, backup/restore, machine health, self-update | Identical | **Verbatim** |

### Dropped wholesale

`infra/onvif`, `infra/rtsp`, `infra/stream`, `infra/recording`, `infra/talk`,
`infra/vision`, `infra/mediarelay`, and `apps/mymatasan/ai/*.py`.

That is the majority of `mymatasan`'s *bulk* but only a minority of its *scaffolding*.

### Kept wholesale

`infra/apphost` (the `App` contract), `infra/config`, `infra/db/{sql,bootstrap}` (code-first
ORM, no migration files), `infra/atrest`, `infra/notification`, `domain/notification`
(analytics), `infra/pairing`, `infra/control`, `infra/fleetca`, `infra/discovery`
(ssdp/mdns/portscan), `infra/versioning`, `infra/apidocs`, `frontend/shared` (`@shared` —
DataTable, Tabs, SideNav, charts, i18n, BrandLogo), and the whole GoReleaser / Inno /
nfpm / Docker release pipeline.

---

## 3. The four genuinely new subsystems

### 3.1 Embedded MQTT broker — `infra/iot/mqtt`

Requiring an external Mosquitto or EMQX would break the single-binary, air-gapped promise.
Embed a pure-Go MQTT 5 broker (`mochi-co/mqtt` v2) with its auth/ACL hooks wired to the
`iot_device` table, so a device's credentials *are* its inventory record. Also support a
"connect to an existing broker" mode for sites that already run one.

MQTT client code already exists in-repo (the `infra/notification` MQTT channel, with TLS
and client certs), so the plumbing is half-familiar.

Listens on 1883 (plain, LAN) and 8883 (TLS). Both configurable; TLS on by default.

### 3.2 Telemetry storage — the real technical risk

100 devices x 10 keys x 1 Hz = 1,000 rows/sec. SQLite will not absorb that naively, and
we cannot add a TSDB dependency without breaking the deployment model.

Three mitigations, each with an existing precedent in the codebase:

1. **Deadband on write** — persist a reading only when the value moves more than the key's
   configured deadband (or a heartbeat interval elapses). This is the single biggest lever;
   most building sensors are near-static most of the time.
2. **Write-behind batching** — buffer in memory, batch-insert, WAL mode.
3. **Rollup + retention** — periodic downsample to 1m/1h buckets reusing the
   `rollup_cursor` + `NotificationRollup` pattern; raw-row purge reusing the recording
   purge loops and the machine-health disk mitigation.

### 3.3 `device_profile` — the abstraction mymatasan lacks

A template declaring a device type's telemetry keys, units, datatypes, Modbus register map,
MQTT topic templates, available commands, and default rules.

Without this, onboarding every device is manual and the product does not scale past a
demo. Ship a built-in catalog (Shelly, Tasmota, Zigbee2MQTT, generic Modbus energy meter,
DS18B20, …) exactly like the `detection_class` label catalog, with import/export
(`.iotprofile`) modelled on the `.mmskill` teach-skill export.

### 3.4 Actuation and device twin

Cameras are read-mostly. IoT devices get *written to*. This needs desired-vs-reported state
and an audited command path.

**Be conservative here.** A bad relay write is physically dangerous in a way a bad PTZ move
is not:

- Read-only by default; actuation is an explicit per-device capability toggle.
- Gated by RBAC (write permission is distinct from view).
- Confirm-to-execute in the UI.
- Rate-limited.
- Every command written to `myseliasan`'s existing `audit_log`.

---

## 4. Domain model

Code-first, per `docs/DB_BOOTSTRAP_SPEC.md` — structs in `apps/myiotsan/entities/`, schema
auto-derived, no migration files, data fixes as `bootstrap.NewSQLSeeder` blocks.

| Entity | Notes |
|---|---|
| `iot_device` | Inventory. Mirrors `camera`: host/port/vendor/model/serial/firmware/health/lastSeen/discoveryMethods, plus `protocol`, `endpoint`, `profile_id`, encrypted credentials, `actuation_enabled`. |
| `device_profile` | Device type template (§3.3). |
| `telemetry_key` | Per-profile datapoint definition: key, unit, datatype, deadband, min/max, register/topic binding. |
| `device_reading` | Raw time-series rows. |
| `reading_rollup` | 1m / 1h downsampled buckets. |
| `device_attribute` | Twin: desired vs reported state. |
| `device_command` | Actuation request + result, audited. |
| `iot_rule` | Ported from `detection_rule`. Condition over telemetry (threshold, delta, rate, stuck, missing/offline, boolean combo across devices), plus hysteresis, debounce, cooldown, schedule window. `ZonePolygon` is dropped and replaced by device/tag scope. |
| `alert_event` | Reused as-is. |
| `runtime_setting`, `local_user`, `notification`, `notification_rollup` | Reused as-is. |

---

## 5. Package layout

```
apps/myiotsan/
  app/app.go            # apphost.App composition root (model on apps/myseliasan/app/app.go)
  apis/                 # devices, profiles, telemetry, rules, alerts, commands, settings, setup, pairing
  entities/             # §4
  services/             # device registry, ingest, rule engine, rollup, command dispatch, discovery
  views/react-webpack/  # React SPA off @shared
  config.json, config.dev.json, certs/
cmd/myiotsan/main.go
infra/iot/
  mqtt/                 # embedded broker + client + ACL hook
  modbus/               # TCP/RTU poller (goburrow/modbus)
  http/                 # REST/webhook ingest for push devices
  codec/                # payload decode: JSON path, CBOR, raw, templating
  twin/                 # desired/reported state + command dispatch
```

Protocol library choices (from research):

- **MQTT** — `mochi-co/mqtt` v2 (broker), `eclipse/paho.golang` (v5 client). Mature.
- **Modbus** — `goburrow/modbus`. Mature, TCP + RTU + ASCII.
- **OPC-UA** — `gopcua/opcua`. Production-proven (Telegraf uses it). Phase 5.
- **CoAP** — `plgd-dev/go-coap`. Solid. Phase 5, only if needed.
- **BACnet** — **skip.** Every Go implementation is experimental or abandoned
  (`alexbeltran/gobacnet` self-describes as "should not be used in anything you want
  working"). Revisit only if a customer pays for it; a cgo binding or an external gateway
  process would be the fallback.

---

## 6. Ports

| Purpose | Port | Notes |
|---|---|---|
| HTTPS / SPA | **3003** | myidsan 3001, myseliasan 3002, mymatasan 3000 |
| MQTT | 1883 / 8883 | New. Broker listeners. |
| Fleet pairing (mTLS) | 39532 | **Unchanged** — these are myseliasan's listeners that nodes dial |
| Fleet control channel | 39533 | **Unchanged** |
| Fleet media relay | 39534 | Unused by myiotsan (no media) |

No new fleet port block is required: `myiotsan` dials the same `myseliasan` endpoints that
`mymatasan` does.

---

## 7. Phases

| Phase | Content | Est. |
|---|---|---|
| **P0** | **Scaffolding.** `apps/myiotsan/`, `cmd/myiotsan/`, root `main.go` app map, `version.json` entry, config + dev certs, webpack FE off `@shared`, SPA shell that boots. Purely mechanical (§8). | ~3d |
| **P1** | **Ingest spine.** `iot_device` CRUD, `device_profile` catalog, embedded MQTT broker, HTTP ingest, telemetry store + deadband + rollup + retention, live device page with `@shared/charts` TimeSeriesChart. | ~1.5wk |
| **P2** | **Rules & alerts.** Port `detection_rule` → `iot_rule`, evaluators, `alert_event`, wire existing notification destinations. **P0–P2 is the shippable MVP.** | ~1.5wk |
| **P3** | **Discovery & onboarding.** MQTT autodiscovery (sniff the Home-Assistant topic convention → propose devices), reuse mDNS/SSDP/portscan, Modbus TCP scan, first-run wizard, profile import. | ~1wk |
| **P4** | **Actuation & twin.** Commands, desired/reported, RBAC + audit + confirm (§3.4). | ~1wk |
| **P5** | **Industrial protocols.** Modbus poller, OPC-UA. | ~1.5wk |
| **P6** | **Fleet.** Adoption by myseliasan; `kind` column on `managed_node`; `nodeiot/` embed mirroring the `nodecam/` trick; then cross-domain rules. | ~2wk |
| **P7** | **Release.** GoReleaser / Inno / nfpm / Docker / workflows, k6, ZAP — copy-and-adapt from myseliasan. | ~1wk |

MVP (P0–P2) is realistically **3–4 focused weeks** given the reuse. Full P0–P7 is around
**3 months**.

---

## 8. P0 checklist

**SHIPPED 2026-07-14.** Derived from how `myseliasan` was actually created (commits
`805769a` → `db5df80` → `c5eea80`). Notes on what actually happened vs. the checklist below:

- Item 1 was reworked mid-flight: rather than hand-copy mymatasan's ~1,300 lines of
  local-auth/RBAC code into `apps/myiotsan`, that stack was **extracted to `domain/shared`**
  (`domain/shared/apis`, `domain/shared/services`) first, as its own preceding commit, so
  mymatasan and myiotsan run one implementation instead of two forks. `apps/myiotsan/app/app.go`
  is consequently smaller than a hand-copy would have been — see
  `docs/modules/apps/myiotsan/app/app.go.md`.
- A shared **session-cookie login endpoint** (`domain/shared/apis/local_login_api.go`,
  `POST /api/auth/login`) was added as part of this, ahead of plan — mymatasan's
  Basic-replayed-every-request design costs a bcrypt verification per request (the load-test
  ceiling that forced its bcrypt cache); myiotsan uses the cookie exchange as its primary
  sign-in path instead. Basic still works for API clients.
- A real bug was found and fixed along the way, applying to **every** app, not just myiotsan:
  an unmatched `/api/*` path returned `200 text/html` to an unauthenticated caller instead of
  404 (`infra/apphost/run.go`) — found by live-booting myiotsan. See
  `docs/modules/infra/apphost/run.go.md`.
- Item 9's changelog entries are scoped `mymatasan` (the auth extraction + the `/api` 404 fix)
  and `myiotsan,core` (the new app), not a single `myiotsan,core` entry — the extraction is a
  real, behavior-preserving code move affecting mymatasan's own release, so it needed its own
  app-scoped entry to cut a mymatasan release.
- **Item 8 (docs) and item 9 (changelog) followed the checklist as written**, including one
  doc per source file for every new/moved module.

Everything else below matched the checklist as originally written.

**§8 release plumbing (the "Deferred to P7" list below) is still deferred to P7** — P0 shipped
with no GoReleaser target, no release workflow, no installer packaging, and no Docker release
image for myiotsan. `go run . -app myiotsan` / `go build ./cmd/myiotsan` are the only supported
ways to run it today.

**Go / app**
1. `apps/myiotsan/app/app.go` — implement `apphost.App` (`Name`/`BaseDir`/`Entities`/`Seeders`/`RegisterAppRoutes`) + `SharedAPIs()` + `RegisterWebRoutes` + `APIDocs()`. Model on `apps/myseliasan/app/app.go`.
2. `apps/myiotsan/{apis,entities,services}/`, `apps/myiotsan/README.md`.
3. `apps/myiotsan/config.json` + `config.dev.json` (TLS port 3003), `apps/myiotsan/certs/`.
4. `cmd/myiotsan/main.go`; register `"myiotsan"` in the root `main.go` app map.
5. Add `"myiotsan": {"version": "0.1.0"}` to `infra/versioning/version.json` — required before any `"scope":"myiotsan"` change entry will validate.

**Frontend**
6. `apps/myiotsan/views/react-webpack/` — copy myseliasan's webpack config (`@shared` alias, `modules: [app node_modules]`, **inline** babel presets so files outside the app dir transpile), output → `../../static`. `npm install`, `make web APP=myiotsan`, commit `apps/myiotsan/static/`.
7. Add `'myiotsan'` to `SYSTEM_APP_CODES` in `apps/myidsan/views/react-webpack/src/views/App.js`, and to `sso.audience` in `apps/myidsan/config*.json`.

**Docs / versioning**
8. `docs/modules/apps/myiotsan/**/*.go.md` + `docs/modules/cmd/myiotsan/main.go.md` (one doc per source file — the `docs/README.md` Update Rule).
9. `changes/pending/<ts>-add-myiotsan/change.json` with `"scope": "myiotsan,core"`.

**Deferred to P7** (release plumbing): `.goreleaser.myiotsan.yaml`,
`.github/workflows/release-myiotsan.yml`, `main.yml` tagging (`tag_if_new myiotsan
"myiotsan-v" "iot"`), `installer-check.yml` matrix row, `cmd/myiotsan/versioninfo.json`,
`packaging/windows/myiotsan.{iss,ico,-icon.svg}`, `packaging/stage-archive-myiotsan.sh`,
`deploy/{dist,systemd,nfpm,launchd,windows}/myiotsan.*`,
`deploy/Dockerfile.myiotsan.release`, `.gitignore` entries.

---

## 8b. P1 + P2 — the MVP, shipped

**SHIPPED 2026-07-14.** P1 (ingest spine) and P2 (rules & alerts) landed together as one
change set. `apps/myiotsan/app/app.go`'s `RegisterAppRoutes` now wires the whole spine:

    broker (infra/iot/mqtt) -> ingest (infra/iot/codec -> deadband -> batched write)
                                                              \
                                                               -> rule engine -> alert -> notification

New packages: `infra/iot/codec`, `infra/iot/mqtt`; `apps/myiotsan/{entities,services,apis,config}`
(devices, profiles, telemetry, deadband, rule engine, rules, ingest). See
`docs/modules/apps/myiotsan/**/*.go.md` and `docs/modules/infra/iot/**/*.go.md` for the
per-file detail.

### §9's risk, resolved

The SQLite write-throughput risk (§9's original table, "the one thing that can invalidate the
storage design") is now **measured and settled**, not merely mitigated on paper. On a live
appliance: 20 devices publishing 10,000 MQTT payloads (~30,000 samples) in under a second
produced **540 written rows, 98.2% suppressed by the deadband, ZERO dropped**. The deadband
(`apps/myiotsan/services/deadband.go`) is why — almost everything a building sensor says is
"still 21.4 degrees", and that is not worth a row. **Do not add a TSDB** — it would break the
single-binary, air-gapped deployment model and it is not needed. `GET /api/devices/stats`
exposes stored/suppressed/dropped in production so this stays observable; `dropped > 0` means
the disk has stopped keeping up.

Second load-bearing invariant, also shipped and tested: **rules are evaluated on every decoded
sample, including the ones the deadband suppressed** (`services.RuleService.OnReading`, called
from `services.Ingest.Handle` regardless of the gate's admit decision). The deadband is a
STORAGE decision, not a detection one — a value sitting steadily over a threshold is not worth
another row but is absolutely worth an alert; gating rules behind the deadband would mean a
steady overheat is never alerted on.

### Bugs found by live-booting

1. **The profile seeder panicked the app on first boot.** The generic repo's `Delete` returns
   an ERROR when it matches zero rows ("total affected: 0" — the same pre-existing quirk that
   makes mymatasan log a scary notification-purge warning on fresh installs). A
   delete-then-insert on an empty table is exactly what seeding the builtin device catalog
   does. Fixed by `isNoResultErr` in `apps/myiotsan/services/device.go` treating that message as
   a non-error, shared across the package.
2. **A missed-alert bug in the rule engine.** State was keyed by rule id alone, so a
   TAG-scoped rule watching 20 devices shared one `firing` flag: the first device to trip it
   silently suppressed all the others (one fridge alerts, nine defrost in silence). A missed
   alert is the one failure a monitoring product may never have. Fixed: engine state is now
   keyed by `(rule, device)`, and the per-device cooldown is seeded FROM THE ALERT LOG on every
   `RuleService.Reload` (an alert row already records exactly when a rule last fired on a
   device, so no second table can drift out of step). Pinned by
   `TestRule_TagScopedRuleFiresPerDeviceNotOnce` and `TestRule_CooldownIsPerDevice` in
   `apps/myiotsan/services/rule_engine_test.go`.
3. **The device authenticator distinguishes "could not CHECK the credential" from "the
   credential is wrong."** A database-unreachable error during MQTT auth is logged distinctly
   and refused (fail closed) rather than reported as a bad password — telling an operator "bad
   password" when the database was unreachable sends them to debug the wrong machine.

### What is still not here

Discovery/onboarding (P3), actuation/twin (P4), Modbus/OPC-UA (P5), fleet adoption (P6), and
release packaging (P7, still deferred exactly as scoped in §8) are all unstarted. `mqtt.Connect`
mode (pointing at an external broker instead of the embedded one) is not implemented — only the
embedded broker.

---

## 9. Known risks

| Risk | Mitigation |
|---|---|
| **SQLite write throughput** under telemetry load. | **RESOLVED (2026-07-14), measured, see §8b.** Deadband + batching + rollup (§3.2) shipped in P1. 20 devices / ~30,000 samples in under a second → 540 rows written, 98.2% suppressed, 0 dropped. `GET /api/devices/stats` keeps it observable going forward. Do not add a TSDB. |
| **`frontend/shared/CameraHero.js`** is camera-specific but lives in the *shared* module. | Still open — P1/P2 shipped as a backend MVP with no device-page hero component yet built against it; decide before the device detail UI lands. |
| **Scope creep** into a Home Assistant clone. | Hold the line stated in §1. |
| **BACnet** demand from building-automation customers. | Out of scope; Go libs are not production-grade. External gateway if forced. |
| **Actuation safety.** | Read-only default, RBAC, confirm, audit, rate limit (§3.4). |
| **No scaffolding tooling exists** — every app so far was hand-copied. | Worth writing a small generator during P0, since this is the second fork. |
| `SYSTEM_APP_CODES` and `sso.audience` are hardcoded lists. | Covered explicitly in the P0 checklist (item 7). |

---

## 10. References

- [ThingsBoard](https://thingsboard.io/) — the reference open-source IoT platform (rule chains, device profiles, alarms). Useful for domain vocabulary; wrong deployment model for us.
- [Home Assistant MQTT discovery](https://www.zigbee2mqtt.io/guide/usage/integrations/home_assistant.html) — the de-facto topic convention Zigbee2MQTT/Tasmota/Shelly all emit. Sniffing it is how P3 auto-onboards devices.
- [Home Assistant network discovery](https://developers.home-assistant.io/docs/network_discovery/) — mDNS/SSDP patterns; we already have the equivalents in `infra/discovery`.
- [`mochi-co/mqtt`](https://github.com/mochi-mqtt/server) — embedded pure-Go MQTT 5 broker.
- [`goburrow/modbus`](https://github.com/goburrow/modbus) — Modbus TCP/RTU.
- [`gopcua/opcua`](https://github.com/gopcua/opcua) — OPC-UA.
- [`alexbeltran/gobacnet`](https://github.com/alexbeltran/gobacnet) — BACnet; explicitly experimental, hence deferred.
