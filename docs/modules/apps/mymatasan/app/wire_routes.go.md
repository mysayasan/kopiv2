# Module: apps/mymatasan/app/wire_routes.go

## Purpose

`registerRoutes` mounts the app's whole HTTP surface: the public (unauthenticated) routes,
the middleware chain, and every protected API group. Moved out of `app.go` (Tier 2 phase
D2) into one function specifically because the middleware ORDER is load-bearing and was
previously just inline statements in the middle of an 800-line function.

## Responsibilities

- `registerRoutes(api *mux.Router, w *wiring) *mux.Router`:
  1. Mounts `apis.NewPairingPublicApi(api, w.pairing, w.enrollment.Kick)` on the **public**
     router first — adopt/release authenticate cryptographically (no local session) and
     must be registered before the session catch-all so requests match here rather than
     being swallowed by auth. `apis.NewManualApi(api)` (`apis/manual.go.md`) is mounted
     right after it, also on the public router and for a structural reason of its own: the
     built-in manual must be readable from the sign-in screen and the first-run wizard,
     both of which have no session either.
  2. Creates `protected := api.PathPrefix("").Subrouter()` and applies middleware in order:
     - `apis.NewResetGate(...)` — sheds load with a clean 503 while a factory reset is
       running. Runs FIRST, before auth, because the reset closes the DB pool and keeps
       serving through the slow free-space scrub; a DB-backed request (including the login
       probe) would otherwise 500 until the restart. Reads `w.systemReset` through a
       closure at request time — it is `nil` at the point `registerRoutes` runs (built
       last, after the monitors/recorder exist) and gets populated later in
       `RegisterAppRoutes`.
     - `apis.NewLocalBasicAuth(w.localUser, w.loginGuard, w.loginLockoutNotifier)` —
       authenticates and puts the principal in context.
     - `apis.NewRequireRolePermission(w.accessRoles, w.accessPerms)` — decides every
       request against the signed-in user's role permission matrix (deny-by-default; a
       superadmin bypasses it). Must come after auth, or it has no principal to check and
       fails closed on everything. Replaced `NewRequireAdminForWrites`, which let every GET
       through to any signed-in user and gated writes with a suffix-matched allow-list — see
       `apis/authorization.go.md`.
  3. Registers every protected API group in order: `NewLocalAuthApi`, `NewOnvifApi`,
     `NewCameraApi` (now also takes `w.audit`), `NewVisionApi` (now also takes
     `w.sightingSearch`, backing `GET /api/vision/alerts/identities` — the identity half of
     federated cross-node search, W2-4/F-10), `NewTrainingApi`,
     `NewTeachApi`, `NewFacesApi`
     (`w.faceGallery` — the face-recognition roster/enrollment surface, see
     `apis/faces.go.md`; admin-only via the same `NewRequireRolePermission` matrix, not a
     separate check), `NewSettingsApi`
     (passing `visionToolSettingsFromAppConfig(w.appCfg, w.detectorPaths.DetectorArgs)`,
     `w.appCfg.Decoder.BrowseRoots` — both off mymatasan's own config since Tier 2 phase C,
     previously `deps.Config` — `w.accessRoles`, which backs `GET /api/settings/roles`, and
     now `w.audit`, `w.continuitySettings`, `w.tamperSettings`),
     `NewRecordingApi` (now also takes `w.audit`), `NewObservationApi` (now also takes
     `w.sightingSearch`, backing `GET /api/observations/search` — the object half of
     federated cross-node search), `NewNotificationApi`,
     `NewAnomalyApi`, `NewCapacityApi`, `NewSetupApi`, `NewDeploymentApi` (deployment mode /
     Phase 1 multi-instance safety — a fixed, read-only `GET /api/deployment/preflight`
     answering `Appliance: true, ApplianceReason: sharedservices.ApplianceLocalMedia`:
     mymatasan owns capture pipelines, writes recordings to local disk, and pins detection to
     this host's GPU, so a second instance would not share that work, it would open its own
     streams against the same cameras; no `POST /api/deployment/mode` route exists — see
     `apis/deployment.go.md`), `NewPairingApi`,
     `NewAuditApi(protected, w.auditService)` — the trail's READ surface; writing happens
     inside the handlers above, and this has no delete or update route by design (see
     `apis/audit.go.md`) — and `NewEvidenceApi(protected, w.evidence, w.audit)` — evidence
     export, operator-grantable separately from deleting (see `services/pages.go.md`).
  4. Returns `protected` — a few API groups (system reset, self-update, backup) are built
     LAST in `RegisterAppRoutes` (they need the monitors and recorder to exist so they can
     quiesce them) and must still mount behind this same middleware chain, so the caller
     needs the subrouter back.

## Notes

- Takes the whole `wiring` struct (`wire_services.go.md`) rather than 20 individual
  parameters — every field it reads was already built by the point `registerRoutes` runs
  in `RegisterAppRoutes`, except `w.systemReset` (read live via closure) and later-built
  API groups (registered separately by the caller against the returned `protected` router).
- Pure move from `app.go`; the middleware order and every API-group registration and
  argument are unchanged.
