# MySeliaSan — install & run

MySeliaSan is the **fleet control plane** for MyMataSan camera nodes: discover them on
the LAN, adopt them over mTLS, and manage each node's cameras, recordings and alerts
remotely — the browser never talks to a node directly.

It is a single pure-Go binary. No ffmpeg, no Python, no model weights.

## What's in this archive

```
myseliasan(.exe)   the server
config.json        defaults — edit before first start (see below)
static/            the web UI
deploy/            service definitions (systemd / WinSW / launchd) + this file
```

Everything the app writes (database, logs, TLS certs, `secret/atrest.key`) is created
next to the binary on first run, unless you point `MYSELIASAN_DATA` elsewhere.

## Quick start

```
./myseliasan            # Linux / macOS
myseliasan.exe          # Windows
```

Then open <https://localhost:3002>. It serves HTTPS immediately using a **self-signed**
certificate generated on first boot, so the browser shows a one-time trust warning. For
a trusted chain, drop your own cert/key at `tls.certPath` / `tls.keyPath`, or front the
app with a TLS-terminating reverse proxy and switch to `server.nonTlsPorts`.

**First-run login.** A strong one-time superadmin password is generated per install,
printed to the log, and saved to `INITIAL_ADMIN_LOGIN.txt` in the data dir. You must
change it on first sign-in. To set it yourself instead, either fill in
`localAuth.password` in `config.json` or export `LOCAL_ADMIN_PASSWORD` before the first
start.

## Configure before adopting nodes

Edit `config.json`:

| Setting | Why it matters |
| --- | --- |
| `pairing.parentBaseUrl` | **The one that bites.** Adopted nodes store this URL and dial back to it. It must be a LAN address other machines can reach (e.g. `https://192.168.1.10:3002`) — leave it empty or `localhost` and adoption from another host silently fails. |
| `sso.providerBaseUrl` | Optional. Point at a MyIDSan instance for federated login. Leave **empty** to run on local accounts only; the "Continue with myidsan" button is then hidden. |
| `db` | Defaults to SQLite (`./data/myseliasan.db`), which is fine for a single control plane. Postgres and MariaDB are also supported. |
| `nodeStream.publicIps` / `udpPort` | Only needed when browsers are not on the parent's LAN (WebRTC live view). |

## Ports

| Port | Proto | Direction | Purpose |
| --- | --- | --- | --- |
| 3002 | TCP/TLS | inbound | Web UI + API |
| 39533 | TCP/mTLS | inbound | node-dialed control channel |
| 39534 | TCP/mTLS | inbound | node-dialed media relay |
| 49531 | UDP multicast | out/in | LAN node discovery |
| 39532 | TCP/mTLS | **outbound** | parent → node management (enroll, heartbeat, release) |

These must match the node side. They are the MyMataSan shipped defaults — do not change
them on only one end.

## Adopting a fleet

1. Sign in, change the admin password.
2. Generate the **fleet key** (a 32-byte PSK). Until you do, LAN discovery returns
   nothing and no node can be adopted.
3. Enter that same fleet key on each MyMataSan node.
4. Scan → adopt.

## Running as a service

Use the matching file in `deploy/`:

- **Linux** — `deploy/myseliasan.service` (systemd). The `.deb`/`.rpm` packages install
  and enable this for you under a dedicated `myseliasan` user.
- **Windows** — `deploy/myseliasan.winsw.xml` (WinSW or NSSM). The
  `myseliasan-setup-*.exe` installer registers the service for you; this file is only
  for the portable `.zip`.
- **macOS** — `deploy/com.mysayasan.myseliasan.plist` (launchd).

All of them set `KOPIV2_SUPERVISED=1`, so an in-app restart exits cleanly and the
supervisor relaunches it.

## Back up `secret/atrest.key`

`secret/atrest.key` (in the data dir) encrypts the **fleet CA private key** and the
**pairing PSK** stored in the database. The app deliberately **fails closed**: if that
key goes missing while encrypted rows remain, it refuses to start rather than silently
resetting the fleet's trust.

Back it up together with the database, and keep them as a pair. Losing the key means
re-adopting every node.

## Upgrading

- **deb/rpm** — `apt install ./myseliasan_*.deb` / `dnf install ./myseliasan-*.rpm` over
  the top. Data under `/opt/myseliasan` is preserved.
- **Portable archive** — stop the service, replace the binary and `static/`, restart.
  Keep your `config.json`, `data/`, `certs/` and `secret/`.
- **Docker** — pull the new tag; keep the `/data` volume.
