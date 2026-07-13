# k6 — load testing for mymatasan + myseliasan (with live Grafana dashboards)

Developer/performance tooling that drives [k6](https://k6.io/) (the
`grafana/k6` Docker image, already pulled on this machine) at a running
**mymatasan** or **myseliasan** instance and streams the results live into a
local **Grafana + InfluxDB** stack.

Pick the app with `-App` (PowerShell) / `--app` (bash); it defaults to
`mymatasan`. The two apps authenticate differently (see **Auth** below), so each
has its own scripts and its own `config/<app>.target.env`.

Like `tools/zaproxy` and `tools/tgbridge`, this is **developer tooling only** —
not part of any shipped app, no runtime dependency on the apps/domain/infra, and
not covered by `docs/modules/` or the version changelog.

Where ZAP answers *"is it safe?"*, k6 answers *"how much can it take, and where
does it fall over?"*.

## What it does

- Spins up **InfluxDB** (metric store) + **Grafana** (dashboards) in Docker, on
  `http://localhost:3300` — pre-provisioned with a datasource and the
  **"k6 Load Testing Results"** dashboard, so it's ready the moment it's up.
- Runs a k6 script from `scripts/` inside that same Docker network, so k6 can
  write to InfluxDB while hitting the app via `host.docker.internal`.
- Saves a JSON summary of each run to `results/` (git-ignored).

## Auth

**mymatasan** authenticates with **HTTP Basic** — the SPA replays the credential
on every request, so the scripts do the same (one `Authorization` header, from
`config/target.env`). This makes **bcrypt-per-request the throughput ceiling**.
The initial admin login is in `apps/mymatasan/INITIAL_ADMIN_LOGIN.txt` (`admin` /
`admin123`; **the first sign-in forces a password change**, so a brand-new
install needs one `POST /api/auth/change-password` before load — see
`mymatasan-verify-recipe` in project memory for the throwaway-instance recipe).

**myseliasan** does a **JSON login + cookie session** (`config/myseliasan.target.env`):
each VU `POST`s `/api/auth/local-login` **once**, captures the session cookie
into a module variable, and replays it as a `Cookie` header on every request
(k6 resets the per-VU cookie jar between iterations, so relying on the jar alone
would 401 after iteration 1). So bcrypt runs once per VU, not per request — the
ceiling here is TLS + JSON + the DB read path, a very different profile from
mymatasan.

The stock superadmin's password depends on how the target was started, and there
is **no longer an `admin`/`admin` fallback** (it was removed — a control plane for
the whole camera fleet must not ship a guessable default):

| target | password |
| --- | --- |
| dev (`config.dev.json`) | `admin123` |
| a packaged install | **generated per install** — printed to the log and saved to `INITIAL_ADMIN_LOGIN.txt` in the data dir |
| anything | `LOCAL_ADMIN_PASSWORD` overrides both |

Whichever you use, the account is **must-change on first login**; clear that flag
first (log in through the UI or `POST /api/auth/change-password`) and put the
*resulting* password in `config/myseliasan.target.env`. While the flag is set,
every authed endpoint except `/api/session/me` returns
`401 password_change_required` and the read scripts have nothing to hit.

## Scripts

Each app has a matching trio; myseliasan's are prefixed `myseliasan-`.

| Script | Shape | Use for |
|--------|-------|---------|
| `smoke` (default) | 1 VU, 5 iters | Verify the target + creds + every endpoint returns 2xx **before** a long run. |
| `load` | ramp → hold → ramp, 50 VUs, think time, **thresholds** | Realistic operator-console read load. Fails if p95 > 800ms or errors > 1%. |
| `stress` | steps VUs to 300, no think time | Find the **knee** — where latency/throughput turn over. No thresholds; watch Grafana live. |

`load`/`stress` only issue **GET**s against read endpoints — mymatasan:
notifications feed, dashboard stats, heatmap, cameras; myseliasan: nodes list,
notifications, session/RBAC — no writes, no purge, no reset — so they're safe to
point at a live instance. (Load *volume* is still real; don't stress-test
something someone is actively using.)

## Setup

1. Start the app (mymatasan dev listens on TLS `:3000`, myseliasan on `:3002`).
2. Copy the matching config template and fill it in:
   ```
   cp config/target.env.example config/target.env                       # mymatasan
   cp config/myseliasan.target.env.example config/myseliasan.target.env # myseliasan
   ```
   - `BASE_URL` — how the **k6 container** reaches the app. On Docker Desktop use
     `https://host.docker.internal:3000` (mymatasan) / `:3002` (myseliasan);
     self-signed cert, k6 skips verify.
   - `AUTH_USER` / `AUTH_PASS` — an app user (blank = anonymous, only public
     endpoints pass).
   - `config/*.target.env` and everything in `results/` are git-ignored.

## Run

Windows (PowerShell):
```powershell
cd tools/k6
./run.ps1                                        # mymatasan smoke
./run.ps1 -Script load                           # mymatasan 50-VU ramping load
./run.ps1 -Script load -TargetVus 100 -Hold 2m
./run.ps1 -Script stress -MaxVus 400             # find the breaking point
./run.ps1 -App myseliasan                        # myseliasan smoke
./run.ps1 -App myseliasan -Script load           # myseliasan ramping load
./run.ps1 -App myseliasan -Script stress -MaxVus 500
./run.ps1 -Script load -NoBackend                # backend already up — skip the up step
```

Linux/Mac/WSL/Git-Bash:
```bash
cd tools/k6
./run.sh                        # mymatasan smoke
./run.sh load                   # mymatasan ramping load
./run.sh --app myseliasan       # myseliasan smoke
./run.sh --app myseliasan stress
# env overrides: BASE_URL=… AUTH_USER=… TARGET_VUS=100 HOLD=2m ./run.sh load
```

Then open **http://localhost:3300** → dashboard **"k6 Load Testing Results"**
(anonymous admin, no login). The dashboard updates live during a run: VUs,
requests/s, error rate, latency p50/p95/p99, throughput-vs-VUs, and p95 **by
endpoint**.

The metrics backend is left running between runs so you can compare. Stop it
with:
```
docker compose down        # keep InfluxDB history
docker compose down -v     # also wipe it
```

## Reading the results

- **`http_req_duration` p95/p99** — the headline latency. `load.js` gates on it.
- **`http_req_failed`** — error rate. Non-zero under load usually means the app
  hit a ceiling (CPU, connection pool, or the rate limiter — see below).
- **`endpoint_duration{endpoint:…}`** — per-route latency, so you can see *which*
  endpoint is the slow one, not just an aggregate.
- **Throughput-vs-VUs** — the classic capacity curve. While it climbs linearly
  and latency stays flat, you have headroom. When throughput plateaus and
  latency climbs, that plateau is your **max sustainable req/s** for this box.

## Gotchas / tuning

- **Rate limiting skews load tests.** The dev config enables `rateLimit`
  (~120 auth req / 20 s), and it's keyed per-IP — all k6 traffic shares one
  bucket, so you'll be throttled to a few req/s and measure the limiter, not the
  app. To measure raw handler/DB capacity, set `rateLimit.enabled: false` (or
  raise the limits) in the instance's config first. To test the limiter itself,
  leave it on and watch for `429`s in the error rate. (myseliasan hits this on
  the login burst as VUs ramp — same fix.)
- **`host.docker.internal`** is provided via `extra_hosts` in the compose file,
  so this works on Linux Docker too (not just Docker Desktop).
- **Grafana is on `3300`** — `3000` is mymatasan's port, `3002` is myseliasan's.
- **myseliasan: login is once per VU, not per request.** A `login_duration`
  trend is tracked separately and is excluded from the load thresholds, so the
  one-time bcrypt cost per VU doesn't fail an otherwise-healthy read run.
- **Empty DB understates aggregation cost.** A throwaway with no data makes
  `/api/notifications/stats` trivial. To load-test the aggregation path
  honestly, point at an instance whose DB actually has notification history.
- **k6 exit code** is non-zero when a threshold fails — that's the signal, not a
  tooling error; read the summary + dashboard.

## Files

```
run.ps1 / run.sh                     entrypoints (Windows / POSIX), -App / --app
docker-compose.yml                   InfluxDB + Grafana + on-demand k6 service
config/target.env.example            copy to config/target.env            (mymatasan)
config/myseliasan.target.env.example copy to config/myseliasan.target.env (myseliasan)
scripts/smoke.js|load.js|stress.js   mymatasan scripts (Basic auth)
scripts/myseliasan-*.js              myseliasan scripts (JSON login + cookie)
scripts/lib/common.js                mymatasan Basic-auth + per-endpoint metrics
scripts/lib/session.js               myseliasan JSON-login/cookie + per-endpoint metrics
grafana/                             auto-provisioned datasource + dashboard
results/                             generated JSON summaries (git-ignored)
```
