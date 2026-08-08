# zaproxy — OWASP ZAP security scanner for mymatasan / myseliasan / myiotsan / myidsan

Developer/security tooling that points [OWASP ZAP](https://www.zaproxy.org/)
(the `ghcr.io/zaproxy/zaproxy:stable` Docker image) at a running **mymatasan**,
**myseliasan**, **myiotsan**, or **myidsan** instance and produces HTML + JSON
vulnerability reports.

Like `tools/tgbridge`, this is **developer tooling only** — not part of any
shipped app, no runtime dependency on the apps/domain/infra, and not covered by
`docs/modules/` or the version changelog.

Pick the app with `-App` (ps1) / `--app` (sh); default is `mymatasan`. Each app
authenticates differently, so each has its own plans + config file:

| App | Port | Auth model | Plans | Config |
|-----|------|------------|-------|--------|
| `mymatasan` (default) | 3000 | HTTP Basic (replayed every request) | `plans/<mode>.yaml` | `config/target.env` |
| `myseliasan` | 3002 | JSON login → session cookie + double-submit CSRF | `plans/myseliasan-<mode>.yaml` | `config/myseliasan.target.env` |
| `myiotsan` | 3003 | JSON login (appliance auth stack, own login path) → session cookie, no CSRF token | `plans/myiotsan-<mode>.yaml` | `config/myiotsan.target.env` |
| `myidsan` | 3001 | JSON login (own login path) → session cookie + double-submit CSRF | `plans/myidsan-<mode>.yaml` | `config/myidsan.target.env` |

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
> Note: this is a JSON API behind mandatory session + CSRF auth, so ZAP's classic
> active scanner (which fuzzes URL query/form params) has few injection points —
> expect thin activeScan results even with the endpoints imported.
>
> Historical note, because the failure mode was invisible: myseliasan's swagger
> generator used to emit the Gorilla-mux path parameter
> `/api/notifications/{id:[0-9]+}/read`. That is not merely cosmetic — it made the
> **whole document** invalid, ZAP recorded an error, and the automation framework
> **terminated the run during `passiveScan-wait`, before `activeScan` and `report`
> ever started**. `failOnError: false` did not prevent it. The scan produced no
> report at all, which is easy to mistake for a flaky container. Fixed at source in
> `infra/apidocs` (paths now emit bare `{id}`); guarded by
> `TestBuildSpecStripsMuxRegexFromPathParameters`.

> **Disable rate-limiting for active (`api`/`full`) myseliasan scans.** Unlike
> mymatasan's replayed Basic header, ZAP re-authenticates myseliasan by POSTing
> `local-login` repeatedly. The dev config's `rateLimit` treats that as a flood
> and returns `429`, which makes ZAP's re-auth fail, collapses the session to
> `401`s, and aborts the active-scan job before it can report. Start the
> throwaway with `RATE_LIMIT_ENABLED=false` (env override) for a thorough active
> scan. The passive `baseline` doesn't need this.

## myiotsan (IoT device hub)

myiotsan is on the same appliance local-auth stack as mymatasan (see
`domain/shared`), but rides a **session cookie** rather than replaying HTTP
Basic: `POST /api/auth/login {username,password}` (its own path — not
myseliasan's `/api/auth/local-login`) sets an `HttpOnly`, `SameSite=Lax` cookie.
That `SameSite=Lax` is the CSRF defence, so unlike myseliasan there is **no
CSRF token and no `csrf-doublesubmit.js` script** in the myiotsan plans.

**myiotsan is not a read-only dashboard — it actuates physical hardware**
(relays, locks, valves), and the plans are scoped around that on purpose:

- **`plans/myiotsan-{api,full}.yaml` deliberately exclude**
  `/api/devices/{id}/commands` (issuing an actuation command),
  `/api/devices/{id}/password` (rotating a device's broker credential — this
  revokes the device), `/api/pairing/*` (adopt/release/fleet-key), the
  discovery/enrollment endpoints (opening a window admits unknown devices), and
  `/api/settings/users/*` (could disable the admin mid-scan) — on top of the
  same destructive-route exclusions the other apps' plans carry
  (`/api/system/*`, `*/purge`, DELETE routes).
- **This is deliberate, not a coverage gap.** An active scanner fuzzing an
  actuation endpoint is a **physical-hardware risk**, not a data one — a
  "throwaway" bench instance may still be wired to a real relay or lock. Do
  **not** "fix" this by adding those routes back to the plan without a
  deliberate decision to do so against real, disconnected hardware only.

Setup:

1. Start myiotsan (dev instance listens on TLS `:3003`).
2. Copy `config/myiotsan.target.env.example` to `config/myiotsan.target.env` and
   fill in `TARGET` + `ZAP_AUTH_USER`/`ZAP_AUTH_PASS`. There is deliberately **no
   shipped default password** (dev config uses `admin123`; a packaged install
   generates one into `INITIAL_ADMIN_LOGIN.txt`). The account is
   must-change-password on first login — clear that first and put the
   *resulting* password here, or authenticated coverage is limited.

Run:

```powershell
./scan.ps1 -App myiotsan                 # safe passive baseline (:3003)
./scan.ps1 -App myiotsan -Mode api       # active /api scan (prompts; excludes actuation)
```

```bash
./scan.sh --app myiotsan                 # baseline
./scan.sh --app myiotsan api             # active /api scan (prompts; excludes actuation)
```

Reports land in `reports/myiotsan-<mode>-<timestamp>.{html,json}`. Same
rate-limiting caveat as myseliasan: start the throwaway with
`RATE_LIMIT_ENABLED=false` before an active run, or `429`s from ZAP's repeated
re-login will collapse the session and abort the scan.

## myidsan (identity provider)

myidsan is JSON login + cookie session like myseliasan, but on its own login
path — `POST /api/login/default {username,password}` sets the session cookie
(`kopiv2_access`) — and, like myseliasan, requires a **double-submit CSRF
token** for state-changing requests (`X-CSRF-Token` must equal the readable
`kopiv2_csrf` cookie), so the active plans load the same
`scripts/csrf-doublesubmit.js` script.

**myidsan needs more care than the other three apps because it is the identity
provider the whole suite depends on**, which changes what "safe to scan" means
in two ways that have no analogue elsewhere in this repo:

- **Exclusions have to cover "would break the scanner itself", not just
  "destructive".** myidsan can revoke the scanner's own session, clear its
  MFA, change its password, disable its account, or mutate the RBAC matrix out
  from under it — after which the rest of the run is a wall of 401s that looks
  like coverage but is not. `plans/myidsan-{api,full}.yaml` exclude that whole
  class (`/api/session/*`, `/api/session-admin/*`, `/api/mfa/*`,
  `/api/mfa-admin/*`, `/api/access-rbac*`, the user-DELETE route,
  change-password), alongside the genuinely destructive routes that reach
  beyond this app (`/api/backup*`, `/api/sso-ca*` — regenerating the CA breaks
  fleet trust, `/api/app-auth-config/*/secret` — rotating a client secret
  breaks **every relying app's** logins, redirect-URI/app-registry/directory
  DELETE and reconfig routes, password-reset resolution, `/api/step-up*`).
  Deliberately left **in** scope, because it is the actual point of scanning
  an IdP: `/api/auth/authorize`, `/api/auth/token`, the login endpoints, and
  MFA verification.
- **The failed-login lockout must be off, not just the rate limiter.**
  myidsan locks out per IP *and* per account, and an active scanner hammering
  `/api/login/default` trips both within seconds — turn `loginSecurity.enabled`
  and `rateLimit.enabled` off on the throwaway target before an `api`/`full`
  run, the same as the rate-limit caveat below but with an extra knob. Both are
  security features working correctly; disabling them is only ever for a
  throwaway.

Setup:

1. Start myidsan (dev instance listens on TLS `:3001`).
2. Copy `config/myidsan.target.env.example` to `config/myidsan.target.env` and
   fill in `TARGET` + `ZAP_AUTH_USER`/`ZAP_AUTH_PASS`. There is no shipped
   default password — the bootstrap superadmin's password is generated per
   install into `INITIAL_ADMIN_LOGIN.txt` in the data dir. The account is
   must-change-password on first login: clear that flag first (sign in once,
   or `POST /api/login/default/change-password`) and put the *resulting*
   password here, or every authenticated endpoint 401s
   `password_change_required` and the scan finds almost nothing.

Run:

```powershell
./scan.ps1 -App myidsan                 # safe passive baseline (:3001)
./scan.ps1 -App myidsan -Mode api       # active /api scan (prompts; excludes the "breaks the scanner" + "breaks other apps" routes above)
./scan.ps1 -App myidsan -Mode full -Yes # full active, skip prompt
```

```bash
./scan.sh --app myidsan                 # baseline
./scan.sh --app myidsan api             # active /api scan (prompts)
./scan.sh --app myidsan full --yes      # full active, skip prompt
```

Reports land in `reports/myidsan-<mode>-<timestamp>.{html,json}`. The same
`failOnError: false` note as myseliasan applies (the swagger generator emits
the Gorilla-mux `{id:[0-9]+}` path parameter, which is not valid OpenAPI and
logs a non-fatal import warning); `full` carries the same AJAX-spider memory
caveat, so prefer `api` unless Docker Desktop's memory is raised.

## `${VAR}` substitution: the two silent-failure traps

ZAP's Automation Framework substitutes `${VAR}` in job and context **parameters**,
but **not in every nested block** — and where it doesn't, nothing warns you. Two
plan fields were bitten, and each one silently destroyed a whole scan while still
looking like a healthy run:

| Field | What ZAP received | Symptom |
|---|---|---|
| `replacer` job's `rules:` → `${ZAP_AUTH_HEADER}` | the literal string | mymatasan's `Authorization` header was nonsense, **every authenticated `/api` request 401'd**, the scan covered only the public surface — and still printed "Automation plan succeeded!" |
| `authentication.verification.pollUrl` → `${TARGET}` | the literal string | the poll URL wasn't absolute, every re-auth poll threw `URIException`, and **ZAP terminated mid-plan before `activeScan`/`report`** (myseliasan/myidsan/myiotsan) |

`scan.sh`/`scan.ps1` therefore **render those two placeholders themselves** into a
throwaway `plans/.rendered-<stamp>-<plan>.yaml` (git-ignored; it holds a live
credential) and hand ZAP that file. `${STAMP}` is deliberately left to ZAP — it is
only used in report filenames, where substitution does work.

**The lesson worth keeping: "plan succeeded" is not evidence the scan was
authenticated, and a missing report is not automatically a flaky container.** The
mymatasan active plans now open with a `requestor` job that asserts
`GET /api/cameras` returns 200, so a broken credential fails loudly and
immediately instead of yielding a clean-looking, empty report. When adding a new
app's plan, add the equivalent preflight, and confirm coverage by checking the
target's own access log for 401s rather than trusting the ZAP summary.

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
- **"SQL Injection" (High) on `{id}` path segments** — **false positive**, verified
  by hand on both apps (`/api/cameras/{id}/ptz/stop`, `/api/floors/{id}/image`).
  ZAP infers injection from response similarity, but every one of these ids is
  parsed with `ParseUint`/validated *before* any DB access, so `... AND 1=1 --`,
  `... AND 1=2 --` and a plain non-numeric id all collapse to the identical
  `400 invalid id`. Identical responses are exactly what the heuristic reads as a
  successful injection. Re-verify the same way if it reappears: compare the `1=1`
  and `1=2` bodies — if they are byte-identical and no SQL error surfaces, it is
  this. Do not dismiss it without that check.
- **"Exposed Secrets in Swagger/OpenAPI Path" (High) on myseliasan
  `GET /api/settings`** — **false positive**. The endpoint returns every secret
  field *empty* alongside a companion `…Set` boolean (`clientSecret: ""` +
  `clientSecretSet: false`); ZAP matches on field *names*, not values. That
  shadow-field pattern is the intended design — the check to run is that the
  values are blank, which they are.
- **myseliasan `__Host-kopiv2_csrf` "Cookie No HttpOnly Flag" (Low)** — by design.
  Double-submit CSRF requires the front-end to *read* the cookie and echo it in
  `X-CSRF-Token`; HttpOnly would break the defence it implements. The session
  cookie beside it (`__Host-kopiv2_access`) is HttpOnly, which is the one that
  matters.

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
config/myiotsan.target.env.example   myiotsan — copy to config/myiotsan.target.env
config/myidsan.target.env.example    myidsan — copy to config/myidsan.target.env
plans/baseline.yaml                  mymatasan passive plan (safe)
plans/api.yaml                       mymatasan active plan, /api only (destructive routes excluded)
plans/full.yaml                      mymatasan active plan, whole app (destructive routes excluded)
plans/myseliasan-baseline.yaml       myseliasan passive plan (JSON auth + cookie session)
plans/myseliasan-api.yaml            myseliasan active plan, /api only (+ CSRF script)
plans/myseliasan-full.yaml           myseliasan active plan, whole app (+ CSRF script + AJAX spider)
plans/myiotsan-baseline.yaml         myiotsan passive plan (JSON auth + cookie session)
plans/myiotsan-api.yaml              myiotsan active plan, /api only (actuation/pairing/enrollment excluded)
plans/myiotsan-full.yaml             myiotsan active plan, whole app (actuation/pairing/enrollment excluded)
plans/myidsan-baseline.yaml          myidsan passive plan (JSON auth + cookie session)
plans/myidsan-api.yaml               myidsan active plan, /api only (+ CSRF script; IdP-specific exclusions)
plans/myidsan-full.yaml              myidsan active plan, whole app (+ CSRF script + AJAX spider; IdP-specific exclusions)
scripts/csrf-doublesubmit.js         HttpSender script: mirrors CSRF cookie into X-CSRF-Token (myseliasan + myidsan)
reports/                             generated HTML+JSON (git-ignored)
```
