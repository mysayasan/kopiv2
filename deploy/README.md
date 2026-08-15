# Installing a packaged release

This page covers **MyMataSan**. For **MySeliaSan** (the fleet control plane), see
[`deploy/README-myseliasan.md`](README-myseliasan.md) — same packaging shapes
(archives, `.deb`/`.rpm`, Windows installer, Docker), built from its own
`.goreleaser.myseliasan.yaml` and released independently under the
`myseliasan-v<version>` tag namespace (see "Versioning Model" in
`docs/TECHNICAL_SPEC.md`) so a MyMataSan release always stays the repository's
"latest" release. For **MyIotSan** (the IoT device hub), see
[`deploy/README-myiotsan.md`](README-myiotsan.md) — same packaging shapes again,
built from `.goreleaser.myiotsan.yaml` and released under its own
`myiotsan-v<version>` tag namespace, also with `--latest=false` for the same reason.
MyIotSan additionally opens a new listening port, `1883/tcp` (its embedded MQTT
broker), which its Windows installer opens a firewall rule for. For **MyIDSan**
(the identity provider), see [`deploy/README-myidsan.md`](README-myidsan.md) —
same packaging shapes once more, built from `.goreleaser.myidsan.yaml` and
released under its own `myidsan-v<version>` tag namespace, also with
`--latest=false`, so it too never displaces MyMataSan as the repository's
"latest" release.

GoReleaser (`.goreleaser.yaml`, `packaging/stage-archive.sh`) builds every release
from `apps/mymatasan` in one run, published to GitHub Releases (and mirrored on the
[Download page](https://r450k.com) — `r450k/worker/index.js` `GET /api/downloads`
resolves the newest release **per product** from the GitHub Releases API (mymatasan's
bare `v<ver>` tag and myseliasan's `myseliasan-v<ver>` tag — it can no longer use
`/releases/latest`, which only ever returns mymatasan's) and edge-caches the result, so
the marketing site never needs a redeploy to reflect a new release of either product;
`r450k/src/sections/Downloads.jsx` renders one download block per product. The r450k
site itself is deployed by `.github/workflows/deploy-r450k.yml` (Cloudflare
`wrangler deploy` on any push to `main` under `r450k/**`; needs a
`CLOUDFLARE_API_TOKEN` repo secret). Because this repo is **private**, downloads are
proxied: `/api/downloads` hands out `/api/download/<assetId>` links and
`r450k/worker/index.js` authenticates to GitHub (Worker secret `GITHUB_TOKEN`, set once
via `npx wrangler secret put GITHUB_TOKEN` — a PAT with `Contents: read`) and redirects
to GitHub's short-lived signed CDN URL, so anonymous visitors can download a private
repo's assets):

- **Archives** (`.tar.gz` linux/macOS / `.zip` windows, amd64+arm64): the `mymatasan`
  binary plus `static/` (web UI), `ai/` (Python worker scripts and the stock
  `yolo11n.pt` model — the heavy Python/torch runtime is instead fetched in-app via
  Settings → AI → Install AI runtime, see `docs/TECHNICAL_SPEC.md`), `deploy/`
  (supervisor examples + this README), and a default `config.json` at the archive
  root. Home dir == data dir (flat, portable layout) unless you set
  `MYMATASAN_HOME`/`MYMATASAN_DATA`. macOS (`darwin` amd64+arm64) archives ship
  alongside linux/windows since the `.goreleaser.yaml` build matrix added `darwin`,
  matching the `deploy/launchd/com.mysayasan.mymatasan.plist` supervisor example
  below that already assumed a macOS build existed.
- **`.deb` / `.rpm`** (linux, via `nfpm`): installs everything flat under
  `/opt/mymatasan` (binary, `static/`, `ai/` including `yolo11n.pt`,
  `deploy/dist/config.json` as `config|noreplace` so an upgrade never clobbers an
  edited config). The postinstall script (`deploy/nfpm/postinstall.sh`) creates an
  unprivileged `mymatasan` system user/group owning `/opt/mymatasan`, then enables
  and (re)starts the `mymatasan.service` systemd unit
  (`deploy/nfpm/mymatasan.service` — distinct from the manual-install example at
  `deploy/systemd/mymatasan.service` below); `preremove.sh`/`postremove.sh` stop
  and disable the service on uninstall. Depends on `ffmpeg` (installed via the
  package manager). `mymatasan.service` sets `MYMATASAN_MANAGED=package`, which
  disables in-app self-update — upgrade via the package manager instead (e.g.
  `sudo apt update && sudo apt install --only-upgrade mymatasan`).
- **Windows installer** (`mymatasan-setup-<version>-windows-x64.exe`, built by the
  `windows-installer` job in `.github/workflows/release.yml` with Inno Setup —
  `packaging/windows/mymatasan.iss`): installs the binary + `static/` + `ai/`
  (including `yolo11n.pt`) to `Program Files\MyMataSan` and registers
  `mymatasan.exe` as a native Windows service (`sc.exe create`, `LocalSystem`,
  auto-restart on failure) with `MYMATASAN_HOME=Program Files\MyMataSan` and
  `MYMATASAN_DATA=%ProgramData%\MyMataSan` as per-service environment. The binary
  is service-aware (`infra/apphost/service_windows.go`: it detects when the SCM
  launched it and runs under `svc.Run`, so `services.msc` Stop/Start controls it
  directly — no WinSW/NSSM wrapper needed for this path). Windows arm64 users
  should take the portable `.zip` archive instead (the installer is x64-only).
  The `mymatasan.exe`, the installer/uninstaller, and the Start Menu shortcuts all
  carry the brand icon (embedded into the exe at build time by
  `packaging/gen-winres.sh` → goversioninfo `.syso`; the installer icon comes from
  `packaging/windows/mymatasan.ico`).

  **After installing (Windows).** The setup's finish page tells the user exactly
  what to do next — it shows the console URL (`https://localhost:3000`) and, on a
  *fresh* install, the bootstrap login: username `admin` and a **strong random
  password generated per install** (injected into the service as
  `LOCAL_ADMIN_PASSWORD`, which `EnsureDefaultAdmin` in
  `apps/mymatasan/services/local_user.go` reads once to seed the admin; the account
  is flagged must-change, so the operator sets their own on first sign-in). The app
  also always writes the same login to `C:\ProgramData\MyMataSan\INITIAL_ADMIN_LOGIN.txt`
  (delete after signing in), so it's recoverable if the finish page is missed. A ticked
  "Open MyMataSan in your browser" checkbox launches the console when setup closes.
  On an *upgrade* the finish page says the existing account is unchanged (no new
  password is shown, since seeding is skipped when the database already exists) —
  because uninstall leaves the data dir in place, a reinstall over old data is an
  upgrade. **If you're locked out** of an existing install, re-run the installer and
  tick **"Reset the admin login"** (offered only on an upgrade): it sets a fresh
  generated password — shown on the finish page and saved to `INITIAL_ADMIN_LOGIN.txt` —
  by dropping a one-shot `RESET_ADMIN` marker the app consumes on next start (your
  cameras, recordings and settings are untouched). A **MyMataSan** Start Menu group is
  created with: open console, Start / Stop the service (self-elevating via UAC), open
  `services.msc`, and Uninstall. **Uninstalling** removes the app and service and then
  *asks* whether to also delete the data dir (`C:\ProgramData\MyMataSan` — recordings,
  database, settings, encryption key), defaulting to **No** so footage/config survive a
  reinstall; answer **Yes** for a clean first-run slate. A **scripted** uninstall states
  its intent instead of being asked — see *Uninstalling* below.
- **Docker images** (`ghcr.io/mysayasan/mymatasan`, linux amd64+arm64, built from
  `deploy/Dockerfile.release`): a `debian:bookworm-slim` base with `ffmpeg` +
  `python3`/`venv` baked in. `MYMATASAN_HOME=/app` (read-only: binary, static
  assets, AI scripts, default config) and `MYMATASAN_DATA=/data` (writable —
  mount a volume here to persist the database, recordings, logs, and the at-rest
  encryption key across container recreation). `MYMATASAN_MANAGED=docker` is also
  set, which disables in-app self-update — pull the new image and recreate the
  container to upgrade instead.

**After installing (Linux / macOS / Docker / portable).** There is no GUI installer
finish page on these paths, so the app itself surfaces the bootstrap login on a
*fresh* install (empty database). `EnsureDefaultAdmin`
(`apps/mymatasan/services/local_user.go`) seeds the `admin` account and, when
neither `localAuth.password` (the packaged `deploy/dist/config.json` ships it
**empty** on purpose) nor the `LOCAL_ADMIN_PASSWORD` env var supplies one,
**generates a strong random per-install password**. The app then prints a sign-in
banner to the console — username, password, and URL — and saves the same to
`INITIAL_ADMIN_LOGIN.txt` in the data dir. The account is flagged must-change, so
the operator sets their own on first sign-in. Where to read it per path:

- **Portable archive / CLI:** the banner prints to the terminal at startup; the
  file is `INITIAL_ADMIN_LOGIN.txt` in the data dir (the flat home dir, or
  `MYMATASAN_DATA` if set).
- **`.deb` / `.rpm` (systemd):** `journalctl -u mymatasan --no-pager | grep -A6 'MyMataSan is ready'`;
  the file is `/opt/mymatasan/INITIAL_ADMIN_LOGIN.txt`. The postinstall script also
  prints these directions.
- **Docker:** `docker logs <container>` shows the banner; the file is
  `/data/INITIAL_ADMIN_LOGIN.txt` (in the mounted volume). To set your own instead,
  pass `-e LOCAL_ADMIN_PASSWORD=…` (or edit `localAuth.password`) — then it is used
  verbatim and not printed.

If you supply the password yourself (config or env), it is used as-is and the banner
does **not** echo it or write the file (you already know it).

All package types resolve their read-only app root vs writable state root
the same way (see "Home/data directory split" in `docs/TECHNICAL_SPEC.md`): an
explicit `MYMATASAN_HOME`/`MYMATASAN_DATA` (or generic `KOPIV2_HOME`/`KOPIV2_DATA`)
env var wins; otherwise data defaults to home, keeping the dev/archive flat layout.

Local dry run without publishing (requires the web bundle built first —
`make web APP=mymatasan`):

```bash
goreleaser release --snapshot --clean --skip=docker
```

## Uninstalling — keep your data, or wipe it

Every uninstall path **keeps your data by default**: an accidental `apt-get remove`, or
an operator clicking through an uninstaller, must never destroy footage. A clean wipe is
always something you ask for explicitly.

**Linux (deb/rpm).** The package installs `/usr/sbin/mymatasan-uninstall`, which calls
`apt`/`dnf` for you:

```sh
sudo mymatasan-uninstall                # remove the package, KEEP /opt/mymatasan
sudo mymatasan-uninstall --purge-data   # remove it and ERASE /opt/mymatasan
sudo mymatasan-uninstall --purge-data -y   # same, unattended (no prompt)
sudo mymatasan-uninstall --purge-data -n   # dry run: print, change nothing
```

Interactively, `--purge-data` makes you type `ERASE` before it does anything. The same
wipe is available straight from the package manager, for config-managed hosts:

```sh
sudo KOPIV2_PURGE_DATA=1 apt-get purge mymatasan     # or: dnf remove
sudo apt-get purge mymatasan                         # without it, data is kept
```

`KOPIV2_PURGE_DATA=1` is read by the package's `postremove` scriptlet
(`deploy/nfpm/postremove.sh`), so it works however the removal was triggered. Both
routes also drop the `mymatasan` service account when they wipe.

**Only `/opt/mymatasan` is erased.** Recordings you pointed at another disk are *not*
touched — delete those yourself.

**Windows.** Uninstalling from Add/Remove Programs or the Start Menu asks whether to
delete `C:\ProgramData\MyMataSan`, defaulting to No. A scripted uninstall says what it
wants instead:

```bat
set KOPIV2_PURGE_DATA=1 & "C:\Program Files\MyMataSan\unins000.exe" /VERYSILENT
"C:\Program Files\MyMataSan\unins000.exe" /VERYSILENT /CLEANDATA
"C:\Program Files\MyMataSan\unins000.exe" /VERYSILENT /KEEPDATA
```

The environment variable is the dependable form: the uninstaller relaunches itself from
`%TEMP%` for its second phase (where the decision is made) and a child process always
inherits the environment. A silent uninstall with no instruction keeps the data, and
keep beats clean if both are given.

**Wiping while keeping the machine.** If you want the data destroyed but MyMataSan still
installed, use **Secure Wipe & Reset** in the app instead — it crypto-erases the at-rest
key and shreds recordings, which a file delete does not.

# TLS / HTTPS

The packaged default (`config.json`) serves **HTTPS on :3000**. On first boot, if
TLS is enabled (`server.tlsPorts` is set) and no certificate exists at
`tls.certPath` / `tls.keyPath`, MyMataSan **generates a self-signed certificate**
into `./certs` covering `localhost`, the configured hostnames, and every local IP.
This means a fresh install serves HTTPS immediately — but browsers will show a
one-time "not secure / proceed anyway" warning because the cert is self-signed.

For a **trusted** setup, pick one:

- **Bring your own cert.** Drop a real `cert.pem` / `key.pem` at the paths in the
  `tls` block (e.g. from your internal CA or a public CA if the box has a domain).
  MyMataSan leaves existing files untouched — it only generates when they're missing.
- **Front it with a TLS-terminating reverse proxy** (recommended when you have a
  domain and want automatic Let's Encrypt certs). Point the proxy at the app and
  switch the app to plain HTTP by moving the port from `server.tlsPorts` to
  `server.nonTlsPorts`. Examples:

  Caddy (`Caddyfile`) — automatic HTTPS:

  ```
  nvr.example.com {
      reverse_proxy 127.0.0.1:3000
  }
  ```

  nginx:

  ```
  server {
      listen 443 ssl;
      server_name nvr.example.com;
      ssl_certificate     /etc/letsencrypt/live/nvr.example.com/fullchain.pem;
      ssl_certificate_key /etc/letsencrypt/live/nvr.example.com/privkey.pem;
      location / {
          proxy_pass http://127.0.0.1:3000;
          proxy_http_version 1.1;
          proxy_set_header Upgrade $http_upgrade;
          proxy_set_header Connection "upgrade";
          proxy_set_header Host $host;
          proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
          proxy_set_header X-Forwarded-Proto $scheme;
      }
  }
  ```

  Note: live view uses WebRTC (media flows over its own UDP ports directly between
  browser and app), so the reverse proxy only needs to carry the HTTPS signaling
  traffic shown above.

  Also set `rateLimit.trustedProxies` in the app's config to the proxy's address
  (e.g. `["127.0.0.1"]` for a local nginx/Caddy as above). Without it, the app
  ignores the `X-Forwarded-For` header it sets and rate-limits/lockouts every
  client behind the proxy as one shared bucket keyed on the proxy's own address
  — this is the secure default (a directly-exposed instance can't be spoofed by
  a forged header), but behind a real proxy it needs to be told which peer to
  trust.

  See [`reverse-proxy/`](reverse-proxy/) for more thorough sample configs and a
  fuller explanation of the `X-Forwarded-*` trust model. Those samples are
  written for MyIDSan, so their redirect-URI-exactness and Kerberos
  connection-affinity notes don't apply here, but the header-trust and
  `trustedProxies` rules are identical for MyMataSan.

---

# Running MyMataSan under a process supervisor

MyMataSan restarts itself for **factory reset** and **in-app self-update**. The
reliable, cross-platform way to do that is to run it under a process supervisor that
relaunches it when it exits — and set `KOPIV2_SUPERVISED=1` so the app *exits cleanly*
for the supervisor instead of trying to relaunch itself (which would race the supervisor
and could double-start).

| Platform            | Supervisor              | File / command                                   |
|---------------------|-------------------------|--------------------------------------------------|
| Linux / Raspberry Pi| systemd                 | `deploy/systemd/mymatasan.service`               |
| macOS               | launchd                 | `deploy/launchd/com.mysayasan.mymatasan.plist`   |
| Windows (installer) | native Windows service  | `packaging/windows/mymatasan.iss` (registers the service; see above) |
| Windows (manual)    | WinSW or NSSM           | `deploy/windows/mymatasan.winsw.xml`             |
| Docker              | `restart: unless-stopped`| `docker-compose.yml` (app service)              |

Each example sets **`KOPIV2_SUPERVISED=1`** and a restart-on-exit policy. Install steps
are in the header comments of each file. The Windows installer path is service-aware
natively (see above), so it needs no WinSW/NSSM wrapper; `deploy/windows/mymatasan.winsw.xml`
remains for manual Windows installs (e.g. arm64, or from a portable `.zip`).

## Self-update

The Settings → Version & Health → Updates panel checks GitHub Releases on a schedule
(and on demand) and, when the running install owns its files, offers a one-click
**Update to vX.Y.Z** that downloads the matching release archive, verifies its
SHA-256 against `checksums.txt`, swaps the binary and `static/`/`ai/` assets, and
restarts (`GET /api/system/update`, `POST /api/system/update/check`,
`POST /api/system/update/apply`). Asset selection requires the `mymatasan_` product
prefix (not just the `_<os>_<arch>` suffix) — the repo also releases MySeliaSan, whose
archives share the identical suffix and extension, so the prefix is what keeps this
updater from ever installing the wrong product over itself; see
`apps/mymatasan/services/update.go.md`. This applies to portable archive and Windows
installer installs. It is disabled — with in-UI guidance instead — for `.deb`/`.rpm`
installs (`MYMATASAN_MANAGED=package`: upgrade via `apt`/`dnf`) and Docker
(`MYMATASAN_MANAGED=docker`: pull the new image and recreate the container).

## How the restart works

- `KOPIV2_SUPERVISED=1` (recommended for any service/Docker deployment): on restart the
  app **exits cleanly** and the supervisor relaunches it. Works identically on Linux,
  macOS, Windows, Docker, and Raspberry Pi.
- **Unset** (bare/dev run, no supervisor): the app relaunches itself — on Unix via an
  in-place `execve` (same pid, no port hand-off race), on Windows by spawning a fresh
  detached process. This is best-effort; for production prefer a supervisor.

So: a factory reset shows "Finalizing & restarting…", the process exits, and the
supervisor brings it back within a couple of seconds; the web UI polls `/health` and
reloads automatically.
