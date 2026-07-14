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

## Phase R — RBAC  *(agreed 2026-07-14; part (a), the role model + enforcement in mymatasan, SHIPPED 2026-07-14; part (b), the fleet + frontend half, SHIPPED 2026-07-14)*

### Status

**Part (a), the mymatasan half, is done:** the role model (`viewer`/`operator`/`admin`),
the catalog (`apps/mymatasan/services/rbac.go`'s `Policy()`), `local_user.RoleId` +
`BackfillRoles` (a startup backfill of existing users, not a bootstrap migration — see the
note on `BackfillRoles` for why: it needs both the auto-migrated `role_id` column AND the
seeded role rows to exist first, and phase M migrations run before either), the
`NewRequireRolePermission` middleware replacing `NewRequireAdminForWrites`, and all four
defects below (R5) are shipped. `control_dispatch.go` now resolves the parent's asserted
role NAME against the node's own roles (R4's node-side half) and the wire vocabulary widened
to `{admin, operator, viewer}`. Live-verified (R6) against a running instance with real
viewer/operator accounts — every boundary in the table below confirmed by hand, not just by
the unit tests.

**Part (b), the fleet + frontend half, is now also done:**

1. **myseliasan's `NodeAccessGrant` gained the third level.** `CanOperate` sits between
   `CanRead` and `CanWrite` as an escalation ladder (`CanWrite` implies `CanOperate` implies
   `CanRead`, enforced on save in `services/node_access.go`'s `normalizeAccess`), so a
   control-plane operator now maps onto a node's `operator` role instead of being forced into
   the node's `admin` or `viewer`. `NodeAccess.Role()` resolves the ladder to the role NAME
   sent over the tunnel (`"viewer"` / `"operator"` / `"admin"`), and the central RBAC
   node-access matrix (`RolesAccessPage` in myseliasan's frontend) exposes all three levels.
   Existing grant rows have `canOperate=false` by construction, so an upgraded install grants
   nothing new until an admin explicitly picks the operator level.
2. **The frontend now has a role picker.** mymatasan's Settings → Users replaced the
   `isAdmin` checkbox with a `RoleSelect` sourced from `GET /api/settings/roles`; the
   create-user form and each user card send `roleId`. `isAdmin` still rides along in the
   update payload as the server's legacy fallback for accounts that predate the backfill, but
   the picker is now the primary way to assign `viewer`/`operator`/`admin`.

### The problem (as it stood before this phase)

Authorization was one bool. `apis/authorization.go` gave admin everything, and non-admin
all GETs plus a hardcoded six-entry suffix allow-list. **"Can view cameras but not delete
recordings" was not expressible** — which is the property that makes an NVR evidentiary
rather than just a camera viewer.

And it collapsed at the fleet boundary: myseliasan has a real permission matrix plus
per-node grants, and all of it was projected down to `IsAdmin: role == "admin"` at
`apis/control_dispatch.go:26` when a command crossed into a node. Part (a) below fixed both
the mymatasan side of this and the node's half of the projection; the fleet side (myseliasan's
grant) is the "still open" item above.

### The role model

Three roles. The line is drawn at **"can this person destroy evidence?"**

| Role | Can | Cannot |
|---|---|---|
| **viewer** | live view, see alerts fire | **playback of recorded footage** (that line is the whole point — see below), and anything else except changing their own password |
| **operator** | + playback/download recorded footage, acknowledge alerts, PTZ, talk-back | **delete or purge anything**, edit AI rules, change settings, add/remove cameras |
| **admin** | everything | — |

(Shipped shape, `apps/mymatasan/services/rbac.go`'s `Policy()` — this table was tightened
from the original plan draft, which had given viewer playback too; giving playback to
viewer would have erased the exact line this phase exists to draw.)

Three, not more: every extra role is a support burden and a matrix the customer will
misconfigure. They ship **defined in Go as reviewable data**, not as an empty grid the
installer fills in by clicking. The matrix stays editable for the customer who needs a
fourth.

### Per-camera scoping is deliberately OUT

Nothing in the codebase scopes below the node level. The shared permission row's only key
is a path prefix, so per-camera means either one row per camera per role (with no way to
express "all cameras except") or a new grant table plus enforcement inside every
camera/recording/vision handler rather than in middleware. Build it when a customer with a
shared building asks. Building it speculatively is how you end up maintaining two RBAC
systems forever.

### The tunnel carries a ROLE NAME, not a permission set

An earlier draft of this plan said to widen the control-channel `Role` string into a
permission set so the parent's matrix survives the tunnel. **That is the wrong design.**

If the parent asserts a permission set, the node is trusting the control plane to say who
may delete its footage — a compromised or buggy parent could assert anything. The node owns
the data; the node's policy must govern.

So the wire keeps carrying a role NAME, and the node evaluates its OWN matrix for it. The
shared vocabulary just widens from `{admin, viewer}` to `{admin, operator, viewer}`. The
frames are plain JSON with no strict decoding, so this is backward compatible: an old node
ignores what it does not know. myseliasan's `NodeAccessGrant` gained the matching third level
(`CanOperate`) so a control-plane grant can express the same three rungs instead of only the
binary that produced the problem — see "Status" above.

### Four defects to fix regardless of the role model

1. **The viewer allow-list matches by `strings.HasSuffix`** (`apis/authorization.go:57`).
   Any future route ending in `/read` or `/ack` becomes silently viewer-writable.
2. **`Path: "/"` is an undefended grant-everything wildcard** in the shared matrix, and the
   management API lets an admin create one.
3. **Longest-prefix-wins means permissions SHADOW, they do not union**
   (`domain/shared/services/access_rbac.go:209`). A more specific row with `canPost=false`
   silently overrides a broader row that granted it — not what anyone building a matrix in
   a UI expects.
4. **The permission path catalog is hand-maintained in JavaScript.** Adding a route in Go
   does not make it appear in the matrix, so a new endpoint is ungoverned until someone
   remembers to add a string to a JS array. Derive it from the registered routes.

### The real cost driver

Not the role model — **mymatasan has no JWT at all** (Basic auth + a session cookie), while
the shared RBAC middleware hard-requires JWT claims. A small shim injecting synthetic claims
after the existing local-auth middleware preserves the standalone (no-myidsan) property.

### Steps

- **R1 DONE** — `apps/mymatasan/services/rbac.go`'s `Policy()`: the three built-in roles as
  Go data (catalog, not routes-derived — see "deviations" below).
- **R2 DONE, but not via a migration** — `local_user.RoleId` shipped as an additive column
  (auto-migrated by the normal schema sync, no `bootstrap.Migration` needed for an add-only
  field per phase M's own rule) plus `ILocalUserService.BackfillRoles`, a startup step
  (`app.go`, after `EnsureRoles`, before the admin seed) rather than a bootstrap migration —
  a migration runs before both the auto-migrated column and the seeded role rows exist, so it
  could not have done this assignment anyway.
- **R3 DONE, but not via a shim** — no synthetic-JWT-claims shim was built. mymatasan reuses
  the shared accessrbac **tables + services** directly against its own
  `AuthenticatedUser`/`RoleId`, with its OWN middleware (`NewRequireRolePermission`) calling
  `perms.Authorize` — injecting synthetic JWT claims into a security middleware built to
  require them was judged the kind of shortcut that becomes a CVE, not a shim worth building.
  `RequireAdminForWrites` is replaced; the `settings.requireAdmin` self-gates on
  user/role-management routes are intentionally KEPT (belt-and-suspenders on the one area
  that governs the authorization model itself), and are additionally covered by the outer
  matrix now too (`/api/settings/users`, `/api/settings/roles` are listed no-grant in the
  catalog for viewer/operator).
- **R4 DONE** — the tunnel now carries a role NAME and the node evaluates its own matrix
  (`control_dispatch.go`); the wire vocabulary widened to `{admin, operator, viewer}` with
  `"admin"` kept as an alias for `superadmin`. **myseliasan's `NodeAccessGrant` gained the
  third level** (`CanOperate`, an escalation ladder normalised on save) — see "Status" above.
- **R5 DONE** — all four defects fixed: segment-wise `*`-wildcard path matching (closes the
  suffix-match/prefix-match holes), `Set` refuses a root-path (`/`) permission row,
  `accessMoreSpecific` replaces the raw string-length comparison for which row wins, and the
  Go catalog (R1) is itself the fix for defect 4 (no more hand-maintained JS list) —
  `rolePermissions` renders it into DB rows on boot instead.
- **R6 DONE** — live-verified against a running instance with real viewer/operator accounts:
  every DELETE/purge on recordings, camera delete/mutation, rules/settings/users/roles/
  training/onvif returns 403 for viewer and operator; viewer is 403 on recorded footage and
  ack/PTZ while operator is allowed; admin reaches every handler; unauthenticated is 401.
