# Installing a packaged release

GoReleaser (`.goreleaser.yaml`, `packaging/stage-archive.sh`) builds every release
from `apps/mymatasan` in one run:

- **Archives** (`.tar.gz` linux / `.zip` windows, amd64+arm64): the `mymatasan`
  binary plus `static/` (web UI), `ai/` (Python worker scripts — model weights are
  fetched at runtime, not bundled), `deploy/` (supervisor examples + this README),
  and a default `config.json` at the archive root. Home dir == data dir (flat,
  portable layout) unless you set `MYMATASAN_HOME`/`MYMATASAN_DATA`.
- **`.deb` / `.rpm`** (linux, via `nfpm`): installs everything flat under
  `/opt/mymatasan` (binary, `static/`, `ai/`, `deploy/dist/config.json` as
  `config|noreplace` so an upgrade never clobbers an edited config). The
  postinstall script (`deploy/nfpm/postinstall.sh`) creates an unprivileged
  `mymatasan` system user/group owning `/opt/mymatasan`, then enables and
  (re)starts the `mymatasan.service` systemd unit
  (`deploy/nfpm/mymatasan.service` — distinct from the manual-install example at
  `deploy/systemd/mymatasan.service` below); `preremove.sh`/`postremove.sh` stop
  and disable the service on uninstall. Depends on `ffmpeg` (installed via the
  package manager).
- **Docker images** (`ghcr.io/mysayasan/mymatasan`, linux amd64+arm64, built from
  `deploy/Dockerfile.release`): a `debian:bookworm-slim` base with `ffmpeg` +
  `python3`/`venv` baked in. `MYMATASAN_HOME=/app` (read-only: binary, static
  assets, AI scripts, default config) and `MYMATASAN_DATA=/data` (writable —
  mount a volume here to persist the database, recordings, logs, and the at-rest
  encryption key across container recreation).

All three package types resolve their read-only app root vs writable state root
the same way (see "Home/data directory split" in `docs/TECHNICAL_SPEC.md`): an
explicit `MYMATASAN_HOME`/`MYMATASAN_DATA` (or generic `KOPIV2_HOME`/`KOPIV2_DATA`)
env var wins; otherwise data defaults to home, keeping the dev/archive flat layout.

Local dry run without publishing (requires the web bundle built first —
`make web APP=mymatasan`):

```bash
goreleaser release --snapshot --clean --skip=docker
```

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

---

# Running MyMataSan under a process supervisor

MyMataSan restarts itself for **factory reset** and (future) **self-update**. The
reliable, cross-platform way to do that is to run it under a process supervisor that
relaunches it when it exits — and set `KOPIV2_SUPERVISED=1` so the app *exits cleanly*
for the supervisor instead of trying to relaunch itself (which would race the supervisor
and could double-start).

| Platform            | Supervisor              | File / command                                   |
|---------------------|-------------------------|--------------------------------------------------|
| Linux / Raspberry Pi| systemd                 | `deploy/systemd/mymatasan.service`               |
| macOS               | launchd                 | `deploy/launchd/com.mysayasan.mymatasan.plist`   |
| Windows             | WinSW or NSSM           | `deploy/windows/mymatasan.winsw.xml`             |
| Docker              | `restart: unless-stopped`| `docker-compose.yml` (app service)              |

Each example sets **`KOPIV2_SUPERVISED=1`** and a restart-on-exit policy. Install steps
are in the header comments of each file.

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
