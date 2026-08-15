# Module: domain/shared/services/deployment.go

## Purpose

The suite's answer to "can I run this behind a load balancer?" — a question that had no answer
anywhere in the product before this. Two apps (`myseliasan`, `myidsan`) are genuinely stateless
over Postgres and can be replicated; the other three are appliances bound to hardware a second
process cannot share (`mymatasan` owns capture pipelines/local recordings/GPU; `myiotsan`'s Modbus
RTU pollers and `mypintusan`'s `osdp.Bus` each hold a serial port open for their whole lifetime).
Two things live here: `IDeploymentModeService` persists what the operator DECLARED, and
`Preflight` turns the surrounding configuration into an actionable readiness checklist. Everything
`Preflight` needs arrives as plain values in `DeploymentEnv` so this package's dependency surface
stays narrow enough that `infra/apphost` can import it without risking a cycle.

## Deployment mode (declared, not inferred)

- `DeploymentModeKey = "deployment.mode"` — the shared `RuntimeSetting` key, alongside
  `SetupStateKey` (`setup_state.go.md`).
- `ModeStandalone` / `ModeClustered` — the two declarable modes. Standalone is both the default
  and the zero value's meaning: an install that was never asked is single-instance, which is what
  every install was before this existed.
- `DeploymentState{Mode, Acknowledged, UpdatedAt}` — `Acknowledged` records that the operator was
  SHOWN and accepted the known cluster-safety gaps (see "Deployment mode / Phase 1+2 multi-instance
  safety" in `apps/myseliasan/app/app.go.md`), stored rather than inferred so a later build can
  distinguish "agreed to today's caveats" from "never asked." `Clustered()` reports
  `Mode == ModeClustered`.
- `IDeploymentModeService.Get(ctx)` / `.Set(ctx, mode, acknowledged)` — `NewDeploymentModeService(repo)`
  wraps a `dbsql.IGenericRepo[entities.RuntimeSetting]`. `Get` reads the row and, on a missing row,
  an empty value, OR a corrupt/unparseable JSON value, returns `{Mode: ModeStandalone}` rather than
  an error — standalone is the safe direction to fail toward, since it keeps every background
  singleton running on this one instance, which is exactly what a lone process needs. `Set` rejects
  any mode outside the two constants; its insert-vs-update guard treats `row == nil || row.Id == 0`
  as "missing" (the same trap `SetupStateService` guards against — a repo signalling "missing" with
  a zero-value row rather than an error would otherwise silently `UpdateById` on id 0 and persist
  nothing).
- `declaredDeploymentModeTimeout`/`declaredDeploymentMode` in `infra/apphost/shared_state.go`
  read this same row at boot to decide the wording of the shared-state boundary warning — see that
  file's doc.

## Preflight readiness checklist

- `Preflight(ctx, env DeploymentEnv, state DeploymentState) PreflightReport` — never returns an
  error; every check that cannot be proven is reported as a FAILED row instead, because an operator
  reading a checklist needs a verdict per line, not one failure hiding the other six.
  - `env.Appliance` short-circuits to `PreflightReport{Clusterable: false, ApplianceReason: ...}`
    with an empty checklist — there is nothing to be ready FOR.
  - Otherwise runs, in order: `checkDbEngine`, `pingCheck` for `sharedCache`, `pingCheck` for
    `sharedLock`, `checkAtRestKey`, `checkJwtSecret`, `checkDbPool`, then `env.ExtraChecks` (app-specific
    addenda, e.g. myseliasan's LLM-sidecar-memory-multiplication warning). `Ready` is true only when
    no BLOCKER-severity check failed; a failed WARNING (e.g. the per-instance DB pool budget) does
    not clear it, since the deployment still works, just wastefully.
- `PreflightCheck{Id, Severity, Ok, Detail}` — `Detail` carries the OBSERVED VALUE (an engine name,
  a fingerprint, a connection count), never advice; the UI (four languages) owns the wording, and a
  value an operator can compare between two instances is exactly what a translation cannot supply.
  `Id` constants (`CheckDbEngine`, `CheckSharedCache`, `CheckSharedLock`, `CheckAtRestKey`,
  `CheckJwtSecret`, `CheckDbPool`) are exported because the UI maps them to translated labels and
  the manual anchors them — a typo'd id silently renders an untranslated row.
- The six checks:
  - `checkDbEngine` — BLOCKER. `sqlite` can never be clustered (pinned to one connection; the file
    is local to one host) — the one check no configuration can fix, so it is evaluated first.
  - `pingCheck` (used for both cache and lock) — BLOCKER. Combines "is this provider shared at
    all?" (`IsSharedCacheProvider`/`IsDistributedLockProvider`) with "can THIS instance actually
    reach it?" (the supplied `Ping` callback) — the name check alone would pass for an unreachable
    Redis, which is the failure an operator most needs to catch during setup, not at 3am.
  - `checkAtRestKey` — BLOCKER. Passes trivially when encryption is off (nothing sealed to
    disagree about); otherwise reports the key FINGERPRINT (`atrest.KeyStore.Fingerprint()`, not
    the install marker's `KeyId` — see `infra/atrest/cipher.go.md`'s "Fingerprint, not KeyId") for
    an operator to compare by eye between instances, since only a human can complete this check.
  - `checkJwtSecret` — BLOCKER. Tests PROVENANCE, not emptiness: by the time anything can ask, the
    in-memory secret is always populated (the host generates one when unconfigured and writes it
    back — see `infra/apphost/run.go.md`'s `applySensitiveConfig`), so what matters is whether THIS
    instance invented its own. Deliberately reports no fingerprint, unlike the at-rest key — an
    operator-chosen JWT secret only clears a 16-character floor, so publishing even a fingerprint of
    a possibly-weak one would be an offline brute-force oracle for forging tokens.
  - `checkDbPool` — WARNING only. Each instance opens its own connection pool against the same
    database server, so N instances claim N× the budget; the default of 25 is considerate for one
    process and inconsiderate for six.
- `IsSharedCacheProvider(provider)` / `IsDistributedLockProvider(provider)` — case-insensitive
  name checks against `redis`/`rediscluster`/`redis-cluster`. Exported as the single source of
  truth so this checklist and `infra/apphost`'s boot-time shared-state warning
  (`shared_state.go.md`) can never disagree about which provider strings count as shared.
- `Pinger` / `PingFunc(p Pinger) func(context.Context) error` — `Pinger` is anything satisfying
  `Ping(ctx) error` (both `cache.Store` and `coordination.Locker` do, without either package being
  imported here); `PingFunc` adapts one to the callback shape `DeploymentEnv` wants, returning `nil`
  for a `nil` `Pinger` so an app that hasn't wired one reports "unreachable" rather than panicking
  during setup — the moment an operator is least able to diagnose it.
- Appliance reason codes (`ApplianceSerialBus`, `ApplianceLocalMedia`) are CODES, not prose — the
  UI ships in four languages, so the sentence an operator reads comes from the translation
  dictionaries; the server's job is only to say which constraint applies.

## Wiring

`apis.NewDeploymentApi` registered per app (`domain/shared/apis/deployment.go.md`,
`apps/*/apis/deployment.go.md`). Tier A (`myseliasan`, `myidsan`) wire a real
`IDeploymentModeService` and a per-request `DeploymentEnv` closure (rebuilt per request, not
captured, so a Settings-editor change to `cache.provider` is reflected without a restart). Tier B
(`mymatasan`, `myiotsan`, `mypintusan`) pass a `nil` mode service and a fixed
`DeploymentEnv{Appliance: true, ApplianceReason: ...}` — read-only, `GET /deployment/preflight`
only, no `POST /deployment/mode` route at all.

## Notes

- `isNoResultFoundErr` (used by `Get`) is the same not-found classifier `SetupStateService` and
  friends use elsewhere in this package.
- This file has no dependency on `infra/atrest`, `infra/coordination`, or `infra/cache` — every
  fact those packages could supply arrives instead as a plain value or a narrow `func(context.Context) error`
  callback in `DeploymentEnv`, which is what keeps this package safely importable from
  `infra/apphost` (see the file's own header comment).
