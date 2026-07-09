# zaproxy — OWASP ZAP security scanner for mymatasan

Developer/security tooling that points [OWASP ZAP](https://www.zaproxy.org/)
(the `ghcr.io/zaproxy/zaproxy:stable` Docker image, already pulled on this
machine) at a running **mymatasan** instance and produces HTML + JSON
vulnerability reports.

Like `tools/tgbridge`, this is **developer tooling only** — not part of any
shipped app, no runtime dependency on the apps/domain/infra, and not covered by
`docs/modules/` or the version changelog.

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
scan.ps1 / scan.sh          entrypoints (Windows / POSIX)
config/target.env.example   copy to config/target.env
plans/baseline.yaml         passive plan (safe)
plans/api.yaml              active plan, /api only (destructive routes excluded)
plans/full.yaml             active plan, whole app (destructive routes excluded)
reports/                    generated HTML+JSON (git-ignored)
```
