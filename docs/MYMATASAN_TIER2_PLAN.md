# MyMataSan — Tier 2 (architecture) Plan

Status: **IN PROGRESS** (started 2026-07-13).

Tier 0 (data-loss correctness, PR #75) and Tier 1 (resilience + metrics, PRs #76/#77) are
done. Tier 2 is the architectural debt — the tier that decides whether [MyIotSan](./MYIOTSAN_PLAN.md)
inherits a clean platform or a copy of the problems.

Agreed sequence: **D → C → M → R.** Decomposing first is cheap and makes the config seam
far easier to cut, because the wiring is already split by subsystem. The config seam is
myiotsan's actual blocker. Migrations are the highest *latent* risk but block nothing
today. RBAC is the biggest product change and goes last.

---

## Phase D — Decompose the composition root

`RegisterAppRoutes` is **792 lines** holding 14 responsibilities, with ordering contracts
enforced by comments rather than types.

### D1 — One `RecorderConfig` builder  ← *first, and it fixes real bugs*

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

## Phase C — Per-app config seam  *(myiotsan's blocker)*

Nine of ~25 blocks in the shared `config.AppConfigModel` are mymatasan-only (`Camera`,
`Decoder`, `Stream`, `Vision`, `Health`, `Recording`, `Notification`, `LoginSecurity`,
`Security`). Worse, the leak runs both ways: `infra/apphost/run.go:687` resolves **YOLO
training directories** — the generic application host has hardcoded knowledge of a vision
feature.

**Deliverable:** `AppConfigModel.App json.RawMessage` + an optional
`apphost.App.DecodeAppConfig(raw)`. mymatasan decodes its own typed config; the
`ResolveWritablePath` calls for vision/recording move into its wiring.

Collapses, as a side effect, the 3× struct triplication (config model ↔ service settings
↔ hand-written mapper) for Decoder, Stream and Recording, and most of the eight
`*FromAppConfig` mappers.

---

## Phase M — Migrations

The entire migration engine is `ALTER TABLE ADD COLUMN`
(`infra/db/bootstrap/bootstrap.go`). Consequences today:

- **Rename** → the new column is added; the old one keeps the data forever. Silent.
- **Drop** → the column stays forever. A legacy `NOT NULL` column with no default will
  eventually break inserts.
- **Type change** → **not detected at all.** Only column *names* are compared.

There is no escape hatch. The only lever is `bootstrap.NewSQLSeeder`, and four of the five
existing seeders are already NULL-backfills compensating for `ADD COLUMN`. Seeders are not
versioned — they re-run on every boot, so each must be hand-written idempotent.

This works while the schema only grows. It has no answer the first time it doesn't, on an
appliance in a customer's building. The cost only rises with the install base.

**Deliverable:** a versioned migration table + ordered up-migrations, running alongside
(not replacing) the additive auto-migrate, which stays the default path.

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
