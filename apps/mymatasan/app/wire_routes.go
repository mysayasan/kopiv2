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
//  3. RequireAdminForWrites — read-only for non-admins. It MUST come after auth, or it has
//     no principal to check and fails closed on everything.
//
// It returns the protected subrouter, because a few API groups (system reset, self-update,
// backup) are built LAST — they need the monitors and recorder to exist so they can quiesce
// them — and must still mount behind the same middleware chain.
func registerRoutes(api *mux.Router, w *wiring) *mux.Router {
	deps := w.deps

	// Public (unauthenticated) routes first — the pairing claim endpoint must be
	// registered before the session catch-all so requests match here rather than being
	// swallowed by auth.
	apis.NewPairingPublicApi(api, w.pairing, w.enrollment.Kick)

	protected := api.PathPrefix("").Subrouter()
	// w.systemReset is nil at this point — it is built last, because it needs the monitors
	// and the recorder to exist so it can quiesce them before wiping. The gate reads it
	// through a closure at REQUEST time, by which point it is set.
	protected.Use(apis.NewResetGate(func() bool { return w.systemReset != nil && w.systemReset.InProgress() }))
	protected.Use(apis.NewLocalBasicAuth(w.localUser, w.loginGuard, w.loginLockoutNotifier))
	protected.Use(apis.NewRequireAdminForWrites())

	apis.NewLocalAuthApi(protected, w.localUser)
	apis.NewOnvifApi(protected, w.camera, w.settings, w.streamManager)
	apis.NewCameraApi(protected, w.camera, w.settings, w.streamManager, w.cameraHealth)
	apis.NewVisionApi(protected, w.vision, w.detectionClass, w.recorder, w.notification, w.camera, w.settings, w.notificationSettings, w.atrestCipher)
	apis.NewTrainingApi(protected, w.training)
	apis.NewTeachApi(protected, w.teach)
	apis.NewSettingsApi(protected, w.settings, w.camera, w.localUser, w.notificationSettings, w.healthSettings, w.machineHealthSettings, w.machineHealth,
		visionToolSettingsFromAppConfig(deps.Config, w.detectorPaths.DetectorArgs), w.ffmpegInstaller, w.pythonInstaller, deps.Config.Decoder.BrowseRoots)
	apis.NewRecordingApi(protected, w.recording, w.recorder, w.camera, w.settings, w.atrestCipher, w.vision, w.recorderConfig)
	apis.NewObservationApi(protected, w.observation)
	apis.NewNotificationApi(protected, w.notification)
	apis.NewAnomalyApi(protected, w.anomalySettings, w.notification, w.camera)
	apis.NewCapacityApi(protected, w.camera, w.settings, w.machineHealth, w.recording, w.objectBackend)
	apis.NewSetupApi(protected, w.setupState)
	apis.NewPairingApi(protected, w.pairing)

	return protected
}
