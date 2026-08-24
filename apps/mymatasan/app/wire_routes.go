package app

import (
	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
)

// registerRoutes mounts the HTTP surface: the public routes, the middleware chain, and
// every protected API group.
//
// The middleware ORDER is load-bearing and is the reason this is one function rather than
// scattered registrations:
//
//  1. ResetGate   — sheds load with a clean 503 while a factory reset is running. It runs
//     FIRST, before auth, because the reset closes the database pool and keeps serving
//     through the slow free-space scrub; a DB-backed request (including the login probe)
//     would otherwise 500 until the restart.
//  2. LocalBasicAuth — authenticates and puts the principal in context.
//  3. RequireRolePermission — the role permission matrix. It MUST come after auth, or it
//     has no principal to check and fails closed on everything.
//
// It returns the protected subrouter, because a few API groups (system reset, self-update,
// backup) are built LAST — they need the monitors and recorder to exist so they can quiesce
// them — and must still mount behind the same middleware chain.
func registerRoutes(api *mux.Router, w *wiring) *mux.Router {

	// Public (unauthenticated) routes first — the pairing claim endpoint must be
	// registered before the session catch-all so requests match here rather than being
	// swallowed by auth.
	apis.NewPairingPublicApi(api, w.pairing, w.enrollment.Kick)
	// The built-in manual is public for the same structural reason: it must be readable from
	// the sign-in screen and the first-run wizard, which are precisely where a reader has no
	// session. It serves only shipped, read-only documentation. See apis.NewManualApi.
	apis.NewManualApi(api)

	protected := api.PathPrefix("").Subrouter()
	// w.systemReset is nil at this point — it is built last, because it needs the monitors
	// and the recorder to exist so it can quiesce them before wiping. The gate reads it
	// through a closure at REQUEST time, by which point it is set.
	protected.Use(apis.NewResetGate(func() bool { return w.systemReset != nil && w.systemReset.InProgress() }))
	protected.Use(apis.NewLocalBasicAuth(w.localUser, w.loginGuard, w.loginLockoutNotifier))
	// Every request is decided against the signed-in user's role. Deny-by-default; a
	// superadmin bypasses the matrix. This replaced a single bool plus a suffix-matched
	// allow-list, under which every GET was allowed to everybody.
	protected.Use(apis.NewRequireRolePermission(w.accessRoles, w.accessPerms))

	apis.NewLocalAuthApi(protected, w.localUser)
	apis.NewOnvifApi(protected, w.camera, w.settings, w.streamManager)
	cameraGroup := apis.NewCameraApi(protected, w.camera, w.settings, w.streamManager, w.cameraHealth, w.audit)
	// PTZ presets, home and guard tours (W3-5) hang off the SAME subrouter, under the same
	// /ptz path the role model already grants as "may move a camera".
	apis.NewPTZApi(cameraGroup, w.camera, w.ptz, w.audit)
	// Relay outputs (W3-5b) get their OWN path rather than hanging off /ptz: a control
	// room operator who may point a camera is not automatically somebody who may open a
	// gate, and separate paths are what makes the two grantable apart.
	apis.NewRelayApi(cameraGroup, w.relays)
	apis.NewVisionApi(protected, w.vision, w.detectionClass, w.recorder, w.notification, w.camera, w.settings, w.notificationSettings, w.atrestCipher, w.sightingSearch, w.ptz, w.relays)
	apis.NewTrainingApi(protected, w.training)
	apis.NewTeachApi(protected, w.teach)
	apis.NewFacesApi(protected, w.faceGallery)
	apis.NewSettingsApi(protected, w.settings, w.camera, w.localUser, w.notificationSettings, w.healthSettings, w.machineHealthSettings, w.machineHealth,
		visionToolSettingsFromAppConfig(w.appCfg, w.detectorPaths.DetectorArgs), w.ffmpegInstaller, w.pythonInstaller, w.appCfg.Decoder.BrowseRoots, w.accessRoles, w.audit, w.continuitySettings, w.tamperSettings, w.eventSettings)
	apis.NewRecordingApi(protected, w.recording, w.recorder, w.camera, w.settings, w.atrestCipher, w.vision, w.recorderConfig, w.audit, w.observation)
	apis.NewObservationApi(protected, w.observation, w.sightingSearch, w.appearance)
	apis.NewNotificationApi(protected, w.notification)
	apis.NewAnomalyApi(protected, w.anomalySettings, w.notification, w.camera)
	apis.NewCapacityApi(protected, w.camera, w.settings, w.machineHealth, w.recording, w.objectBackend)
	apis.NewSetupApi(protected, w.setupState)
	// Single-instance by design (local recordings + host-pinned capture/GPU).
	apis.NewDeploymentApi(protected)
	apis.NewPairingApi(protected, w.pairing)
	// The audit trail's READ surface. Writing happens inside the handlers above; this
	// only exposes the trail for review and CSV export, and has no delete or update
	// route by design — see apis/audit.go.
	apis.NewAuditApi(protected, w.auditService)
	// Evidence export: a verifiable bundle of a span of footage. Operator-grantable,
	// separately from deleting — see services/pages.go.
	apis.NewEvidenceApi(protected, w.evidence, w.audit)
	// Case files: bookmark, annotate, assign, close, and export the whole investigation
	// as one bundle. It reads the audit trail as well as writing to it — the case's own
	// entries are the chain of custody the bundle ships.
	apis.NewCaseApi(protected, w.cases, w.evidence, w.auditService, w.localUser, w.audit)
	// Named video walls: what the control room is arranged to look at.
	apis.NewWallApi(protected, w.walls, w.audit)

	return protected
}
