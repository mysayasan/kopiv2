# Module: infra/apphost/firstboot/validate.go

## Purpose

Per-field and whole-form validation for the wizard's submitted `Answers`, aimed at giving
the operator a message naming the field WHILE the form is still on screen, rather than
finding out the value was unusable on the boot that follows.

## `validate(a *Answers, reservedPort int) error`

Runs `validateDB` → `validateCache` → `validateWeb` → `validateAdmin` in order, returning
the first failure. `reservedPort` is the wizard's own listening port (see
`server.go.md`); `validateWeb` refuses an app port equal to it.

## `validateDB` / `validateCache`

- `validateDB`: engine defaults to `postgres` when blank, must be one of
  `supportedEngines` (`postgres`, `mariadb`, `sqlite`). For SQLite, only a non-empty
  `db_name` (file path) is required — host/user/password/ssl_mode/port left over from a
  previously-selected engine are cleared rather than carried into the file, where they
  would read as configuration that means something. For a server engine, host/port
  (1-65535)/user/db_name are all required.
- `validateCache`: provider defaults to `default` (in-process) when blank, must be one of
  `supportedCacheProviders` (`default`, `redis`). Redis additionally requires a non-empty
  address and a non-negative DB index.

## `validateWeb(w *WebSettings, reservedPort int) error`

Normalizes port lists (`normalizePorts` drops zero/blank rows the form sends for empty
fields), defaults `Hostnames` to `["*"]` when empty, and requires at least one HTTP or
HTTPS port. `EnableTLS` is reconciled with the TLS port list rather than trusted blindly:
switching it on with no TLS port is refused, and TLS ports typed in with the switch off
are taken as intent (`EnableTLS` is flipped on) rather than silently dropped. Every port
across both lists must be in range, unique, and not equal to `reservedPort` — the wizard's
own port, which the app would otherwise fail to bind on the very next boot.

## `validateAdmin(a *AdminSettings) error`

A disabled local-auth admin skips validation entirely. Enabled requires a non-empty
username; the password may be blank (keep the stored one — or, if there is no stored one,
the app generates one on first run and announces it, as usual), but a non-blank password
must be at least 8 characters.

## Notes

- `contains`/`normalizePorts` are small shared helpers used across the validators.
- `isSQLite`/`isRedis` (in `firstboot.go.md`) are the engine/provider checks this file's
  branching is built on.
