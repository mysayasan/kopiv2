# myiotsan

`myiotsan` is the suite's on-prem IoT device hub — "the NVR, but for sensors." It is the
fourth app in the suite, alongside `mymatasan` (camera NVR), `myseliasan` (fleet control
plane), and `myidsan` (identity/SSO). It is built as an appliance on the same runtime host as
`mymatasan`: a single binary, on-prem, air-gapped-capable, and adoptable into the `myseliasan`
fleet over the existing pairing/control channel.

**This is P0: scaffolding.** The app boots, authenticates, and serves its SPA shell. The IoT
domain — device inventory, telemetry ingest, rules, alerts — is deliberately **absent**
rather than stubbed, so nothing shipped here pretends to work before it does. See
`docs/MYIOTSAN_PLAN.md` for the full roadmap (P1 ingest spine, P2 rules & alerts, P3
discovery/onboarding, P4 actuation, P5 industrial protocols, P6 fleet adoption, P7 release
packaging).

## Authentication

`myiotsan` reuses mymatasan's local-auth stack, extracted to `domain/shared` so both appliance
apps run the same security-critical code instead of two forks:

- DB-backed local users (`local_user` table, shared entity/service), bcrypt password hashing,
  a bcrypt-verification cache.
- **Session-cookie login** (`POST /api/auth/login`) as the primary sign-in path — unlike
  mymatasan, which authenticates by replaying an HTTP Basic header on every request (costing a
  bcrypt verification per request), myiotsan exchanges the credential once for a session
  cookie. HTTP Basic still works for API clients and scripts.
- A forced-password-change gate on first login (`must_change_password`), a failed-login
  lockout with escalating backoff (`loginSecurity` config), and a three-role permission
  matrix (`viewer`/`operator`/`admin`) that decides **every** request, not just writes —
  deny-by-default (`apps/myiotsan/services/rbac.go`).
- On first startup, the bootstrap admin account is seeded from `localAuth.username`/
  `localAuth.password` in config (or `LOCAL_ADMIN_PASSWORD` env, or a generated per-install
  password when neither is set) and is always must-change. The credential is revealed via a
  console banner and a `INITIAL_ADMIN_LOGIN.txt` recovery file written to the data dir —
  delete it after signing in.

## Role model

Three roles, drawing the same line mymatasan draws — **can this person destroy the record?**

- `viewer` — see devices and their current readings, and see that an alert fired. No access to
  historical telemetry.
- `operator` — + review telemetry history, acknowledge alerts. Cannot actuate a device, delete
  readings, or change rules and settings.
- `admin` — everything.

myiotsan draws a **second** line mymatasan does not need: actuation (writing to a device, e.g.
a relay) is admin-only, because a bad write to a physical device is dangerous in a way a bad
camera PTZ move is not. This lands with the command path in P4 and is not to be loosened
without a deliberate decision.

The P0 authorization catalog is intentionally small — it lists only what P0 actually serves
(`/api/auth/change-password`, and the admin-only user/role management routes) rather than
naming routes that don't exist yet.

## Configuration

- `apps/myiotsan/config.json` / `config.dev.json` — HTTPS port `3003` (myidsan `3001`,
  myseliasan `3002`, mymatasan `3000`), SQLite by default (`./data/myiotsan.db`).
- `apps/myiotsan/certs/` — dev TLS cert/key.
- No new fleet ports: when myiotsan is later adopted into a myseliasan fleet (P6), it will
  dial the same discovery/pairing/control ports mymatasan nodes already use.

Run locally:

```bash
go run . -app myiotsan
```

## Frontend

The SPA (`apps/myiotsan/views/react-webpack/`) is built off the shared `@shared` frontend
module the same way myseliasan's is — no per-app copy of `DataTable`/`SideNav`/icons/i18n. In
P0 it renders the login flow and an SPA shell; there is no application content behind it yet.
