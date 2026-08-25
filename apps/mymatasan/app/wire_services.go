package app

import (
	"fmt"
	"strings"

	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	mmconfig "github.com/mysayasan/kopiv2/apps/mymatasan/config"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/notification"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/apphost"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// wiring is everything the composition root has constructed: the services, the managers,
// and the resolved values they were built from.
//
// It exists so the two big remaining phases — registering routes and starting background
// workers — can be functions with ONE parameter instead of thirty, without going back to
// reading thirty free variables out of an 800-line scope. The struct is the seam: if a
// phase needs something, it has to be in here, and that is visible.
//
// It is deliberately a plain bag of already-built things, not a service locator. Nothing
// resolves anything from it at runtime; the composition root fills it once, in order, and
// the phases read it.
type wiring struct {
	deps apphost.Dependencies
	// appCfg is mymatasan's own config (camera/vision/recording/...), decoded from the same
	// config.json the host parsed. See apps/mymatasan/config.
	appCfg *mmconfig.Config

	// Resolved values.
	atrestKeyStore *atrest.KeyStore
	atrestCipher   *atrest.Cipher
	detectorPaths  detectorModelPaths
	objectBackend  vision.ObjectDetector
	httpsPort      int

	// Domain services.
	camera         services.ICameraService
	vision         services.IVisionService
	detectionClass services.IDetectionClassService
	training       services.ITrainingService
	teach          services.ITeachService
	faceGallery    *services.FaceGalleryService
	recording      services.IRecordingService
	observation    *services.ObservationService
	// sightingSearch answers the control plane's federated fleet search over this node.
	sightingSearch *services.SightingSearch
	// appearance ranks recorded sightings by how much they look alike (W3-2).
	appearance *services.AppearanceService
	metadata   *services.MetadataRecorder
	localUser  services.ILocalUserService
	setupState sharedservices.ISetupStateService
	pairing    services.IPairingService

	// Settings services.
	settings              services.IRuntimeSettingsService
	notificationSettings  services.INotificationSettingsService
	healthSettings        services.IHealthSettingsService
	machineHealthSettings services.IMachineHealthSettingsService
	anomalySettings       services.IAnomalySettingsService

	// Managers and monitors.
	notification          *notification.Service
	notificationRollup    *notification.RollupMaintainer
	recorder              *recording.Manager
	recorderConfig        *services.RecorderConfigBuilder
	streamManager         *stream.Manager
	cameraHealth          *services.CameraHealthMonitor
	machineHealth         *services.MachineHealthMonitor
	visionMonitorSettings services.VisionMonitorSettings

	// Fleet.
	enrollment *services.EnrollmentManager
	control    *services.ControlChannelManager
	media      *services.MediaChannelManager

	// Auth + authorization. accessRoles/accessPerms are the shared RBAC services: mymatasan
	// uses the shared role + permission MODEL, with its own middleware over its own
	// Basic-auth principal (the shared middleware hard-requires a JWT it does not have).
	loginGuard           *apis.LoginGuard
	loginLockoutNotifier services.INotificationPublisher
	accessRoles          sharedservices.IAccessRoleService
	accessPerms          sharedservices.IAccessPermissionService

	// audit is the append-only evidence-handling trail. Held as the API-layer Auditor
	// rather than the bare service because every audited handler also needs the
	// trusted-proxy list to resolve a caller's real address.
	audit        *apis.Auditor
	auditService services.IAuditService

	// continuitySettings backs the recording-continuity monitor, which answers "was there
	// actually footage" rather than "is the camera reachable".
	continuitySettings services.IContinuitySettingsService

	// evidence builds verifiable export bundles of recorded footage.
	evidence services.IEvidenceExportService
	// cases is the investigation container, and the authority on what footage is held.
	cases services.ICaseService
	// walls are the named video-wall arrangements Live View renders.
	walls services.IWallService
	// ptz owns guard tours and alarm recall — the two things that make a PTZ camera an
	// unattended device rather than one that needs somebody holding a button.
	ptz services.IPTZService
	// ptzJournal is the one place that knows a camera's view changed because WE changed
	// it. Held here because the tamper monitor reads it and the camera service writes it.
	ptzJournal *services.PTZJournal
	// relays is the CHOKEPOINT for everything that switches a camera's output — an
	// operator's button, a detection rule, anything added later. It is the only thing in
	// this appliance that acts on the world, so it audits and rate-limits in one place.
	relays services.IRelayService
	// privacy owns the regions of a camera's view that must not be seen (W3-6), and the
	// question of whether the CAMERA is actually masking them or only the exports are.
	privacy services.IPrivacyService
	// eventSettings backs the camera event listener (W3-5b): what the CAMERA noticed,
	// including whatever is wired into its terminal block.
	eventSettings services.IOnvifEventSettingsService
	// standby is the N+1 failover surface (W3-7): the camera sets this appliance holds on
	// behalf of others, and the one path by which it takes them over. It is the only thing
	// here that can create cameras nobody on this appliance ever added.
	standby services.IStandbyService

	// tamperSettings backs the camera tamper monitor — the third health question, after
	// "does it answer" and "is it recording": is it still showing its scene?
	tamperSettings services.ITamperSettingsService

	// Installers.
	ffmpegInstaller *services.FFmpegInstaller
	pythonInstaller *services.PythonInstaller

	// systemReset is built LAST — it needs the monitors and recorder to exist so it can
	// quiesce them before wiping. The reset gate middleware is registered before it exists
	// and reads it through a closure, so this field is nil until late in wiring. See the
	// comment at its construction site.
	systemReset *services.SystemResetService
}

// validate fails fast if a field was forgotten when the struct was populated.
//
// This is the one real cost of gathering the wiring into a bag: a missing field is a nil
// dereference deep inside a phase at runtime, not a compile error. That is not theoretical
// — the config seam shipped with appCfg unset and panicked in registerRoutes. Startup is
// the right place to find that, with the field's NAME, rather than a stack trace.
//
// systemReset is deliberately excluded: it is legitimately nil here and set later.
func (w *wiring) validate() error {
	missing := []string{}
	check := func(name string, ok bool) {
		if !ok {
			missing = append(missing, name)
		}
	}

	check("appCfg", w.appCfg != nil)
	check("camera", w.camera != nil)
	check("vision", w.vision != nil)
	check("detectionClass", w.detectionClass != nil)
	check("training", w.training != nil)
	check("teach", w.teach != nil)
	check("faceGallery", w.faceGallery != nil)
	check("recording", w.recording != nil)
	check("observation", w.observation != nil)
	check("sightingSearch", w.sightingSearch != nil)
	check("appearance", w.appearance != nil)
	check("metadata", w.metadata != nil)
	check("localUser", w.localUser != nil)
	check("setupState", w.setupState != nil)
	check("pairing", w.pairing != nil)
	check("settings", w.settings != nil)
	check("notificationSettings", w.notificationSettings != nil)
	check("healthSettings", w.healthSettings != nil)
	check("machineHealthSettings", w.machineHealthSettings != nil)
	check("anomalySettings", w.anomalySettings != nil)
	check("notification", w.notification != nil)
	check("notificationRollup", w.notificationRollup != nil)
	check("recorder", w.recorder != nil)
	check("recorderConfig", w.recorderConfig != nil)
	check("streamManager", w.streamManager != nil)
	check("cameraHealth", w.cameraHealth != nil)
	check("machineHealth", w.machineHealth != nil)
	check("enrollment", w.enrollment != nil)
	check("control", w.control != nil)
	check("media", w.media != nil)
	check("loginGuard", w.loginGuard != nil)
	check("accessRoles", w.accessRoles != nil)
	check("audit", w.audit != nil)
	check("continuitySettings", w.continuitySettings != nil)
	check("evidence", w.evidence != nil)
	check("cases", w.cases != nil)
	check("walls", w.walls != nil)
	check("ptz", w.ptz != nil)
	check("ptzJournal", w.ptzJournal != nil)
	check("relays", w.relays != nil)
	check("privacy", w.privacy != nil)
	check("eventSettings", w.eventSettings != nil)
	check("standby", w.standby != nil)
	check("tamperSettings", w.tamperSettings != nil)
	check("accessPerms", w.accessPerms != nil)
	check("ffmpegInstaller", w.ffmpegInstaller != nil)
	check("pythonInstaller", w.pythonInstaller != nil)

	if len(missing) > 0 {
		return fmt.Errorf("app wiring incomplete: %s", strings.Join(missing, ", "))
	}
	return nil
}
