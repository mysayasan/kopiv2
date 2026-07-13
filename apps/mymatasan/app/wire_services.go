package app

import (
	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/notification"
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
	recording      services.IRecordingService
	observation    *services.ObservationService
	metadata       *services.MetadataRecorder
	localUser      services.ILocalUserService
	setupState     services.ISetupStateService
	pairing        services.IPairingService

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

	// Auth.
	loginGuard           *apis.LoginGuard
	loginLockoutNotifier services.INotificationPublisher

	// Installers.
	ffmpegInstaller *services.FFmpegInstaller
	pythonInstaller *services.PythonInstaller

	// systemReset is built LAST — it needs the monitors and recorder to exist so it can
	// quiesce them before wiping. The reset gate middleware is registered before it exists
	// and reads it through a closure, so this field is nil until late in wiring. See the
	// comment at its construction site.
	systemReset *services.SystemResetService
}
