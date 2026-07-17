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
     being swallowed by auth.
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
     `NewCameraApi`, `NewVisionApi`, `NewTrainingApi`, `NewTeachApi`, `NewFacesApi`
     (`w.faceGallery` — the face-recognition roster/enrollment surface, see
     `apis/faces.go.md`; admin-only via the same `NewRequireRolePermission` matrix, not a
     separate check), `NewSettingsApi`
     (passing `visionToolSettingsFromAppConfig(w.appCfg, w.detectorPaths.DetectorArgs)`,
     `w.appCfg.Decoder.BrowseRoots` — both off mymatasan's own config since Tier 2 phase C,
     previously `deps.Config` — and `w.accessRoles`, which backs `GET /api/settings/roles`),
     `NewRecordingApi`, `NewObservationApi`, `NewNotificationApi`, `NewAnomalyApi`,
     `NewCapacityApi`, `NewSetupApi`, `NewPairingApi`.
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
