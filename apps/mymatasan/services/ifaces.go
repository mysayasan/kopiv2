package services

import (
	"context"
	"net/http"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// SnapshotSource is a resolved camera source used for browser MJPEG output.
type SnapshotSource struct {
	URI      string
	RTSPURI  string
	Username string
	Password string
}

// StreamSelectionRequest selects which ONVIF media profile should be saved as the camera RTSP stream.
type StreamSelectionRequest struct {
	Credentials  onvif.Credentials
	ProfileToken string
	RTSPURL      string
}

// CameraDetail is a flattened view of Camera + CameraOnvif used by the service and API layers.
// Assembled by the camera service from two tables; not a DB entity itself.
type CameraDetail struct {
	entities.Camera
	// ONVIF-specific (zero-value if camera is non-ONVIF)
	XAddr        string `json:"xAddr"`
	Types        string `json:"types"`
	Scopes       string `json:"scopes"`
	HardwareID   string `json:"hardwareId"`
	MediaXAddr   string `json:"mediaXAddr"`
	PTZXAddr     string `json:"ptzXAddr"`
	PTZSupported bool   `json:"ptzSupported"`
	ProfileToken string `json:"profileToken"`
	Username     string `json:"username"`
	HasPassword  bool   `json:"hasPassword"`
	Password     string `json:"-"`
}

// ICameraService manages cameras discovered via any protocol.
type ICameraService interface {
	Discover(ctx context.Context, timeoutMs int64) ([]onvif.Device, error)
	Probe(ctx context.Context, address string) (*onvif.Device, error)
	Get(ctx context.Context, limit uint64, offset uint64) ([]*CameraDetail, uint64, error)
	GetById(ctx context.Context, id uint64) (*CameraDetail, error)
	Save(ctx context.Context, detail CameraDetail) (uint64, error)
	SaveCredentials(ctx context.Context, id uint64, credentials onvif.Credentials) (*CameraDetail, error)
	ChangeCameraPassword(ctx context.Context, id uint64, req ChangeCameraPasswordRequest) (*CameraDetail, error)
	StreamOptions(ctx context.Context, id uint64, credentials onvif.Credentials) (*onvif.StreamOptionsResult, error)
	ResolveStream(ctx context.Context, id uint64, req StreamSelectionRequest) (*CameraDetail, error)
	SetLiveStream(ctx context.Context, id uint64, rtspURL string) (*CameraDetail, error)
	ResolveLiveView(ctx context.Context, id uint64, credentials onvif.Credentials) (*CameraDetail, error)
	PTZMove(ctx context.Context, id uint64, req PTZMoveRequest) (*CameraDetail, error)
	PTZStop(ctx context.Context, id uint64) (*CameraDetail, error)
	SnapshotSource(ctx context.Context, id uint64) (SnapshotSource, error)
	TestStream(ctx context.Context, id uint64) (*rtsp.ProbeResult, error)
	// DisplayName returns the camera's human-readable name (name/model/host),
	// cached and invalidated on save so renames reflect immediately. Returns ""
	// when it cannot be resolved.
	DisplayName(ctx context.Context, id int64) string
	// UpdateHealth persists the live reachability state recorded by the camera
	// health monitor. status is "online"/"offline"; checkedAt is a unix timestamp.
	UpdateHealth(ctx context.Context, id int64, status string, checkedAt int64) error
	Delete(ctx context.Context, id uint64) (uint64, error)
}

type ChangeCameraPasswordRequest struct {
	CurrentUsername string `json:"currentUsername"`
	CurrentPassword string `json:"currentPassword"`
	TargetUsername  string `json:"targetUsername"`
	NewPassword     string `json:"newPassword"`
	UserLevel       string `json:"userLevel"`
}

type PTZMoveRequest struct {
	Direction  string  `json:"direction"`
	Speed      float64 `json:"speed"`
	DurationMs int64   `json:"durationMs"`
}

type DetectionRuleRequest = vision.DetectionRuleRequest

type AlertEventRequest = vision.AlertEventRequest

// VisionMonitorSettings contains runtime-independent startup settings for the background detector worker.
type VisionMonitorSettings struct {
	Enabled                   bool
	Interval                  int64
	CaptureTimeout            int64
	DiagnosticCooldownSeconds int64
	SnapshotDir               string
	Detector                  vision.Detector
	Recorder                  *recording.Manager
	Notifier                  INotificationPublisher
	NotificationDestinations  INotificationDestinationsProvider
	Resolver                  ClassResolver
	// SnapshotCipher (optional) encrypts alert snapshot images at rest. nil = plaintext.
	SnapshotCipher *atrest.Cipher
	// DetectStreamConfig resolves the per-camera RecorderConfig used to run a
	// detection-only frame stream when NVR recording is off (siphon/auto modes).
	// Returns ok=false when the camera has no usable stream. Injected by app wiring
	// so the monitor reuses the exact stream/credential/ffmpeg resolution as the
	// recorder. When nil, the monitor never starts detection-only streams.
	DetectStreamConfig func(ctx context.Context, cameraID int64) (recording.RecorderConfig, bool)
}

// INotificationDestinationsProvider supplies the configured delivery destinations
// so the vision-alert path can render and route a tailored payload per
// destination. Satisfied by INotificationSettingsService.
type INotificationDestinationsProvider interface {
	Destinations(ctx context.Context) []NotificationDestination
}

// RuntimeSettings contains runtime-editable mymatasan settings.
type RuntimeSettings struct {
	Decoder DecoderSettings `json:"decoder"`
	Stream  StreamSettings  `json:"stream"`
	Vision  VisionSettings  `json:"vision"`
}

// VisionSettings holds AI detection tuning parameters that can be changed at runtime.
type VisionSettings struct {
	Yolo              YoloInferenceSettings      `json:"yolo"`
	Capture           CaptureSettings            `json:"capture"`
	AlertNotification *AlertNotificationSettings `json:"alertNotification"`
}

// AlertNotificationSettings controls which fields and media a detection alert
// contributes to the notification payload (webhook, telegram, persisted meta).
// A nil value (legacy settings saved before this existed, or never configured)
// means "include everything" — see defaultAlertNotificationSettings. An explicit
// struct with fields set to false is respected, so the user can trim the payload.
type AlertNotificationSettings struct {
	// IncludeRuleName adds the triggering rule's name (used as the alert title).
	IncludeRuleName bool `json:"includeRuleName"`
	// IncludeLabel adds the detected object label (e.g. "person").
	IncludeLabel bool `json:"includeLabel"`
	// IncludeConfidence adds the detection confidence (and the body percentage).
	IncludeConfidence bool `json:"includeConfidence"`
	// IncludeBoundingBox adds the detection bounding box JSON.
	IncludeBoundingBox bool `json:"includeBoundingBox"`
	// IncludeZonePolygon adds the rule's zone polygon JSON.
	IncludeZonePolygon bool `json:"includeZonePolygon"`
	// IncludeSnapshot attaches the snapshot image (Telegram photo / webhook base64).
	IncludeSnapshot bool `json:"includeSnapshot"`
}

// CaptureSettings controls how the AI detector sources frames per camera.
// Zero values mean "use the built-in default" (same convention as Yolo).
type CaptureSettings struct {
	// Mode selects the frame source: "auto" (siphon when fresh, else standalone),
	// "siphon" (read decoded frames off the recorder), or "standalone" (AI opens
	// its own one-frame RTSP grab). Empty = "auto".
	Mode string `json:"mode"`
	// IntervalMs is the per-camera sampling interval in milliseconds (0 = default).
	IntervalMs int `json:"intervalMs"`
	// FrameWidth is the downscaled frame width in pixels fed to detection (0 = default).
	FrameWidth int `json:"frameWidth"`
	// Standalone holds parameters used only in standalone capture.
	Standalone CaptureStandaloneSettings `json:"standalone"`
	// Siphon holds parameters used only when reading frames off the recorder.
	Siphon CaptureSiphonSettings `json:"siphon"`
}

// CaptureStandaloneSettings tunes the standalone (self-opened RTSP) frame source.
type CaptureStandaloneSettings struct {
	// CaptureTimeoutMs bounds a standalone one-frame RTSP grab (0 = default).
	CaptureTimeoutMs int `json:"captureTimeoutMs"`
}

// CaptureSiphonSettings tunes the siphon (recorder tee) frame source.
type CaptureSiphonSettings struct {
	// Fps is the recorder tee sampling rate in frames per second (0 = default).
	Fps int `json:"fps"`
	// StaleLimitMs is how old a siphoned frame may be (ms) before auto mode falls
	// back to standalone (0 = default).
	StaleLimitMs int `json:"staleLimitMs"`
}

// YoloInferenceSettings holds YOLO inference overrides applied per frame.
// Zero values mean "use the worker's env-var default"; non-zero override that default.
type YoloInferenceSettings struct {
	// Conf is the YOLO detection confidence threshold (0 = use env default MYMATASAN_YOLO_CONF).
	// Lower values detect more objects but increase false positives.
	Conf float64 `json:"conf"`
	// Iou is the NMS intersection-over-union threshold (0 = use YOLO default 0.45).
	// Lower values keep more overlapping boxes — helps when back-facing person boxes overlap.
	Iou float64 `json:"iou"`
	// Augment enables test-time augmentation (flips + scale during inference).
	// Significantly improves detection of back-facing or partially-occluded subjects.
	Augment bool `json:"augment"`
	// Imgsz is the inference image size in pixels (0 = use env default MYMATASAN_YOLO_IMGSZ).
	// Larger values (640, 1280) improve accuracy for small or distant objects at the cost of speed.
	Imgsz int `json:"imgsz"`
	// Half enables FP16 half-precision inference on CUDA GPUs.
	// Faster on GPU but may reduce accuracy slightly.
	Half bool `json:"half"`
	// MaxDet is the maximum detections per image (0 = use YOLO default 300).
	MaxDet int `json:"maxDet"`
}

type DecoderSettings struct {
	MJPEG  MJPEGDecoderSettings  `json:"mjpeg"`
	FFmpeg FFmpegDecoderSettings `json:"ffmpeg"`
}

type MJPEGDecoderSettings struct {
	FFmpegPath string `json:"ffmpegPath"`
	Quality    int    `json:"quality"`
	Threads    int    `json:"threads"`
}

type FFmpegDecoderSettings struct {
	RTSPTransport   string `json:"rtspTransport"`
	HWAccel         string `json:"hwaccel"`
	HWAccelDevice   string `json:"hwaccelDevice"`
	InitHWDevice    string `json:"initHwDevice"`
	VideoDecoder    string `json:"videoDecoder"`
	ProbeSize       int    `json:"probeSize"`
	AnalyzeDuration int    `json:"analyzeDuration"`
	LowDelay        *bool  `json:"lowDelay"`
	NoBuffer        *bool  `json:"noBuffer"`
}

type StreamSettings struct {
	WebRTC        WebRTCSettings        `json:"webrtc"`
	MJPEGFallback MJPEGFallbackSettings `json:"mjpegFallback"`
}

type WebRTCSettings struct {
	Enabled    bool               `json:"enabled"`
	ICEServers []stream.ICEServer `json:"iceServers"`
}

type MJPEGFallbackSettings struct {
	Enabled bool `json:"enabled"`
}

// AuthenticatedUser is the local user identity attached to authenticated requests.
type AuthenticatedUser struct {
	Id                 int64  `json:"id"`
	Username           string `json:"username"`
	DisplayName        string `json:"displayName"`
	IsAdmin            bool   `json:"isAdmin"`
	MustChangePassword bool   `json:"mustChangePassword"`
	SessionHash        string `json:"-"`
}

type CreateLocalUserRequest struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	DisplayName        string `json:"displayName"`
	IsAdmin            bool   `json:"isAdmin"`
	IsActive           bool   `json:"isActive"`
	MustChangePassword bool   `json:"mustChangePassword"`
}

// ChangeLocalUserPasswordRequest is the self-service password change body.
type ChangeLocalUserPasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type UpdateLocalUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	IsAdmin     bool   `json:"isAdmin"`
	IsActive    bool   `json:"isActive"`
}

type ResetLocalUserPasswordRequest struct {
	Password string `json:"password"`
}

// IRuntimeSettingsService manages runtime-editable settings.
type IRuntimeSettingsService interface {
	Get(ctx context.Context) (RuntimeSettings, error)
	Save(ctx context.Context, settings RuntimeSettings) (RuntimeSettings, error)
	Reset(ctx context.Context) (RuntimeSettings, error)
	Stream(ctx context.Context) (StreamSettings, error)
	Decoder(ctx context.Context) (DecoderSettings, error)
}

// ILocalUserService manages standalone mymatasan login users.
type ILocalUserService interface {
	EnsureDefaultAdmin(ctx context.Context) error
	Authenticate(ctx context.Context, username string, password string) (*AuthenticatedUser, error)
	AuthenticateSession(ctx context.Context, username string, sessionHash string) (*AuthenticatedUser, error)
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.LocalUser, uint64, error)
	Create(ctx context.Context, req CreateLocalUserRequest) (*entities.LocalUser, error)
	Update(ctx context.Context, id uint64, req UpdateLocalUserRequest) (*entities.LocalUser, error)
	ResetPassword(ctx context.Context, id uint64, password string) (*entities.LocalUser, error)
	ChangePassword(ctx context.Context, userId int64, currentPassword string, newPassword string) (*AuthenticatedUser, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

// SaveRecordingConfigRequest is the request body for creating or updating a per-camera NVR recording config.
type SaveRecordingConfigRequest struct {
	CameraId          int64  `json:"cameraId"`
	Enabled           bool   `json:"enabled"`
	PreRollSec        int    `json:"preRollSec"`
	PostRollSec       int    `json:"postRollSec"`
	StoragePath       string `json:"storagePath"`
	RetentionDays     int    `json:"retentionDays"`
	SegmentMinutes    int    `json:"segmentMinutes"`
	LiveStreamUrl     string `json:"liveStreamUrl"`
	StreamURL         string `json:"streamUrl"`
	FallbackStreamUrl string `json:"fallbackStreamUrl"`
}

// IRecordingService manages per-camera recording configs and saved video segments.
type IRecordingService interface {
	// GetSegments returns segments filtered by camera, alert, and optional time range.
	// startedAfter / startedBefore are unix timestamps; 0 means no bound.
	GetSegments(ctx context.Context, limit, offset uint64, cameraId, alertId, startedAfter, startedBefore int64) ([]*entities.RecordingSegment, uint64, error)
	GetSegmentById(ctx context.Context, id uint64) (*entities.RecordingSegment, error)
	SaveSegment(ctx context.Context, seg recording.SegmentResult) error
	DeleteSegment(ctx context.Context, id uint64) error
	GetConfig(ctx context.Context, cameraId int64) (*entities.RecordingConfig, error)
	ListConfigs(ctx context.Context) ([]*entities.RecordingConfig, error)
	SaveConfig(ctx context.Context, req SaveRecordingConfigRequest) (*entities.RecordingConfig, error)
	PurgeOldSegments(ctx context.Context) (int, error)
}

// IVisionService manages AI detection rules and alert events.
type IVisionService interface {
	GetRules(ctx context.Context, limit uint64, offset uint64) ([]*entities.DetectionRule, uint64, error)
	SaveRule(ctx context.Context, req DetectionRuleRequest, userId int64) (*entities.DetectionRule, error)
	DeleteRule(ctx context.Context, id uint64) (uint64, error)
	GetAlerts(ctx context.Context, limit uint64, offset uint64, cameraId int64, createdAfter int64, createdBefore int64, ruleId int64, status string, detectionType string) ([]*entities.AlertEvent, uint64, error)
	GetAlertById(ctx context.Context, id uint64) (*entities.AlertEvent, error)
	CreateAlert(ctx context.Context, req AlertEventRequest, userId int64) (*entities.AlertEvent, error)
	AcknowledgeAlert(ctx context.Context, id uint64, userId int64) (*entities.AlertEvent, error)
}

// ITrainingService manages custom-model training datasets and their labeled
// images (Phase 1: collection). Annotation editing, export, and training land in
// later phases.
type ITrainingService interface {
	ListDatasets(ctx context.Context) ([]*entities.TrainingDataset, error)
	GetDataset(ctx context.Context, id uint64) (*entities.TrainingDataset, error)
	SaveDataset(ctx context.Context, req TrainingDatasetRequest, userId int64) (*entities.TrainingDataset, error)
	DeleteDataset(ctx context.Context, id uint64) (uint64, error)
	ListImages(ctx context.Context, datasetId int64) ([]*entities.TrainingImage, error)
	GetImage(ctx context.Context, id uint64) (*entities.TrainingImage, error)
	GetImageBytes(ctx context.Context, id uint64) ([]byte, error)
	StoreUpload(ctx context.Context, datasetId int64, data []byte, userId int64) (*entities.TrainingImage, error)
	AddFromAlert(ctx context.Context, datasetId int64, alertId int64, userId int64) (*entities.TrainingImage, error)
	SaveAnnotations(ctx context.Context, imageId int64, annotations []TrainingAnnotation, userId int64) (*entities.TrainingImage, error)
	AutoLabel(ctx context.Context, imageId int64, userId int64) (*entities.TrainingImage, error)
	DeleteImage(ctx context.Context, id uint64) (uint64, error)
	ExportZip(ctx context.Context, datasetId int64) (string, error)
	ListModels(ctx context.Context) ([]*entities.TrainingModel, error)
	GetModel(ctx context.Context, id uint64) (*entities.TrainingModel, error)
	ImportModel(ctx context.Context, req ImportModelRequest, weights []byte, userId int64) (*entities.TrainingModel, error)
	ActivateModel(ctx context.Context, id uint64, userId int64) (*entities.TrainingModel, error)
	DeactivateModel(ctx context.Context, userId int64) error
	DeleteModel(ctx context.Context, id uint64) (uint64, error)
	MachineCapability(ctx context.Context) MachineCapability
	StartTraining(ctx context.Context, req StartTrainingRequest, userId int64) (*entities.TrainingModel, error)
	StartDepsSetup(ctx context.Context) error
	DepsSetupStatus() DepsSetupState
	GetStockModel(ctx context.Context) StockModelInfo
	SetStockModel(ctx context.Context, model string, userId int64) error
}

// INotificationService is the unified notification feed: it publishes events to
// the hub and exposes persisted history plus the live SSE stream. The domain
// notification.Service satisfies it.
type INotificationService interface {
	INotificationPublisher
	List(ctx context.Context, limit, offset uint64, cameraId int64, unreadOnly bool, category, source string) ([]*sharedentities.Notification, uint64, error)
	MarkRead(ctx context.Context, id uint64, userId int64) (*sharedentities.Notification, error)
	MarkReadByRef(ctx context.Context, refType string, refId int64, userId int64) (int, error)
	Purge(ctx context.Context, olderThan int64, onlyRead bool) (int, error)
	PurgeOlderThanDays(ctx context.Context, days int, onlyRead bool) (int, error)
	Configure(cfg notification.ChannelConfig)
	StreamHandler() http.Handler
	Close(ctx context.Context) error
}

// ICameraHealthProber runs an on-demand reachability probe of every active
// camera and returns fresh per-camera status, used for immediate checks (e.g. at
// login) without waiting for the debounced background sweep. *CameraHealthMonitor
// satisfies it.
type ICameraHealthProber interface {
	ProbeAllNow(ctx context.Context) []CameraHealthSnapshot
}

// IHealthSettingsService manages the runtime-editable camera health monitor
// settings persisted in the database. The monitor reads these live on every
// sweep, so changes made in the UI take effect without a restart.
type IHealthSettingsService interface {
	Get(ctx context.Context) (HealthSettings, error)
	Save(ctx context.Context, settings HealthSettings) (HealthSettings, error)
}

// IMachineHealthSettingsService manages the runtime-editable host (machine)
// health monitor settings (CPU/memory/disk thresholds + disk mitigation)
// persisted in the database. The monitor reads them live on every sample.
type IMachineHealthSettingsService interface {
	Get(ctx context.Context) (MachineHealthSettings, error)
	Save(ctx context.Context, settings MachineHealthSettings) (MachineHealthSettings, error)
}

// IMachineMetricsProvider returns a one-shot snapshot of current host metrics,
// used by the live readout / "Check now" button in the Settings UI.
type IMachineMetricsProvider interface {
	Sample(ctx context.Context) MachineMetrics
}

// INotificationSettingsService manages the runtime-editable notification
// delivery settings (webhook, telegram, retention) persisted in the database.
type INotificationSettingsService interface {
	Get(ctx context.Context) (NotificationSettings, error)
	Save(ctx context.Context, settings NotificationSettings) (NotificationSettings, error)
	// Destinations returns the configured delivery destinations (for
	// per-destination alert rendering). Implements INotificationDestinationsProvider.
	Destinations(ctx context.Context) []NotificationDestination
	// Sync loads persisted settings and applies them to the live notification
	// hub. Called once at startup.
	Sync(ctx context.Context) error
	// Retention returns the current purge retention (days, onlyRead). days <= 0
	// means the periodic purge is disabled.
	Retention(ctx context.Context) (days int, onlyRead bool)
	// Test dispatches a test notification at the given severity so the user can
	// verify their webhook/telegram configuration.
	Test(ctx context.Context, severity string) error
}
