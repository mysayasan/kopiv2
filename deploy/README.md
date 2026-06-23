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
