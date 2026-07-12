# zaproxy — OWASP ZAP security scanner for mymatasan / myseliasan

Developer/security tooling that points [OWASP ZAP](https://www.zaproxy.org/)
(the `ghcr.io/zaproxy/zaproxy:stable` Docker image) at a running **mymatasan**
or **myseliasan** instance and produces HTML + JSON vulnerability reports.

Like `tools/tgbridge`, this is **developer tooling only** — not part of any
shipped app, no runtime dependency on the apps/domain/infra, and not covered by
`docs/modules/` or the version changelog.

Pick the app with `-App` (ps1) / `--app` (sh); default is `mymatasan`. The two
apps authenticate differently, so each has its own plans + config file:

| App | Port | Auth model | Plans | Config |
|-----|------|------------|-------|--------|
| `mymatasan` (default) | 3000 | HTTP Basic (replayed every request) | `plans/<mode>.yaml` | `config/target.env` |
| `myseliasan` | 3002 | JSON login → session cookie + double-submit CSRF | `plans/myseliasan-<mode>.yaml` | `config/myseliasan.target.env` |

## What it checks

mymatasan authenticates with **HTTP Basic** (the SPA replays the credential on
every request), so authenticated scanning just needs a Basic header — no login
form or CSRF dance. The plans inject it via a ZAP *replacer* rule.

Three scan plans in `plans/`:

| Mode | Plan | Sends attacks? | Safe on live? | Use for |
|------|------|----------------|---------------|---------|
| `baseline` (default) | `baseline.yaml` | **No** — passive only | ✅ Yes | Security headers, cookie flags (`HttpOnly`/`SameSite`), info disclosure, mixed content, TLS hints |
| `api` | `api.yaml` | **Yes** — active, scoped to `/api` | ❌ No | Injection / traversal / auth flaws on the API |
| `full` | `full.yaml` | **Yes** — active, whole app | ❌ No | Deepest coverage |

## ⚠️ Safety

- **`baseline` is the default and is safe on a live instance** — it never sends
  attack payloads and never submits forms.
- **`api` and `full` send real attack payloads** and can create/modify/delete
  data, fire notifications, and stress the box. The scripts prompt for
  confirmation before running them.
- All plans **exclude the destructive control-plane routes** so a scan can never
  trigger them: `/api/system/*` (factory **reset**/restart/update/restore),
  every `*/purge`, camera `factory-default`, recording `segments` DELETE, and
  the `{id}` DELETE resource routes. mymatasan's config has `allowReset: true` —
  an unguarded active scan hitting `POST /api/system/reset` would **wipe the
  install**, which is exactly what these exclusions prevent.
- For `api`/`full`, run against a **throwaway instance**, not production — see
  the `mymatasan-verify-recipe` (isolated `HOME`/`DATA` overrides, fresh sqlite,
  seeded admin) in the project memory.

## Setup

1. Start mymatasan (dev instance listens on TLS `:3000`).
2. Copy the config template and fill it in:
   ```
   cp config/target.env.example config/target.env
   ```
   - `TARGET` — how the **ZAP container** reaches the app. On Docker Desktop use
     `https://host.docker.internal:3000` (self-signed cert; ZAP ignores it).
   - `ZAP_AUTH_USER` / `ZAP_AUTH_PASS` — a mymatasan user to scan authenticated
     surface (blank = anonymous scan only). The initial admin login is in
     `apps/mymatasan/INITIAL_ADMIN_LOGIN.txt`.
   - `config/target.env` and everything in `reports/` are git-ignored.

## Run

Windows (PowerShell):
```powershell
cd tools/zaproxy
./scan.ps1                      # safe passive baseline
./scan.ps1 -Mode api            # active API scan (prompts to confirm)
./scan.ps1 -Mode full -Yes      # full active scan, skip the prompt
./scan.ps1 -Target https://host.docker.internal:3000 -User admin -Pass 'secret'
```

Linux/Mac/WSL/Git-Bash:
```bash
cd tools/zaproxy
./scan.sh                       # baseline
./scan.sh api                   # active API scan (prompts)
./scan.sh full --yes            # full active, skip prompt
```

Reports land in `reports/<mode>-<timestamp>.html` and `.json`. Open the HTML in
a browser; the JSON is for diffing/CI.

## SPA coverage: why `full` drives a browser

mymatasan is a single-page app. ZAP's traditional spider does not execute
JavaScript, so on its own it only ever sees the login shell + static assets and
never discovers the `/api/*` endpoints the SPA calls at runtime — an active scan
built on it barely touches the real attack surface. The `full` plan therefore
adds a **`spiderAjax`** job: it launches a headless Firefox (bundled in the ZAP
image), proxied through ZAP so the replacer's Basic-auth header logs it in,
clicks through the authenticated UI, and records the API requests the app fires.
Those recorded requests are what `activeScan` then attacks. This is what makes
an authenticated scan actually exercise the API.

When running `full` against a throwaway, set `bootstrap.allowReset:false` in that
instance's config first: the AJAX spider clicks real buttons, and a stray click
on a destructive control would otherwise fire its request (the plan's
`excludePaths` stop ZAP from *attacking* those routes, but not the browser from
*navigating* to them).

## myseliasan (control plane)

myseliasan does **not** use HTTP Basic. It is a relying control-plane SPA that
authenticates federated (through myidsan SSO) but also exposes a local
bootstrap login: `POST /api/auth/local-login {username,password}` sets a session
cookie. State-changing requests then require a **double-submit CSRF token** —
the `X-CSRF-Token` header must equal the non-HttpOnly `__Host-kopiv2_csrf`
(HTTPS) / `kopiv2_csrf` (dev) cookie. So the myseliasan plans differ from
mymatasan's:

- **Authentication**: ZAP's *JSON* auth method posts the local-login body and
  captures the session cookie; *cookie* session management replays it. No Basic
  header, no `replacer`.
- **CSRF**: the active plans load `scripts/csrf-doublesubmit.js` (a ZAP
  HttpSender script) which mirrors the CSRF cookie into the `X-CSRF-Token`
  header on every request — without it every active-scan write would 403. The
  passive `baseline` plan doesn't need it (it never writes).
- **No public landing**: `/` redirects unauthenticated users to the SSO
  provider, so an *anonymous* scan barely sees anything — always scan
  authenticated.

Setup:

1. Start myseliasan (dev instance listens on TLS `:3002`). For a local-login-only
   scan you do **not** need myidsan running; the local bootstrap path is
   self-contained.
2. Copy `config/myseliasan.target.env.example` to `config/myseliasan.target.env`
   and fill in `TARGET` + `ZAP_AUTH_USER`/`ZAP_AUTH_PASS`.
   - The stock superadmin seeds as `admin`/`admin` (or whatever `localAuth` in
     the config sets) and is flagged **must-change-password** on first login.
     Clear that flag first (log in through the UI once, or
     `POST /api/auth/change-password`) and put the resulting password here —
     otherwise authenticated coverage is limited.

Run:

```powershell
./scan.ps1 -App myseliasan                 # safe passive baseline (:3002)
./scan.ps1 -App myseliasan -Mode api       # active /api scan (prompts)
./scan.ps1 -App myseliasan -Mode full -Yes # full active, skip prompt
```

```bash
./scan.sh --app myseliasan                 # baseline
./scan.sh --app myseliasan api             # active /api scan (prompts)
./scan.sh --app myseliasan full --yes      # full active, skip prompt
```

Reports land in `reports/myseliasan-<mode>-<timestamp>.{html,json}`. All the
mymatasan safety notes below apply equally; the myseliasan plans additionally
exclude the fleet-key regen, remote node wipe/reset over the tunnel, node
release/adopt, RBAC-matrix mutation, the bootstrap handoff (`elevate`), and the
`notifications/stream` SSE endpoint (a long-lived response that stalls active
scanners).

> **Prefer `api` over `full` for myseliasan; `full` is memory-hungry.** The
> active coverage of an API-first SPA comes from the **OpenAPI import** (the
> `api`/`full` plans import `/swagger/openapi.json`, adding the documented `/api`
> endpoints for activeScan to attack), *not* from the AJAX spider — myseliasan's
> `__Host-` HttpOnly session cookie can't be injected into the headless browser
> the way mymatasan's replayed Basic header can, so the AJAX spider hits the auth
> wall and discovers nothing extra. The `full` plan additionally runs that AJAX
> spider (headless Firefox), and Firefox + ZAP's auto-sized ~8 GB JVM heap
> **OOM-kills the ZAP container inside Docker Desktop's default WSL2 memory
> envelope**, aborting the activeScan phase. Either raise Docker Desktop's memory
> (Settings → Resources, ≥12 GB) or just run `api`, which reaches the same
> authenticated `/api` surface without a browser.
>
> Note: myseliasan's swagger generator emits the Gorilla-mux path parameter
> `/api/notifications/{id:[0-9]+}/read`, which isn't valid OpenAPI; ZAP logs a
> non-fatal warning and imports the remaining endpoints. The active plans set
> `failOnError: false` so that warning doesn't abort the run. Also note this is a
> JSON API behind mandatory session + CSRF auth, so ZAP's classic active scanner
> (which fuzzes URL query/form params) has few injection points — expect thin
> activeScan results even with the endpoints imported.

> **Disable rate-limiting for active (`api`/`full`) myseliasan scans.** Unlike
> mymatasan's replayed Basic header, ZAP re-authenticates myseliasan by POSTing
> `local-login` repeatedly. The dev config's `rateLimit` treats that as a flood
> and returns `429`, which makes ZAP's re-auth fail, collapses the session to
> `401`s, and aborts the active-scan job before it can report. Start the
> throwaway with `RATE_LIMIT_ENABLED=false` (env override) for a thorough active
> scan. The passive `baseline` doesn't need this.

## Known / accepted findings

- **`CSP: style-src unsafe-inline` (Medium)** — kept intentionally. The React
  front-end uses inline `style=""` attributes (dynamic chart/layout values) and
  `style-loader` injects `<style>` at runtime; CSP nonces/hashes cannot cover
  inline style *attributes*, so removing `'unsafe-inline'` from `style-src`
  would blank the UI. Script injection — the dangerous vector — is fully locked
  down (`script-src 'self'`, no `unsafe-inline`), so the residual risk is
  limited to CSS-only injection. This is the standard trade-off for React SPAs.
- **Sub-Resource-Integrity** — resolved by self-hosting the Quicksand font
  (`views/react-webpack/src/assets/fonts.css`) instead of loading it from Google
  Fonts. That removed the only cross-origin `<link>` (which can't be reliably
  SRI-hashed) and let the CSP drop the `fonts.googleapis.com` / `fonts.gstatic.com`
  domains. First-party same-origin bundles are not flagged for SRI.
- **"Modern Web Application" / "Re-examine Cache-Control" (Informational)** —
  not vulnerabilities; no action.

## Tuning notes

- **Rate limiting:** the dev config enables `rateLimit` (~120 auth req / 20 s).
  A scan will hit `429`s, which slows it and adds noise. For a thorough run,
  raise or disable `rateLimit` in the instance's `config.dev.json` first.
- **Login lockout:** the failed-login lockout only guards `GET .../auth/session`,
  which the active plans exclude, so scanning won't self-lock the admin.
- **Exit code:** ZAP exits non-zero when it raises alerts or a job warns — the
  scripts surface this. That's expected, not a tooling error; read the report.
- **Custom image/tag:** override with `-Image` (ps1) or `ZAP_IMAGE` (sh).

## Files

```
scan.ps1 / scan.sh                   entrypoints (Windows / POSIX); -App/--app selects the app
config/target.env.example            mymatasan — copy to config/target.env
config/myseliasan.target.env.example myseliasan — copy to config/myseliasan.target.env
plans/baseline.yaml                  mymatasan passive plan (safe)
plans/api.yaml                       mymatasan active plan, /api only (destructive routes excluded)
plans/full.yaml                      mymatasan active plan, whole app (destructive routes excluded)
plans/myseliasan-baseline.yaml       myseliasan passive plan (JSON auth + cookie session)
plans/myseliasan-api.yaml            myseliasan active plan, /api only (+ CSRF script)
plans/myseliasan-full.yaml           myseliasan active plan, whole app (+ CSRF script + AJAX spider)
scripts/csrf-doublesubmit.js         HttpSender script: mirrors CSRF cookie into X-CSRF-Token
reports/                             generated HTML+JSON (git-ignored)
```
