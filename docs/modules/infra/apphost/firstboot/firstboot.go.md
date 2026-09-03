# Module: infra/apphost/firstboot/firstboot.go

## Purpose

Defines package `firstboot` — the pre-boot setup wizard: a small embedded web page that
configures the infrastructure an app needs to boot AT ALL (`db`, `cache`, the listen
address, the bootstrap administrator) before that infrastructure has been read for real.

Every one of those blocks is read by `infra/apphost` exactly once, at boot, before the
app is ever handed a database handle. That ordering has a hard consequence: an install
whose `db`/`cache` settings are wrong cannot be fixed from inside the app, because the app
never gets far enough to serve a page — the in-app Settings screen (`apps/myseliasan/
services/settings.go.md`) only works once the app is already up. So this wizard runs
BEFORE any of that, in the same process, on its own port: it needs no database, no cache,
no session, and no app wiring. It writes `config.json` and returns; `infra/apphost/run.go`
then continues the normal boot sequence in the same process and reads the file this
package just wrote — no restart, no supervisor dependency.

This is a genuinely different mechanism from each app's existing IN-APP "first-run setup
wizard" (`domain/shared/services/setup_state.go`, `docs/TECHNICAL_SPEC.md`'s "Suite-wide
first-run setup seam"): that one runs AFTER the app is up and a superadmin has signed in,
walks through app-level onboarding (sites, node adoption, etc.), and is gated by a
`RuntimeSetting` DB row. This package runs before there is a database to hold that row at
all, and only ever asks about the handful of settings the host itself needs.

## `Needed(configPath string) (bool, string, error)`

Decides whether the wizard should run for this config file, and reports why — narrowly on
purpose (see the file-level comment): reachability of any dependency is never consulted, a
transient database outage must never flip a running fleet control plane into a
configuration wizard.

- `KOPIV2_SETUP=1` (or `true`/`yes`/`on`/`force`, case-insensitive) forces it on;
  `KOPIV2_SETUP=0` (or `false`/`no`/`off`/`never`) forces it off — the recovery path for an
  install that can no longer boot, and the escape hatch for an operator who wants to skip
  it despite the config saying otherwise. An unrecognized value falls back to the config
  file rather than silently meaning "off" (`envSetupOverride`).
- Otherwise reads `configPath` and looks at the top-level `"setup"` block
  (`SetupBlockKey`, `readSetupBlock`): a missing config file, or a config with no `setup`
  key at all, answers "not needed" — an install that predates this feature, or one an
  operator configured by hand, is already set up and is never ambushed. `setup.completed`
  is read as a `*bool` (`setupBlock.Completed`) specifically so "absent" and "explicitly
  false" are distinguishable: only an EXPLICIT `false` triggers the wizard.

## Answer types

`DBSettings`, `CacheSettings`, `WebSettings`, `AdminSettings`, bundled as `Answers` — the
whole wizard, as submitted. `DBSettings`/`CacheSettings` mirror `dbsql.DbConfigModel`'s and
the redis cache config's JSON shape in `config.json` (snake_case `db_name`/`ssl_mode`).
`AdminSettings.Password` blank means "keep whatever the config already has, including
none" — the app then generates one on first run and announces it, same as every other app.

## `Options` / `Run` / `Result`

`Options` is what the host (`infra/apphost/firstboot_hook.go`, `firstBootOptions`) hands
in: `AppName`, `ConfigPath`, `DataDir`, an optional `Logf`, an optional `Browser` (opens
the setup URL and reports whether/why it did not — see `infra/apphost/browser.go.md`), and
optional `ProbeDB`/`ProbeCache` (attempt a real connection with the operator's answers so
a bad setting is caught here instead of on the boot that follows; a nil probe reports "not
verified" rather than pretending success). `Run` (see `server.go.md`) serves the page,
blocks until the operator finishes (or `ctx` is cancelled), and returns a `Result`
(`ConfigPath`, `StartURL` — where the real app will answer once boot continues, derived
from the answers, not from a reload of the config that has not happened yet).

## `currentAnswers` / `commit`

- `currentAnswers(raw []byte) (Answers, error)` reads the existing config into the
  wizard's shape, so every field opens pre-filled with what the install already ships
  rather than a blank form — including translating the legacy single `server.ports` list
  into whichever of `tlsPorts`/`nonTlsPorts` the config's TLS switch says it really serves.
- `commit(configPath string, a Answers) error` writes the answers back as explicit leaf
  patches via the shared `infra/config/configfile` package (`infra/config/configfile/
  patch.go.md`) — `configfile.Materialize` — so every block the wizard does not ask about,
  and every untouched key inside the blocks it does, keep their exact original bytes. The
  `cache.redis.*` leaves are written only when Redis is actually the chosen provider,
  so picking the in-process cache never stamps a blank address over a working one. The
  final patch is always `{SetupBlockKey, "completed"}, true`.

## Notes

- `isSQLite`/`isRedis` are the two engine/provider checks the form and validation
  (`validate.go.md`) both key off.
- See `server.go.md` for the HTTP server, listener/port selection, the security headers
  and remote-access token, and the console/`SETUP_URL.txt` announcement; `validate.go.md`
  for per-field validation; `infra/apphost/firstboot_hook.go.md` for how `infra/apphost`
  wires this package into `runApp`.
