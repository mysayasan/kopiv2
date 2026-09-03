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

**First start only:** the shipped `config.json` marks setup as not yet completed, so the
process opens a small pre-boot **configuration wizard** at
<http://127.0.0.1:39530> instead — loopback-only, and a browser tab is opened there for
you (unless `KOPIV2_OPEN_BROWSER=0`). It confirms the database, cache, listen address and
administrator before the app itself ever starts, and needs no login of its own since
nothing has booted yet to log in to. Finishing it writes `config.json` and the real app
comes straight up in the same process — no restart. Every start after that skips the
wizard entirely (it is gated on `config.json`'s `setup.completed` flag, never on whether
the database happens to be reachable), and it never appears at all if you hand-edit
`config.json` before first start and remove the `setup` block. Recovery path for an
install that can no longer boot at all: set `KOPIV2_SETUP=1` for one run. See
`docs/modules/infra/apphost/firstboot/`.

Once through it, open <https://localhost:3002>. It serves HTTPS immediately using a
**self-signed** certificate generated on first boot, so the browser shows a one-time trust
warning. For a trusted chain, drop your own cert/key at `tls.certPath` / `tls.keyPath`, or
front the app with a TLS-terminating reverse proxy and switch to `server.nonTlsPorts`. See
[`reverse-proxy/`](reverse-proxy/) (written for MyIDSan) for working nginx/Caddy configs
and the `X-Forwarded-*`/`rateLimit.trustedProxies` trust model, which applies here
unchanged.

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
| 39530 | TCP | loopback only | pre-boot configuration wizard, first start only — never reachable from the network, no firewall rule needed |

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

## Uninstalling — keep your data, or wipe it

Removing MySeliaSan **keeps `/opt/myseliasan` by default** on every path. That is
deliberate: `secret/atrest.key` encrypts the fleet CA key and the pairing PSK, so
erasing it orphans every adopted node (see *Back up `secret/atrest.key`* above). You
have to ask for the clean wipe.

**Linux (deb/rpm).**

```sh
sudo myseliasan-uninstall                # remove the package, KEEP /opt/myseliasan
sudo myseliasan-uninstall --purge-data   # remove it and ERASE /opt/myseliasan
sudo myseliasan-uninstall --purge-data -y   # same, unattended (no prompt)
sudo myseliasan-uninstall --purge-data -n   # dry run: print, change nothing
```

`myseliasan-uninstall` is installed to `/usr/sbin` by the package. It calls
`apt`/`dnf` for you, so you never need to remember which. Interactively, `--purge-data`
makes you type `ERASE` before it does anything.

If you'd rather drive the package manager yourself, the same wipe is one variable:

```sh
sudo KOPIV2_PURGE_DATA=1 apt-get purge myseliasan     # or: dnf remove
sudo apt-get purge myseliasan                         # without it, data is kept
```

**Windows.** Uninstall from Add/Remove Programs (or the Start Menu) and answer the
prompt — it asks whether to also delete `C:\ProgramData\MySeliaSan`, defaulting to
**No**. For a scripted uninstall:

```bat
set KOPIV2_PURGE_DATA=1 & "C:\Program Files\MySeliaSan\unins000.exe" /VERYSILENT
"C:\Program Files\MySeliaSan\unins000.exe" /VERYSILENT /CLEANDATA
"C:\Program Files\MySeliaSan\unins000.exe" /VERYSILENT /KEEPDATA
```

A silent uninstall with no instruction keeps the data, and `KEEPDATA` beats
`CLEANDATA` if both are given — an unattended run should never destroy a fleet by
accident.
