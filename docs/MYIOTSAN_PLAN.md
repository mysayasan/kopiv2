# MyIotSan — Implementation Plan

Status: **P0 SHIPPED** (2026-07-14). **P1 (ingest spine) + P2 (rules & alerts) SHIPPED
(2026-07-14) — P0-P2 is the shippable MVP.** **P3 (discovery & onboarding) SHIPPED (2026-07-14)
— see §8c for what shipped and what was deliberately deferred.** **P4 (actuation & device twin)
SHIPPED (2026-07-14) — see §8d, including a deviation from §3.4's audit-log design and a real
unassignable-roles gap found and closed.** **P6 (fleet adoption + cross-domain rules) SHIPPED
(2026-07-14) — see §8e. `myiotsan` is now an adoptable `myseliasan` fleet node, and the control
plane can correlate its events against a `mymatasan` camera's — THE reason this fourth app
exists, and the payoff §1's differentiator promised.** **P7 (release) SHIPPED (2026-07-15) — see
§8f: `myiotsan` is now an installable product (portable archives, .deb/.rpm, Windows Inno, Docker,
release CI, k6, ZAP), and the `nodeiot/` embed deferred out of P6 also shipped in the same pass
(§8f) — `myseliasan` can now manage an adopted `myiotsan` node's devices/rules/alerts/commands
directly, the way it already does for a `mymatasan` camera node.** Only **P5** (Modbus/OPC-UA)
remains.

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

**SHIPPED (P6, 2026-07-14) — see §8e.** `myseliasan` already adopted `mymatasan` camera nodes;
it now also adopts `myiotsan` sensor nodes, and it can express **cross-domain rules** that no
competitor can:

> *Motion on Camera 3* **and** *door contact opened* **and** *no badge swipe* → alert.

ThingsBoard cannot see your cameras. `mymatasan` cannot see your door sensors. This
correlation is the reason the fourth app exists, and every earlier phase's decisions kept the
path to it open on purpose. Verified live end-to-end against real events from an adopted
camera node and an adopted sensor node — see §8e, including the late-badge-swipe case that
proves the grace-period design actually stays silent on a legitimate entry.

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

**Shipped 2026-07-14 (§8d) — this last point deviated.** myiotsan is a standalone appliance that
may never be adopted into a `myseliasan` fleet at all, so it cannot depend on `myseliasan`'s
`audit_log` existing; the audit trail is myiotsan's own `device_command` table (every attempt,
including refusals) plus the notification feed instead. Everything else above shipped as
specified, plus one property the plan did not name: an unconfirmed command is never auto-retried
(a retry is a second physical action — see §8d).

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
| **P3** | **Discovery & onboarding — SHIPPED 2026-07-14, see §8c.** Time-boxed enrollment window + quarantined candidate capture + admin adoption, profile-match suggestion, profile import/export, first-run wizard. mDNS/SSDP/portscan and a Modbus TCP scan deliberately deferred to P5 (see §8c). | ~1wk |
| **P4** | **Actuation & twin — SHIPPED 2026-07-14, see §8d.** Commands, desired/reported, RBAC + audit + confirm (§3.4). | ~1wk |
| **P5** | **Industrial protocols.** Modbus poller, OPC-UA. | ~1.5wk |
| **P6** | **Fleet — SHIPPED 2026-07-14, see §8e.** Adoption by myseliasan; `Kind` column on `managed_node`; cross-domain correlation rules. A dedicated `nodeiot/` embed mirroring `nodecam/` (full remote device-management pages inside myseliasan, the way camera pages are embedded today) was **not** built in this pass — deliberately deferred to P7; see §8e "What was deliberately NOT built" and §8f. | ~2wk |
| **P7** | **Release — SHIPPED 2026-07-15, see §8f.** GoReleaser / Inno / nfpm / Docker / workflows, k6, ZAP — copy-and-adapt from myseliasan. The deferred `nodeiot/` embed also shipped in this pass. | ~1wk |

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

Modbus/OPC-UA (P5) and release packaging (P7, still deferred exactly as scoped in §8) are
unstarted. `mqtt.Connect` mode (pointing at an external broker instead of the embedded one) is
not implemented — only the embedded broker. (Actuation/twin, P4, fleet adoption + cross-domain
rules, P6, and release, P7, have since shipped — see §8d, §8e and §8f. P5's driver foundation and
its app integration have since partially landed too — see §8g.)

---

## 8c. P3 — Discovery & onboarding, shipped

**SHIPPED 2026-07-14.** The design problem P3 exists to solve: the broker's security model is
that a device not in `iot_device` cannot connect at all, which is what makes the device table
the credential store and makes deleting a device actually revoke it. But a building's two
hundred door contacts cannot be onboarded by typing two hundred device keys by hand, and a
device that does not exist yet cannot announce itself. Those two facts are in direct tension.

The resolution is a deliberate, time-boxed hole with four load-bearing properties, all shipped:

1. **TIME-BOXED.** An admin opens an enrollment window (`POST /api/discovery/window`); it
   expires on its own — TTL clamped to 1 hour server-side, default 10 minutes. There is no way
   to leave it open by forgetting about it, which is how every "temporary" provisioning mode
   ends up permanent.
2. **SECRET-GATED.** The window mints a one-time key with real entropy (24 bytes of
   `crypto/rand`), returned exactly once, bcrypt-hashed at rest, never readable back.
   Re-opening replaces it, so two keys can never be valid at once.
3. **QUARANTINED — the property that makes the hole safe to open.** An unknown client
   presenting the key is admitted (`infra/iot/mqtt.Principal.Enrolling`), but its payloads are
   recorded as a `DiscoveredDevice` candidate and NOWHERE else: no telemetry row is stored and no
   rule is evaluated. Somebody who slips into the window can leave junk candidates for an admin
   to decline; they cannot forge a sensor reading, move a chart, or trigger or suppress an alert.
   **Verified live:** an unknown device published 5 messages through an open window and the
   appliance stored 0 telemetry rows, 0 devices, 0 alerts.
4. **CAPPED + AUDITED.** A 500-candidate cap so a flood cannot fill the disk; opening a window
   publishes a security event to the notification feed so it cannot be done quietly.

Adoption is the deliberate act: the admin sees what the thing actually sends, is told what it
probably is (the profile suggester — see below), and mints it a real credential via
`POST /api/discovery/candidates/{id}/adopt`. **The enrollment key then stops working for that
device** — it is now a known device requiring its own permanent, generated password, so a leaked
window key cannot impersonate anything it let in.

### The profile suggester

Adoption suggests a device type by matching the observed payload's field names against each
profile's telemetry keys. Score = the fraction of the **profile's** keys the device actually
sent (so a profile that merely shares "battery" with everything does not win), plus a small
bonus if the topic prefix matches. **Below a 0.6 floor it suggests nothing** — a wrong
suggestion an installer accepts without thinking silently mis-decodes every reading that device
will ever send, which is worse than no suggestion. **Verified live:**
`{battery, humidity, linkquality, temperature}` correctly suggested "Temperature / humidity
sensor".

### Profile import/export

A device type is a small declarative document, so it is portable. The point is not backup:
tuning a deadband for a particular sensor in a particular building is real work, and an
integrator who does it once should carry it to the next site — the same reasoning as
mymatasan's `.mmskill`. Shipped guarantees: an imported profile is **never** builtin whatever
the document claims; a slug collision is **reported, not silently overwritten** (quietly
replacing a profile would re-point every device using it at different decoding rules — data
corruption wearing the costume of a successful import); an unknown format version is refused
rather than guessed at. `GET /api/profiles/{id}/export`, `POST /api/profiles/import`
(`apps/myiotsan/services/profile_transfer.go.md`).

### What was deliberately NOT built

The original P3 line in §7 also listed mDNS/SSDP/portscan and a Modbus TCP scan. Both were
**deliberately deferred, not forgotten:**

- **mDNS/SSDP/network portscan.** MQTT sensors announce over MQTT, not mDNS — a network scan
  would find gateways and hubs rather than the sensors themselves, which is a half-useful
  feature at best. The enrollment window covers the real onboarding path for MQTT devices.
- **Modbus TCP scan.** Belongs with the Modbus poller in P5, where it can be tested against an
  actual device rather than built speculatively against a protocol this app does not speak yet.

### New surface

- `apps/myiotsan/entities/discovered_device.go` — the candidate table.
- `apps/myiotsan/services/enrollment.go` (+ `enrollment_test.go`) — the window, quarantine,
  profile suggester, adopt/reject.
- `apps/myiotsan/services/profile_transfer.go` — profile import/export.
- `apps/myiotsan/apis/discovery.go` — `/api/discovery/*`, ADMIN-ONLY end to end
  (`services/rbac.go`) since opening the window is the one act that lets an unknown thing talk to
  the broker at all.
- `infra/iot/mqtt/broker.go` — the `Authenticator` seam now returns a `Principal{DeviceId,
  Enrolling}` instead of a bare device id, and `MessageHandler` receives it; `Enrolling` marks
  the quarantined session all the way through the broker hook, `DeviceService`, and `Ingest`.
- Frontend: a Discovery page and a first-run onboarding wizard
  (`views/react-webpack/src/views/components/discovery.js`, `.../onboarding.js`).

See `docs/modules/apps/myiotsan/**/*.go.md` and `docs/modules/infra/iot/mqtt/broker.go.md` for
per-file detail.

---

## 8d. P4 — Actuation & device twin, shipped

**SHIPPED 2026-07-14.** The design problem P4 exists to solve: every prior phase only *reads*
a device. Actuation is where a bug stops being a wrong number on a chart and becomes a relay
that physically fires — a door unlocks, a breaker trips, a thermostat is set to 200°C. A camera
is read-mostly; an IoT device gets *written to*. Every decision below follows from that.

### The five gates from §3.4, all enforced SERVER-SIDE (`apps/myiotsan/services/commands.go`)

1. **Read-only by default.** A device cannot be commanded unless `IotDevice.ActuationEnabled`
   was explicitly turned on for it. Adoption never sets it.
2. **Admin only.** Not an operator power. This rule was written into the policy catalog in P0,
   *before* the command path itself existed.
3. **Only declared commands.** A device can be told exactly what its profile declares
   (`ProfileCommand`) and nothing else. There is deliberately no generic "publish this payload to
   that topic" endpoint anywhere in the app — that would be a remote shell for the building's
   electrics.
4. **Bounds are server-side.** A setpoint outside the profile's `Min..Max` is refused in the
   service, not merely blocked in the UI — "the frontend validates it" is not a safety property.
   A setpoint declaring NO range (`Min == 0 && Max == 0`) refuses EVERY value: an unbounded
   setpoint on a physical device is an omission, and the safe reading of an omission is no.
5. **Rate-limited (2s floor per device) and audited.** A relay has a duty cycle; something that
   can chatter it can destroy it. Every attempt — INCLUDING every refusal — is a `device_command`
   row naming the actor, plus a notification. "Somebody tried to unlock the front door at 03:00
   and was refused" is exactly what must not be thrown away just because it failed.

### Two hazards the original plan did not name

**A. A command is never auto-retried.** Re-sending a relay write is a SECOND PHYSICAL ACTION. If
the first one landed but its confirmation was lost, a retry fires the relay again — the door
opens twice — and nothing at this layer can distinguish that from the first one never arriving.
So an unconfirmed command becomes `failed` after 30s (`SweepUnconfirmed`, swept every 10s), with
an error that says plainly *"the device never reported the new state — it may or may not have
acted. Not retried automatically: re-sending could act twice."*, and the decision is left to a
human. **Verified live** with a relay simulator that obeys but never reports back: it physically
switched, the command was recorded `failed`, and exactly ONE command was ever sent.

**B. Desired state expires (5 minutes) and is never re-applied.** The obvious twin
implementation re-applies desired state whenever a device reconnects — fine for a light bulb,
dangerous here. A door controller offline for a month would come back and immediately apply a
month-old "unlock" somebody issued for thirty seconds during a delivery, with nobody watching —
the door opens. So an expired desire is NOT re-applied, though the disagreement is still SHOWN
(`GET /api/devices/{id}/twin`) — an operator must see that what they asked for never took effect.

**C. "Sent" is not "done".** `confirmed` is the only `DeviceCommand` status meaning the physical
thing actually happened, and it is set only when the device reports the resulting state back on
the profile's `ConfirmKey` (`services.CommandService.OnReported`, wired into `services.Ingest`'s
hot path for every decoded sample). Publishing a message is a different fact from a relay
closing.

### Deviation from §3.4: the audit trail is NOT `myseliasan`'s `audit_log`

§3.4 originally said "every command written to `myseliasan`'s existing `audit_log`." That is
**not** what shipped, and deliberately: at the time P4 shipped, `myiotsan` was a standalone
appliance that might never be adopted into a `myseliasan` fleet at all (fleet adoption was still
P6, unstarted), so the command audit trail could not depend on a table that might not exist.
What shipped instead is myiotsan's own `device_command` table (§4) plus the unified
notification feed (`infra/notification.CategorySystem`) — the same pattern the rest of the app
already uses for its own security events (an enrollment window opening, a login lockout).

**This held even after P6 shipped (§8e).** A `myiotsan` node's own `device_command` table
remains its authoritative command audit trail regardless of fleet membership — adoption only
additionally surfaces its notifications (including command refusals) in the control plane's
unified feed via the fleet event sink, exactly the mechanism this section anticipated; it does
not migrate or duplicate the audit record itself, and `myseliasan` never depends on
`myiotsan`'s schema.

### A real gap found and closed: unassignable roles

`/api/settings/roles` and `/api/settings/users` **404'd**. The policy catalog
(`apps/myiotsan/services/rbac.go`) has named those routes since P0, and the `viewer`/`operator`
roles have existed since then — but nothing served them, so the roles were unassignable and the
appliance was effectively single-admin. That is exactly the lie the catalog exists to prevent
("a catalog that names routes the app does not serve is a lie an operator would rely on").
`apps/myiotsan/apis/settings.go` now serves them on the shared appliance user service
(`domain/shared/services`) — the same code `mymatasan` uses, so bcrypt/sessions/the last-admin
guard are one implementation, not two. **Verified live:** operator and viewer get `403` on
actuation, `200` on the command history and twin.

### The one profile that can act on the world

Of the eight built-in device-type profiles, only `smart-relay` (Shelly/Tasmota conventions)
declares a command — every other shipped profile remains read-only. Its `output` command
publishes a Shelly-style `Switch.Set` RPC and is confirmed by the device's own `output`
telemetry key reporting back.

### New/changed surface

- `apps/myiotsan/entities/{profile_command,device_command,device_attribute}.go` — the
  declaration, the audit record, and the twin.
- `apps/myiotsan/services/commands.go` (+ `commands_test.go`) — every gate, the twin, the
  unconfirmed-command sweep.
- `apps/myiotsan/apis/commands.go` — `/api/devices/{id}/commands`, `/commands/history`, `/twin`.
- `apps/myiotsan/apis/settings.go` — `/api/settings/{users,roles}` (the gap above).
- `apps/myiotsan/services/{profile,profile_catalog}.go` — profiles now declare commands; the
  catalog gained `smart-relay`.
- `apps/myiotsan/services/ingest.go` — every reading also updates the twin's reported half.
- `apps/myiotsan/services/rbac.go` — `/api/devices/*/commands` admin-only;
  `/api/devices/*/commands/history` and `/api/devices/*/twin` readable by viewer and operator
  (seeing what was done is not the same power as doing it).
- `apps/myiotsan/app/app.go` — command service, twin wiring, the 10s unconfirmed sweep, the new
  entities, `NewSettingsApi`.

See `docs/modules/apps/myiotsan/**/*.go.md` for per-file detail.

---

## 8e. P6 — Fleet adoption + cross-domain rules, shipped

**SHIPPED 2026-07-14.** This is the phase the whole plan pointed at from §1: `myiotsan` becomes
adoptable, and once `myseliasan` holds events from both a camera node and a sensor node, it can
correlate across them.

### The node-side stack was extracted first, not copied

`myiotsan` needed exactly what `mymatasan` already had: LAN discovery, adoption by claim code,
mTLS enrollment, the node-dialed control channel, and the event sink — about 1,400 lines. Two
copies of that would be two copies of a SECURITY PROTOCOL, drifting the first time one was
fixed. It moved to `domain/shared/fleetnode` (+ `domain/shared/apis/{pairing,control_dispatch}`,
+ `domain/entities.RuntimeSetting` joining `LocalUser` as a shared appliance entity, same
load-bearing struct-name constraint). `mymatasan`'s call sites are unchanged behind same-named
aliases (`apps/mymatasan/{services/fleetnode.go,apis/fleet.go,entities/runtime_setting.go,
services/backoff.go}`); its tests pass untouched. See
`docs/modules/domain/shared/fleetnode/doc.go.md`.

### A node now has a `Kind`, and it travels two different ways on purpose

The control plane manages two sorts of appliance and they are not interchangeable — a camera
node has recordings and live views; a sensor node has telemetry and relays.

- The discovery announce carries `Kind` as an **advisory, unsigned** hint
  (`infra/pairing.Announce.Kind`) — deliberately excluded from the HMAC. Signing it would break
  every already-deployed `mymatasan` node the instant a control plane upgraded: the node signs
  without the field, the parent verifies with it, and the whole fleet would silently stop being
  discoverable.
- The adopt reply carries it **authoritatively** (`fleetnode.AdoptResult.Kind`), over a call
  that is fleet-key-signed and claim-code-gated — and that is the value `myseliasan` stores
  (`ManagedNode.Kind`).
- So a hostile host on the LAN can make a fake node show the wrong icon in a scan list. It
  cannot make the control plane adopt anything, and it cannot change what an adopted node is.
- **Empty means camera** — every node adopted before this field existed is a `mymatasan` node
  and keeps behaving exactly as it always did.

### `myiotsan` joins the fleet — two channels, not three

`apps/myiotsan/app/wire_fleet.go` mirrors `mymatasan`'s own `wire_fleet.go` with one deliberate
structural difference: `mymatasan` dials a third channel (media, for camera RTP);
`myiotsan` has no video, so it does not open that port at all. An unused listener is an unused
attack surface. Everything is node-dialed — the control plane never needs a route back, which is
what lets a node sit behind NAT with no inbound firewall rule.

`openFleetSecretCipher` in `apps/myiotsan/app/app.go` mirrors `myseliasan`'s own boot sequence:
the fleet key/token/mTLS key are encrypted at rest, and on `atrest.ModeRecoveryPending` it
**fails closed** — refuses to boot rather than mint a replacement key and silently un-adopt the
node.

### The correlator — why the fourth app exists

    motion on Camera 3 (a mymatasan node)
    AND a door contact opening (a myiotsan node)
    AND no badge swipe (a myiotsan node)
    -> intrusion

No node can see that. `mymatasan` cannot see your door sensors; `myiotsan` cannot see your
cameras; a cloud IoT platform can see neither. Only `myseliasan` — which already receives every
node's events in one feed — is in a position to notice the conjunction. And the conjunction is
where the signal is: a camera's motion alert at 03:00 is a moth; a door contact at 03:00 is a
cleaner; the two together with no badge swipe is an intrusion. Correlation is how a fleet of
noisy sensors becomes one trustworthy signal.

`apps/myseliasan/entities/fleet_rule.go` (`FleetRule` + `FleetRuleClause`, each clause
`"required"` or `"absent"`) and `apps/myseliasan/services/correlate.go` (`Correlator`) implement
it — see `docs/modules/apps/myseliasan/services/correlate.go.md` for the full mechanics.

**The grace delay is the hard part.** A badge reader reports over a network, through a
controller, into a hub — it is routinely a second or two BEHIND the door contact it just
authorised. Fire the moment the door opens and the rule cries intrusion on EVERY legitimate
badge entry, all day, until somebody turns it off — and then the one real intrusion is not
alerted on either. So the correlator NEVER fires on an event: when the required clauses are
satisfied it **arms**, waits out the grace period, and only then asks whether the absence really
held. A badge swipe arriving inside the grace period **disarms** it. An absence you have not
waited for is not an absence — it is a race with the badge reader. Ten tests in
`correlate_test.go` pin this; the late-swipe one is the one that matters.

Other invariants:

- The correlator is fed the NODE's events, never the control plane's own re-published copy
  (`apps/myseliasan/app/correlate_bridge.go`). Correlating on our own output would let one
  fleet rule's alert satisfy another's clause, and two rules could trigger each other forever.
  Events come from nodes; conclusions come from here.
- A node's kind is resolved from the ADOPTED NODE'S RECORD (`ManagedNode.Kind`), never from
  anything in the event body — otherwise a door sensor could claim to be a camera and satisfy a
  camera-scoped clause.
- A rule made only of "absent" clauses is REFUSED (at save time and again defensively at
  evaluation time): it would fire on nothing at all, forever, and a rule that fires on nothing
  is worse than no rule because somebody will trust it.
- Writing a fleet rule (`POST`/`DELETE /api/fleet-rules`) is a SUPERADMIN power — a correlation
  rule is an estate-wide security control, and whoever can write one can write one that never
  fires. Reading (`GET /api/fleet-rules`) is open to any authenticated session.

### Live verification

Three apps run together: `myseliasan` + an adopted `mymatasan` node + an adopted `myiotsan`
node. Both adopted; both showed the correct kind in the fleet UI. Real events from both nodes (a
sign-in lockout on the camera node; a door alert from a real MQTT reading on the sensor node) →
**the cross-domain rule fired**. Replayed with the badge swipe arriving 3 seconds late → **it
disarmed and stayed silent**.

Also found only by live-booting, not by tests: the fleet block had never been inserted into
`myiotsan`'s `app.go` — a silent string-replace miss. The build was green and every unit test
passed, because nothing exercised the actual route table; the node simply had no pairing
routes. Caught by a `404` on `/api/pairing/fleet-key`. See
`docs/modules/apps/myiotsan/app/app.go.md` "Fleet (P6)".

### What was deliberately NOT built (in this phase — it shipped in P7, see §8f)

A dedicated `nodeiot/` embed mirroring `myseliasan`'s `nodecam/` trick — full remote
device-management pages for an adopted `myiotsan` node rendered inside `myseliasan`, the way a
camera node's Live View/Detection/Recordings/Settings tabs are today — was scoped in the
original §7 phase table but not built in this pass. What shipped instead is the piece that
actually delivers the differentiator: fleet adoption plus the correlator. A `myiotsan` node's
own UI remains the way to manage it directly; `myseliasan` sees its kind, its liveness, and its
events (feeding the correlator), but not yet an embedded device/telemetry management surface.

### New/changed surface

- `domain/shared/fleetnode/{doc,pairing,enrollment,control_channel,event_sink}.go` (+ tests) —
  moved from `apps/mymatasan/services/{pairing,node_enrollment,control_channel,
  control_event_sink}.go`.
- `domain/shared/apis/{pairing,control_dispatch}.go` — moved from `apps/mymatasan/apis/`.
- `domain/entities/runtime_setting.go` — shared appliance entity, joins `LocalUser`.
- `apps/mymatasan/{services/fleetnode.go,services/backoff.go,apis/fleet.go,
  entities/runtime_setting.go}` — thin aliases keeping mymatasan's call sites unchanged.
- `infra/pairing/{packet.go,prober.go}` — `Announce.Kind` / `AnnounceInfo.Kind` /
  `ProbeResult.Kind` (advisory, unsigned).
- `apps/myiotsan/app/wire_fleet.go` — the node's two-channel fleet wiring; `apps/myiotsan/app/app.go`
  — the block that registers it.
- `apps/myseliasan/entities/managed_node.go` — `Kind` field; `apps/myseliasan/services/node_registry.go`
  — stores the authoritative kind, `DiscoveredNode.Kind` carries the advisory hint.
- `apps/myseliasan/entities/fleet_rule.go`, `apps/myseliasan/services/{correlate.go,
  correlate_crud.go,correlate_test.go}`, `apps/myseliasan/apis/fleet_rules_api.go`,
  `apps/myseliasan/app/correlate_bridge.go` — the correlator and its HTTP surface.
- `apps/myseliasan/app/app.go` — correlator wiring, the 1s sweep ticker, `onNodeEvent` now also
  calls `observeForCorrelation`.

See `docs/modules/domain/shared/fleetnode/**/*.go.md`,
`docs/modules/apps/myiotsan/app/wire_fleet.go.md`, and
`docs/modules/apps/myseliasan/{entities,services,apis,app}/**/*.go.md` for per-file detail.

---

## 8f. P7 — Release plumbing + the deferred `nodeiot/` embed, shipped

**SHIPPED 2026-07-15.** Two pieces landed together: `myiotsan` becomes an installable product,
and the `nodeiot/` embed deferred out of P6 (§8e) shipped in the same pass.

### Release plumbing — copy-and-adapt from myseliasan, with one hazard myseliasan didn't have

`.goreleaser.myiotsan.yaml`, `.github/workflows/release-myiotsan.yml`,
`packaging/stage-archive-myiotsan.sh`, `packaging/windows/myiotsan.{iss,ico,-icon.svg}`,
`cmd/myiotsan/versioninfo.json`, `deploy/Dockerfile.myiotsan.release`, `deploy/README-myiotsan.md`,
`deploy/dist/myiotsan-config.json`, `deploy/systemd/myiotsan.service`,
`deploy/nfpm/myiotsan*.{service,sh}`, `deploy/launchd/com.mysayasan.myiotsan.plist`,
`deploy/windows/myiotsan.winsw.xml`, `tools/k6/{config,scripts}/myiotsan*`,
`tools/zaproxy/{config,plans}/myiotsan*` — plus changes to `.github/workflows/{main.yml,
installer-check.yml}`, `.gitignore`, `tools/k6/{run.sh,run.ps1,scripts/lib/session.js}`,
`tools/zaproxy/{scan.sh,scan.ps1}` to add `-App myiotsan` support.

The decisions worth a future maintainer NOT undoing:

1. **A `myiotsan` release must never be GitHub's "latest".** It publishes under its own
   `myiotsan-v<ver>` tag namespace with `gh release create --latest=false`. `mymatasan`'s in-app
   updater reads `releases/latest` and matches assets by a `mymatasan_` prefix; that prefix guard
   stops a node **overwriting itself** with the wrong product, but it does nothing to stop
   **starvation** — if a `myiotsan-v…` tag ever became "latest", the updater would find no
   matching asset and every deployed camera node would silently stop receiving updates. Own
   namespace + `--latest=false` is what prevents that.
2. **GoReleaser OSS cannot derive a semver from a prefixed tag** (splitting the product prefix
   off a tag to get a version is a GoReleaser Pro feature; this repo is monorepo/OSS). Worked
   around with `GORELEASER_CURRENT_TAG` (the actual version, tag prefix stripped) feeding
   GoReleaser, `release: disable: true` in the yaml so GoReleaser never touches GitHub releases
   itself, `--skip=validate` (validation assumes an unprefixed tag), and a separate
   self-publish step that runs `gh release create myiotsan-v<ver> --latest=false` directly.
3. **The shipped config carries NO default admin password.** `deploy/dist/myiotsan-config.json`
   ships `localAuth.password` empty; the app generates one per install and writes
   `INITIAL_ADMIN_LOGIN.txt`. A shipped default credential on an appliance that can switch relays
   is a fleet-wide backdoor, not just a weak-password finding. Pairing ports (39532/33/34) are
   pinned explicitly in the shipped config — a previous release cycle shipped stale ports in
   `deploy/dist` and parent+node silently never reconnected.
4. **MQTT `1883/tcp` is a new listening port** — no other product in this repo opens one. The
   Windows Inno installer (`packaging/windows/myiotsan.iss`) adds a firewall rule for it; without
   one, the UI comes up clean and no device ever connects, which presents to an operator as
   "broken devices", not "blocked port".
5. **The ZAP plans deliberately exclude `/api/devices/*/commands`**, device-password rotation,
   pairing, and enrollment (`tools/zaproxy/plans/myiotsan-{api,baseline,full}.yaml`). `myiotsan`
   actuates PHYSICAL hardware — an active scanner fuzzing an actuation endpoint is a physical
   risk (it can fire a relay), not a data one, and a "throwaway" bench instance may still be
   wired to a real device. This is a deliberate exclusion, not a coverage gap to "fix" later.
6. **`myiotsan` has no self-updater.** Updates are manual / package-manager-driven
   (`deploy/README-myiotsan.md` says so explicitly) — unlike `mymatasan`, there is no in-app
   updater checking GitHub releases.
7. **The k6 scripts (`tools/k6/scripts/myiotsan-{smoke,load,stress}.js`) drive the HTTP console
   only.** `myiotsan`'s real throughput risk is MQTT→SQLite ingest (§3.2, §9), which k6 cannot
   drive — it isn't an HTTP client. A green k6 run says the console holds up under viewer/operator
   load; it says nothing about whether the box keeps up with its device estate. Use
   `GET /api/devices/stats` (deadband suppression / dropped counters) for that.

### The deferred `nodeiot/` embed

`apps/myseliasan/views/react-webpack/src/views/components/nodeiot/**` +
`node_iot_manager.js`; `node_manager.js` now routes `kind === 'iot'` to it (alongside the existing
`kind === 'camera'` route to `nodecam/`). It is the same trick as the `nodecam/` embed (§8e's
"what was deliberately NOT built", now closed): the `myiotsan` node app's own device/rules/alerts/
commands UI components are copied into `myseliasan`'s frontend, `apiBase()` is overridden inside
the embed so every call routes through the commander proxy (`/api/nodes/{id}/proxy/...`) instead
of a direct URL, and the node's CSS is scoped to the embed's container — concatenating the
`@shared` stylesheets in too, because a scoped/shadow rendering context does not inherit the host
document's stylesheets, so anything styled via `@shared` would otherwise render unstyled.

**The browser never talks to the node directly** — every call goes browser → `myseliasan` →
fleet control channel → node. That is what lets an adopted `myiotsan` node sit behind NAT with no
inbound firewall rule, same as an adopted `mymatasan` camera node.

With this, `myseliasan` can manage an adopted `myiotsan` node's devices, rules, alerts, and
commands directly from the control plane, closing the gap §8e left open.

---

## 8g. P5 + P8 — Industrial protocols and the solar "system workspace" (driver foundation + app integration landed; control writes, RTU/OPC-UA, and the workspace remain)

Prompted by a request to make myiotsan **handle solar systems** without writing code per inverter
model: use the protocols we have, and combine them into a **reusable template workspace** for a
specific model (customisable, and re-usable for future protocol sets). The study below is the
agreed direction; the **driver + simulator foundation is built and proven**, **app integration has
now partially landed (2026-07-15) — see below** — and **guarded Modbus control, RTU/OPC-UA, and
the workspace layer (P8) remain**.

### The shape of the problem, and why the current model doesn't fit

A solar system is not one device. It is an **inverter + battery/BMS + charge controller + grid &
PV meters**, and those speak **different wire protocols** (an inverter is usually Modbus/SunSpec, a
meter might be a Shelly on MQTT, a bridge might push HTTP). The meaningful numbers —
self-consumption, net grid power, battery autonomy, PV yield — are **derived across devices**, and
the useful rules are **system-level**. Today's `device_profile` is one device / one protocol
(MQTT-topic + JSON-path only; `iot_device.Protocol` is a `mqtt|http` string; the codec is
JSON-only). So two things are missing, and the design is **two layers**:

### Layer A — a protocol-driver seam (make protocols work *with each other*)

Everything downstream of `decode → []codec.Sample` (deadband, storage, rollup, rules, twin,
dashboards) is already protocol-blind. The seam is therefore: **normalise every protocol to a
`codec.Sample` stream.** Concretely:

- **A driver abstraction.** Push drivers (MQTT/HTTP, today) subscribe and emit; poll drivers
  (Modbus/OPC-UA) run a poll loop and emit the *same* `codec.Sample`. Both feed a shared
  `handleSamples(dev, samples)` — the back half of `Ingest.Handle` (`ingest.go:149-187`) extracted
  into a reusable method. The deadband still governs storage; the poll interval governs bus load.
- **A protocol-agnostic key binding.** `telemetry_key.go` today has only `JsonPath`. It needs a
  per-protocol binding: for Modbus `{register, kind (u16/i16/u32/i32/acc32), scaleFactor,
  wordOrder, bitIndex}`, for OPC-UA a `nodeId`. **Sign and scale live here** — the classic solar
  footgun (import-positive vs export-positive, charge vs discharge).

**SunSpec is the "don't code per model" unlock.** SunSpec is *not a protocol* — it is a standard
**information model over Modbus**: a device publishes the marker `SunS` at a base register, then a
self-describing chain of models (`[id][length][data]…[0xFFFF]`) standardised by id. Walk the chain
and one driver decodes **any** compliant inverter/meter/battery with no per-model map. The
non-compliant vendors (many cheap hybrids) get a **manual register map** in their profile — still
data, not code.

#### Built and proven (2026-07-15) — the foundation, in `tools/` and `infra/iot`

- **`tools/sunspec-sim/`** — a SunSpec-over-Modbus-TCP simulator, stdlib-only (no Modbus dep; the
  TCP framing is ~150 lines). It serves a **real** SunSpec chain (Common 1 / Inverter 103 /
  Controls 123 / Storage 124 / Meter 203) driven by a live physics loop (PV bell curve, battery
  charge/discharge, grid balance, curtailment) over a compressed day. It is the `-deaf`-relay
  equivalent for the Modbus path. Control writes are honoured, so the read-back a guarded write
  confirms against actually changes. It serves **three devices on three Modbus unit ids** to
  exercise the *mixed-protocol* case: **unit 1** = SunSpec hybrid inverter, **unit 2** = a
  standalone SunSpec meter (a different, shorter chain), **unit 3** = a **non-SunSpec vendor
  inverter** exposing a flat vendor register block (no `SunS`). `models_test.go` pins every model's
  block length to the spec (a shifted field silently corrupts every downstream value).
- **`infra/iot/sunspec/`** — the SunSpec decoder: `Discover` (tries base 40000/50000/0), `Walk`
  (follow the chain by length), a registry of the standard integer models (101/102/103,
  201–204, 124, 123) with **scale-factor arithmetic**, and `DecodeDevice`, which **role-prefixes**
  every key (`inv_ac_power`, `grid_ac_power`, `batt_soc`, `ctl_w_max_lim_pct`) so a hybrid
  inverter's own inverter block and its built-in grid meter don't collide on `ac_power`.
- **`infra/iot/modbus/`** — a stdlib Modbus TCP client (fn 3/4/6/16), a `RegisterMap` for the
  non-SunSpec path (one round trip covers the whole span), a `Poller` that normalises **either**
  path to `codec.Sample`, and **`WriteConfirm` — a guarded write with read-back that NEVER
  re-issues the write** (a Modbus write to an inverter/battery is a physical action; a lost
  confirmation must not become a second one — the same rule the MQTT actuation path already
  enforces).
- Verified live: `MODBUS_SIM_ADDR=127.0.0.1:1502 go test ./infra/iot/modbus/ -run Live` read all
  three personas over real Modbus and confirmed a curtailment write by read-back. Hermetic unit
  tests (synthetic register banks) cover the decoding and scaling deterministically.

#### App integration — items 1-3 LANDED (2026-07-15), items 4-5 still to land for P5

1. **LANDED.** `telemetry_key.go` gained the Modbus binding fields (`Register`, `RegKind`
   (`u16`/`i16`/`u32`/`i32`), `ScaleFactor`, `WordSwap`); `device_profile.go` gained `Transport`
   (`""`/`"mqtt"` default, or `"modbus"`), `ModbusMode` (`"sunspec"` or `"regmap"`), `ModbusBase`
   (SunSpec base register, `0` = auto-discover), and `PollSeconds` (poll cadence, `0` = 5s
   default) — so a profile can describe a SunSpec or a vendor-map device without a second entity.
   `iot_device.go` gained `Endpoint`/`Unit` for the device the app now dials OUT to (an MQTT
   device is still addressed by its `DeviceKey` instead). All plumbed through the profile/device
   CRUD, import/export, and builtin-seeding paths (`services/profile.go`,
   `services/profile_transfer.go`, `services/profile_catalog.go`, `services/device.go`).
2. **LANDED.** `Ingest.Handle`'s back half was extracted into `handleSamples(dev, binds, samples,
   nowMs, nowSec)` — deadband → storage → rules → twin, unchanged — and a new
   `Ingest.HandlePolled(dev, samples)` feeds it from a POLL driver: `TouchSeen` first
   unconditionally (same liveness rule as the MQTT path), no payload to parse and no enrollment
   quarantine to apply (a polled device is one the operator configured, not a stranger that
   dialled in). `apps/myiotsan/services/modbus_poller.go` (new) is the poller service: one
   goroutine per Modbus device (`safego.Go`), reconciled against the device inventory on a 30s
   ticker in `app.go` (`services.NewModbusPoller` + `safego.Supervise`) so a device
   added/edited/disabled in the UI starts/restarts/stops its poller with no process restart. A
   poll is idempotent and connectionless (dials fresh each tick), so a transient bus outage needs
   no recovery logic — it just fails one tick and is retried on the next.
3. **LANDED.** Catalog gained two builtin profiles: **`generic-sunspec-solar`** (self-describing,
   `ModbusMode: "sunspec"`, `ModbusBase: 0` auto-discover — one profile reads any compliant
   inverter/meter/battery with no per-model register map) and **`huawei-sun2000`** (the
   vendor register-map worked example, `ModbusMode: "regmap"`, for the world's most-installed
   inverter — its inverter block is SunSpec-ish but its LUNA battery and built-in meter are
   vendor registers, so every key carries an explicit binding per Huawei's published Modbus
   interface definitions). Bonus finding along the way: the Huawei layout's blocks (inverter
   ~32000, battery ~37760, meter ~37113) sit far enough apart (~5,700 registers) that a
   single-span read is impossible under the Modbus 125-register limit, so
   `infra/iot/modbus.RegisterMap.Read` gained **clustered reads** (`clusters()`, one bounded
   request per block) — a real-map-shaped bug the two earlier (tighter) simulator personas never
   would have caught. `tools/sunspec-sim` gained a fourth persona (unit 4, Huawei) driven by its
   own PV/battery/grid physics loop to exercise this end to end without hardware. Verified live:
   the app booted against the simulator, seeded both profiles, polled unit 4 over Modbus TCP, and
   stored correctly-scaled/signed readings (freq 49.99 Hz, batt_soc 13.2%, grid +600W import).
4. Guarded Modbus control extends `ProfileCommand` to write a holding register with the driver's
   read-back confirm (admin-only, bounded, never-retried — the §8d gates apply unchanged).
5. **RTU (serial)** is the same driver behind a serial transport (adds CRC + a serial port / RTU→TCP
   gateway); TCP is first because the simulator is TCP. **OPC-UA** is a later peer driver.

### Layer B — the system workspace (the reusable "template", P8)

The composite unit that makes "a solar system" one object instead of six devices:

- **`system_template`** — the reusable workspace ("Solar system"): a category, **member slots**
  (role = inverter / battery / pv-string / grid-meter / load-meter / charge-controller; which
  device-profiles fit; required/optional; cardinality), **derived-metric definitions**, default
  system rules, and a dashboard layout. Builtin catalog + import/export (`.iotsystem`, mirroring
  the shipped `.iotprofile`).
- **`system_instance`** — a deployed system created from a template, binding each slot to a concrete
  `iot_device`. This is where a specific model set is customised.
- **`derived_metric`** — computed telemetry (an expression over member keys) stored as a *synthetic
  telemetry key*, so it rides the identical deadband → rollup → rules → charts machinery.
  `net_grid = grid_import − grid_export`, `pv_total = Σ mppt.power`, `autonomy_h = batt_energy /
  load_power`.
- **System-scoped rules** — `iot_rule` already has device/tag scope; add a `system` scope so a rule
  can reference derived metrics and member roles ("SoC < 20% AND pv_total ≈ 0 AND grid down → shed
  non-critical load"). This is the cross-domain-rules differentiator applied to energy.

The workspace changes **nothing** downstream (derived metrics are just synthetic keys) — it is a
grouping + computed-value + layout descriptor, the same "declare once, instantiate many" pattern as
`device_profile`, one level up.

### Scope discipline (refuse)

- No visual rule-chain editor (ThingsBoard-style); the template is a declarative descriptor.
- No MPPT curve optimisation / forecasting / energy trading. Monitor + alert + **guarded** control.
- **CAN-bus batteries stay out** (pure-Go CAN over the wire isn't viable in the single-binary model
  — same verdict as BACnet); those need a Modbus-TCP gateway.

## 9. Known risks

| Risk | Mitigation |
|---|---|
| **SQLite write throughput** under telemetry load. | **RESOLVED (2026-07-14), measured, see §8b.** Deadband + batching + rollup (§3.2) shipped in P1. 20 devices / ~30,000 samples in under a second → 540 rows written, 98.2% suppressed, 0 dropped. `GET /api/devices/stats` keeps it observable going forward. Do not add a TSDB. |
| **`frontend/shared/CameraHero.js`** is camera-specific but lives in the *shared* module. | Still open — P1/P2 shipped as a backend MVP with no device-page hero component yet built against it; decide before the device detail UI lands. |
| **Scope creep** into a Home Assistant clone. | Hold the line stated in §1. |
| **BACnet** demand from building-automation customers. | Out of scope; Go libs are not production-grade. External gateway if forced. |
| **Actuation safety.** | **RESOLVED (2026-07-14), see §8d.** Read-only default, admin-only RBAC, declared-commands-only, server-side bounds, 2s rate limit, full audit (incl. refusals) — all shipped, plus never-auto-retry and non-re-applied expiring desired state, which the original §3.4 did not name. |
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
