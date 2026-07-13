# MyMataSan — Tier 2 (architecture) Plan

Status: **IN PROGRESS** (started 2026-07-13). D1/D2, C, and now M are done; R remains.

Tier 0 (data-loss correctness, PR #75) and Tier 1 (resilience + metrics, PRs #76/#77) are
done. Tier 2 is the architectural debt — the tier that decides whether [MyIotSan](./MYIOTSAN_PLAN.md)
inherits a clean platform or a copy of the problems.

Agreed sequence: **D → C → M → R.** Decomposing first is cheap and makes the config seam
far easier to cut, because the wiring is already split by subsystem. The config seam was
myiotsan's actual blocker — it is now shipped (Phase C, below), so myiotsan is unblocked on
this front. Migrations are the highest *latent* risk but block nothing today. RBAC is the
biggest product change and goes last.

---

## Phase D — Decompose the composition root

`RegisterAppRoutes` is **792 lines** holding 14 responsibilities, with ordering contracts
enforced by comments rather than types.

### D1 — One `RecorderConfig` builder  ← *first, and it fixes real bugs* — **DONE**

A `RecorderConfig` is built **three times**, in three places, each subtly different:

| Site | Codec / decoder settings | Stream URL preference |
|---|---|---|
| `app/app.go` startup fan-out | captured **at boot** (`recSettings` at L305) | `StreamURL` → discovered |
| `apis/recording.go` save-config | read **live** | `StreamURL` → discovered |
| `app/app.go` `DetectStreamConfig` | read live | `StreamURL` → `LiveStreamUrl` → discovered |

This duplication is not cosmetic — it has already caused two bugs:

- **`ShredPasses` was silently dropped** by the API site (Tier 0, fixed in #75): secure
  shred degraded to a plain unlink the moment an operator saved any recording setting.
- **The at-rest codec is captured at boot** by the app site, so changing it in Settings
  does not apply until a restart — despite the code claiming otherwise.

Every new `RecorderConfig` field must currently be remembered in three places or it is
silently lost in the others. One builder removes the whole class.

**Deliverable:** `services.RecorderConfigBuilder` — the single thing that knows how to
turn a stored per-camera recording config into a runnable recorder (resolve stream URL +
inject credentials, read live decoder/storage settings, attach cipher/shred/metrics).
All three sites call it. Fixes the boot-captured-codec bug as a side effect.

### D2 — Split `RegisterAppRoutes` into per-subsystem wiring ← **DONE**

```
app/app.go            module manifest + RegisterAppRoutes (sequences the builders below)
app/wire_security.go  at-rest key + recovery-mode early return
app/wire_storage.go   repositories
app/wire_vision.go    detector backend + model paths (detectorModelPaths)
app/wire_fleet.go     enrollment, control + media channels
app/wire_monitors.go  starting the monitors + the periodic purges
app/wire_routes.go    middleware chain + the API groups
app/wire_services.go  the wiring struct threaded between phases
app/config_map.go     the *FromAppConfig mappers (already pure and tested — just moved)
```

`RegisterAppRoutes` went from 792 lines to ~490. No behavior change; two ordering hazards
were converted from convention into type as part of the move:

1. **`deps.Config` was mutated in place** (`Vision.Detector.Args` resolved), and three
   later calls depended on that write having happened. Move one line and training silently
   resolved the wrong worker script — no compile error, no failing test. Fixed: the
   resolved args are now a value (`detectorModelPaths.DetectorArgs`, `wire_vision.go`)
   every consumer takes as an explicit parameter. `deps.Config` is no longer mutated
   anywhere in `app.go`.
2. **Four `os.Setenv` calls** (`MYMATASAN_ACTIVE_MODEL_FILE`, `_STOCK_`, `_LPR_`,
   `_ANOMALY_FILE`) are how Go tells the Python worker where the model is. Process-global,
   invisible to the type system, and impossible to run two instances in one process. Fixed
   to the extent a mechanical decomposition should: the four calls collapsed into one type
   (`detectorModelPaths`) with one publication point (`PublishToProcessEnv()`), called once
   from the composition root. `os.Setenv` no longer appears in `app.go`.

**Follow-up — phase D3 (not started):** the env-var channel to the Python worker still
exists, because several spawn sites (`vision_tool`, `teach_anomaly`, `training_runner`)
inherit the process environment rather than being handed `detectorModelPaths` (or its
`Env()` output) directly. Removing `os.Setenv` entirely means threading the typed value
into each of those spawn sites — real work, not something to fold into D2's mechanical
move. See the `detectorModelPaths` type comment in `wire_vision.go`.

**Also fixed while decomposing** (latent bugs, not behavior changes for a correctly
configured install): the three fleet loops (`enrollmentManager.Run`, `controlChannel.Run`,
`mediaChannel.Run`) were bare `go` calls — a panic in any of them silently killed the whole
process, with nothing in the logs to say the node had stopped enrolling / answering the
parent / relaying video. All three are now `safego.Supervise`d.

See `docs/modules/apps/mymatasan/app/*.go.md` for the full per-file breakdown.

---

## Phase C — Per-app config seam  *(myiotsan's blocker)* — **DONE**

Nine of ~25 blocks in the shared `config.AppConfigModel` were mymatasan-only. Worse, the
leak ran both ways: `infra/apphost/run.go` resolved **YOLO training directories** — the
generic application host had hardcoded knowledge of a vision feature.

**Shipped, six of those nine blocks moved** (`Camera`, `Decoder`, `Stream`, `Vision`,
`Health`, `Recording`) to a new `apps/mymatasan/config` package. The other three
(`Notification`, `LoginSecurity`, `Security`) turned out to belong shared, not
mymatasan-only, on inspection (`Security` in particular — `myseliasan` also uses
encryption-at-rest) — the seam line was a judgement call verified by grep, not a mechanical
"is this app's" test: what moved is only what nothing else reads.

**What was actually built** differs from the original sketch below in one deliberate way:
no nested `"app"` key. `infra/config.AppConfigModel` gained an unexported `raw []byte` field
+ `Raw()` accessor instead, retaining the exact document `LoadAppConfiguration` decoded. An
app implementing the new `apphost.AppConfigDecoder` interface
(`DecodeAppConfig(raw []byte, dataDir string) error`) decodes its own blocks straight out of
that same raw document — the blocks stay at the top level of `config.json`, unchanged, so no
deployed config file needs migrating. `infra/apphost/run.go` calls it once, after the shared
config is loaded and normalized and before any route is registered; an error aborts
startup. `apps/mymatasan/config.Config.Normalize(dataDir)` replaced the `ResolveWritablePath`
calls that used to live in `infra/apphost/run.go`'s `normalizePathConfig` for the snapshot
dir and training dir.

**Bug fix found along the way:** `infra/config.LoadAppConfiguration` used to call `Decode`
and discard the error — a malformed `config.json` silently produced an all-zero config with
no indication anything was wrong. It now returns the error, and both the shared loader and
`apps/mymatasan/config.Load` fail loudly on a syntax error instead.

Collapsed, as a side effect, 7 of the 11 `*FromAppConfig` mappers in `config_map.go` now
take `*mmconfig.Config` instead of `*config.AppConfigModel` (the other 4, for blocks that
stayed shared, are unchanged) — not full struct-triplication removal, but the blocks these
mappers translate are no longer duplicated between the shared model and the app.

See `docs/modules/apps/mymatasan/config/config.go.md`,
`docs/modules/infra/config/config_models.go.md`, and `docs/modules/infra/apphost/types.go.md`
for the full breakdown.

---

## Phase M — Migrations — **DONE**

The entire migration engine was `ALTER TABLE ADD COLUMN`
(`infra/db/bootstrap/bootstrap.go`). Consequences before this phase:

- **Rename** → the new column is added; the old one keeps the data forever. Silent.
- **Drop** → the column stays forever. A legacy `NOT NULL` column with no default will
  eventually break inserts.
- **Type change** → **not detected at all.** Only column *names* are compared.

There was no escape hatch. The only lever was `bootstrap.NewSQLSeeder`, and four of the five
existing seeders are NULL-backfills compensating for `ADD COLUMN`. Seeders are not
versioned — they re-run on every boot, so each must be hand-written idempotent.

This worked while the schema only grew. It had no answer the first time it didn't, on an
appliance in a customer's building. The cost only rises with the install base.

**Shipped:** a versioned migration table (`schema_migration`) + ordered, checksummed
up-migrations (`infra/db/bootstrap/migration.go`), running **before** (not replacing) the
additive auto-migrate, which stays the default path for additive changes. A fresh database
baselines migrations instead of replaying them; an already-applied migration that is later
edited fails startup loudly (checksum tamper check); MariaDB's lack of transactional DDL is
documented as a "write it idempotently" constraint rather than papered over. A companion
drift detector (`infra/db/bootstrap/drift.go`) reports — never auto-repairs — what auto-migrate
still cannot fix: changed column types and columns the entity no longer declares, comparing
against the type each engine *actually* stores (not the manifest's engine-neutral type) so it
doesn't cry wolf on SQLite/MariaDB's differing boolean storage.

`apphost.Migrator` (`infra/apphost/types.go`) is the optional app interface;
`apps/mymatasan/app/app.go`'s `(*module) Migrations()` implements it and currently returns
`nil` — mymatasan has no pending structural changes, only additive ones, which need no
migration. The factory-reset call site also wires `Migrations: m.Migrations()`, so a reset
correctly baselines the rebuilt database instead of leaving it unable to start on the next
boot.

15 new tests (`migration_test.go`, `drift_test.go`) against a real SQLite database. Live-boot
verified twice against mymatasan: `schema_migration` created, fresh boot baselines (zero
declared migrations), second boot's drift check ran against all 18 real entities with zero
false positives.

**Follow-up (not done in this phase):** four of mymatasan's five seeders are NULL-backfills
compensating for `ADD COLUMN` and re-run on every boot; they are now candidates to become
run-once migrations instead. See `docs/DB_BOOTSTRAP_SPEC.md` for the full mechanism writeup
and the "how to write a migration" section.

---

## Phase R — RBAC

Authorization is one bool. `apis/authorization.go` gives admin everything, and non-admin
all GETs plus a hardcoded six-entry suffix allow-list. **"Can view cameras but not delete
recordings" is not expressible.**

And it collapses at the fleet boundary: myseliasan has a real path/method permission
matrix plus per-node grants, and all of it is projected down to
`IsAdmin: role == "admin"` at `apis/control_dispatch.go:26` when a command crosses into a
node. **A fleet operator's fine-grained role is silently degraded to admin/not-admin at
every node.** That is the real ceiling on the fleet product.

**Deliverable:** adopt the shared `accessrbac` stack in mymatasan (myseliasan already uses
it), and widen the control-channel request's `Role` string into a permission set so the
parent's matrix survives the tunnel.

This one is a product decision as much as a refactor — the role model needs agreeing
before the code.
