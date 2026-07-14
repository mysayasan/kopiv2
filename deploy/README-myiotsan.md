# MyIotSan — install & run

MyIotSan is the on-prem **IoT device hub** — "the NVR, but for sensors." Devices publish
telemetry to its **embedded MQTT broker**; it stores the readings, evaluates alert rules
over them, and raises alerts into a unified notification feed. It can also be adopted as a
node into a **MySeliaSan** fleet, so its alerts sit alongside a camera node's.

It is a single pure-Go binary. No ffmpeg, no Python, no model weights, and **no separate
MQTT broker to install** — the broker is in the binary.

## What's in this archive

```
myiotsan(.exe)     the server (MQTT broker included)
config.json        defaults — edit before first start (see below)
static/            the web UI
deploy/            service definitions (systemd / WinSW / launchd) + this file
```

Everything the app writes (database, logs, TLS certs, `secret/atrest.key`) is created next
to the binary on first run, unless you point `MYIOTSAN_DATA` elsewhere.

## Quick start

```
./myiotsan            # Linux / macOS
myiotsan.exe          # Windows
```

Then open <https://localhost:3003>. It serves HTTPS immediately using a **self-signed**
certificate generated on first boot, so the browser shows a one-time trust warning. For a
trusted chain, drop your own cert/key at `tls.certPath` / `tls.keyPath`, or front the app
with a TLS-terminating reverse proxy and switch to `server.nonTlsPorts`.

**First-run login.** A strong one-time superadmin password is generated per install,
printed to the log, and saved to `INITIAL_ADMIN_LOGIN.txt` in the data dir. You must change
it on first sign-in. To set it yourself instead, either fill in `localAuth.password` in
`config.json` or export `LOCAL_ADMIN_PASSWORD` before the first start.

There is deliberately **no shipped default password**. This appliance can actuate relays;
a known default would be a site-wide backdoor.

## Ports

| Port | Proto | Direction | Purpose |
| --- | --- | --- | --- |
| 3003 | TCP/TLS | inbound | Web UI + API |
| **1883** | **TCP** | **inbound** | **The embedded MQTT broker — devices publish telemetry here.** |
| 39532 | TCP/mTLS | inbound | management listener, dialled by the MySeliaSan parent (heartbeat, release). Fleet installs only. |
| 39533 | TCP/mTLS | **outbound** | node → parent control channel. Fleet installs only. |
| 49531 | UDP multicast | out/in | LAN fleet discovery announce. Fleet installs only. |

**1883 is the one to remember.** It is a listening port no other product in this suite
opens, so it will not already be allowed by a firewall rule you made for MyMataSan or
MySeliaSan. Until you open it, the UI comes up fine and **no device can connect** — which
looks like broken devices, not a blocked port.

```
ufw allow 1883/tcp                                                    # Debian/Ubuntu
firewall-cmd --add-port=1883/tcp --permanent && firewall-cmd --reload # RHEL/Fedora
netsh advfirewall firewall add rule name="MyIotSan MQTT" dir=in action=allow protocol=TCP localport=1883
```

To move or disable the broker (e.g. a site that already runs Mosquitto/EMQX), edit the
`mqtt` block in `config.json`. The ingest pipeline does not care where a payload came from.

## Configure

Edit `config.json`:

| Setting | Why it matters |
| --- | --- |
| `mqtt.addr` | The broker listener (default `0.0.0.0:1883`). Narrow it to one LAN address on a multi-homed host, or set `mqtt.enabled: false` to use an external broker. |
| `telemetry_store` | Write batching (`batchSize`/`flushMs`/`queueSize`) and retention (`rawRetentionDays`, `rollupRetentionDays`). These are the knobs that decide whether the box keeps up with a chatty estate. A full queue **sheds readings** rather than stalling the broker on the disk. |
| `pairing.*` | Only if adopting into a MySeliaSan fleet. The `3953x` ports must match the control plane's — a node and a parent that disagree never reconnect. |
| `db` | Defaults to SQLite (`./data/myiotsan.db`). Postgres and MariaDB are also supported; consider one of them for a large estate, since every reading is a row. |

## Onboarding devices

A device that is **not in the inventory cannot connect at all** — the device table is the
credential store, which is what makes deleting a device actually revoke it. So you do not
onboard by editing config:

1. Sign in, change the admin password.
2. Open a time-boxed **enrollment window** (Discovery in the UI, or
   `POST /api/discovery/window`). It returns a one-time enrollment key, shown **exactly
   once**, and closes on its own.
3. Point unprovisioned devices at the broker using that key as their MQTT password. They
   are admitted but **quarantined** — recorded as candidates, no telemetry stored, no rules
   evaluated.
4. Review the candidates and adopt the ones you recognise.

## Running as a service

Use the matching file in `deploy/`:

- **Linux** — `deploy/myiotsan.service` (systemd). The `.deb`/`.rpm` packages install and
  enable this for you under a dedicated `myiotsan` user.
- **Windows** — `deploy/myiotsan.winsw.xml` (WinSW or NSSM). The `myiotsan-setup-*.exe`
  installer registers the service for you; this file is only for the portable `.zip`.
- **macOS** — `deploy/com.mysayasan.myiotsan.plist` (launchd).

All of them set `KOPIV2_SUPERVISED=1`, so an in-app restart exits cleanly and the
supervisor relaunches it.

## Back up `secret/atrest.key`

`secret/atrest.key` (in the data dir) encrypts the **fleet key** and the **device
credentials** stored in the database. The app deliberately **fails closed**: if that key
goes missing while encrypted rows remain, it refuses to start rather than silently
resetting trust.

Back it up together with the database, and keep them as a pair. Losing the key means
re-provisioning every device. The database also holds all telemetry history, which exists
nowhere else.

## Updating

**MyIotSan has no in-app self-updater.** Unlike MyMataSan, it never phones home, never
checks GitHub for a release, and will not update itself. Updates are entirely manual and
entirely on your schedule — deliberate, for an appliance that may sit on an air-gapped
building network.

- **deb/rpm** — `apt install ./myiotsan_*.deb` / `dnf install ./myiotsan-*.rpm` over the
  top. Data under `/opt/myiotsan` is preserved.
- **Portable archive** — stop the service, replace the binary and `static/`, restart. Keep
  your `config.json`, `data/`, `certs/` and `secret/`.
- **Docker** — pull the new tag; keep the `/data` volume.

Releases are published under the **`myiotsan-v<ver>`** tag namespace on GitHub (never as
the repo's "latest" release — that belongs to MyMataSan, whose updater reads it).
