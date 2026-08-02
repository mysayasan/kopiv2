# Module: apps/myidsan/apis/settings.go

## Purpose

HTTP surface for the in-app editor over the safe subset of `config.json`
(`services.ISettingsService`, `services/settings.go.md`). Ported from myseliasan's pattern
(`docs/MYIDSAN_PRODUCTIZATION_PLAN.md` §4.1). Backed by the SPA's **Settings** page
(`views/react-webpack/src/views/components/settings.js`) — a tabbed editor over these
routes with per-field (i) info tips, masked secrets, and a "restart required" banner wired
to `apis/system.go.md`'s `POST /api/system/restart`.

## Endpoints

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/settings` | Every editable section (secrets masked) plus `sections` (the display-order id list). |
| `POST` | `/api/settings/cache/test` | Live Redis connectivity test (`services.ISettingsService.TestCache`); body capped at 256 KiB (`maxSettingsBody`). A blank `password` (and `address`) falls back to the stored value. Returns `{"ok": true}` on success; a failed ping is a 400 with the connection error as the message. Never writes anything. |
| `GET` | `/api/settings/{section}` | One section's values, secrets masked. Unknown `{section}` → 400. |
| `PUT` | `/api/settings/{section}` | Validate + persist a section; body capped at 256 KiB. Response is `SaveResult{needsRestart}`. |
| `POST` | `/api/settings/{section}/reset` | Restore a section to its captured first-run defaults. Response is `SaveResult{needsRestart}`. |

The `cache/test` literal route is registered on the `/settings` subrouter **before** the
`/{section}` var routes so gorilla/mux's `{section}` pattern never captures it.

Unlike myseliasan's port, there is **no `GET /api/settings/fs/browse`** file-browse endpoint.
That endpoint enumerates directories on the host; myidsan is the identity provider — the
highest-value target in the suite, and the one whose compromise reaches every other app. The
two host paths an operator can set here (the log file, the file-storage root) are chosen once
at install and can be typed.

## Authorization

Superadmin enforced as **middleware** on the whole `/settings` route group —
`auth.Middleware` + `access.Middleware` + `access.RequireSuperadmin` — matching how every
other sensitive myidsan surface is gated (audit, backup, mfa-admin, session-admin). This
differs from myseliasan's per-handler `requireSuper` wrapper: these values include the JWT
secret and the lockout policy, and are never delegated to a lesser role via the permission
matrix regardless of what it says.

## Constructor

`NewSettingsApi(router, auth, access, settings, audit, trustedProxies)` — mounts the five
routes on a `/settings` subrouter. `settings` is `services.ISettingsService`; `audit` is
`services.IAuditService`; `trustedProxies` is `deps.Config.RateLimit.TrustedProxies`, threaded
into the shared `auditRecorder` so a recorded client IP agrees with the rate limiter's notion
of a trusted proxy. Registered in `app.go`'s `RegisterAppRoutes`, with `settingsService` built
from `deps.Config`, `deps.ConfigPath`, `deps.Db`, the fleet `secretCipher`, and a `log.Printf`
closure.

## Audit trail

`saveSection`/`resetSection` each call `recordChange`, which writes a best-effort audit entry
(`settings.save` / `settings.reset`, `TargetType: "settings"`, `TargetId: <section>`) via
`services.IAuditService` — **never the values themselves**, since a section can carry the JWT
secret, the SSO internal token, or a Redis password; only which section changed and by whom.
`getAll`/`getSection`/`testCache` are reads (or, for `testCache`, a non-persisting
connectivity check) and are not audited.

## Notes

- Seeded as an `api_endpoint` row in `app.go`'s `Seeders` (`Title: "Settings"`, `Path:
  /api/settings`, `AccessTier: DevOnly`, with menu metadata placing it in the System group)
  for rate-limiting/runtime metadata and nav — the superadmin gate itself is enforced by the
  route-group middleware above, not by this seed row.
- Applying a saved/reset change requires a restart. myidsan now has an in-app restart
  action too — `POST /api/system/restart` (`apis/system.go.md`, mounted right after this
  API in `app.go`), the same pattern myseliasan uses — so the Settings page's
  `needsRestart` banner has a Restart button rather than only naming the requirement.
- `testCache` is purely diagnostic from the API's perspective — it can never write a file,
  read file contents, or change the live config; see `services/settings.go.md`'s `TestCache`.
