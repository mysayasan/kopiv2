package services

import (
	"context"
	"net/http"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/talk"
	"github.com/mysayasan/kopiv2/infra/telemetry"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// LPRCapabilityResult describes whether a camera is suitable for license-plate
// recognition. Supported gates the LPR rule option in the UI; RTSPURL (when set)
// is the highest-resolution ONVIF profile's stream so LPR capture auto-picks it.
type LPRCapabilityResult struct {
	// Supported is true when the camera can plausibly produce plate-legible frames:
	// an ONVIF high-res profile exists, or resolution is unknown (non-ONVIF) so we
	// allow it rather than block. False only when a known max resolution is too low.
	Supported bool `json:"supported"`
	// Onvif is true when profile resolutions were actually read (so Width/Height and
	// the auto-pick RTSPURL are meaningful).
	Onvif bool `json:"onvif"`
	// Width/Height are the highest profile's resolution (0 when unknown).
	Width  int `json:"width"`
	Height int `json:"height"`
	// RTSPURL is the highest-resolution profile's stream, used for auto-pick capture.
	// Empty when not resolvable (non-ONVIF / probe failed) — capture falls back to
	// the camera's existing recording/live stream.
	RTSPURL string `json:"rtspUrl"`
	// Detail is a short human-readable note for the UI (e.g. "highest profile 1920x1080").
	Detail string `json:"detail"`
}

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
	// VerifyDeviceCredentials checks creds against a not-yet-saved camera; CameraAuthStatus
	// verifies a saved camera's stored creds. Both return "ok"|"unauthorized"|"unreachable".
	VerifyDeviceCredentials(ctx context.Context, detail CameraDetail, credentials onvif.Credentials) (string, error)
	CameraAuthStatus(ctx context.Context, id uint64) (string, error)
	ChangeCameraPassword(ctx context.Context, id uint64, req ChangeCameraPasswordRequest) (*CameraDetail, error)
	// ListCameraUsers / CreateCameraUser / DeleteCameraUser manage the camera's local
	// ONVIF user accounts (Device Management GetUsers/CreateUsers/DeleteUsers).
	ListCameraUsers(ctx context.Context, id uint64) ([]onvif.User, error)
	CreateCameraUser(ctx context.Context, id uint64, req CreateCameraUserRequest) error
	DeleteCameraUser(ctx context.Context, id uint64, username string) error
	// Maintenance / config via ONVIF Device Management.
	RebootCamera(ctx context.Context, id uint64) (string, error)
	FactoryDefaultCamera(ctx context.Context, id uint64, hard bool) error
	GetCameraDateTime(ctx context.Context, id uint64) (*onvif.SystemDateTime, error)
	SetCameraDateTime(ctx context.Context, id uint64, req SetCameraDateTimeRequest) error
	GetCameraNetwork(ctx context.Context, id uint64) (*onvif.NetworkConfig, error)
	SetCameraNetwork(ctx context.Context, id uint64, req SetCameraNetworkRequest) error
	GetCameraCapabilities(ctx context.Context, id uint64) (*CameraCapabilities, error)
	GetCameraDeviceInfo(ctx context.Context, id uint64) (*CameraDeviceInfo, error)
	StreamOptions(ctx context.Context, id uint64, credentials onvif.Credentials) (*onvif.StreamOptionsResult, error)
	ResolveStream(ctx context.Context, id uint64, req StreamSelectionRequest) (*CameraDetail, error)
	SetLiveStream(ctx context.Context, id uint64, rtspURL string) (*CameraDetail, error)
	ResolveLiveView(ctx context.Context, id uint64, credentials onvif.Credentials) (*CameraDetail, error)
	PTZMove(ctx context.Context, id uint64, req PTZMoveRequest) (*CameraDetail, error)
	PTZStop(ctx context.Context, id uint64) (*CameraDetail, error)
	GetCameraEncoder(ctx context.Context, id uint64) (*onvif.VideoEncoderConfig, error)
	ApplyCameraEncoder(ctx context.Context, id uint64, req ApplyCameraEncoderRequest) (*onvif.VideoEncoderConfig, error)
	SnapshotSource(ctx context.Context, id uint64) (SnapshotSource, error)
	// PreviewSource resolves an arbitrary detected-profile RTSP URL into a playable
	// source using the camera's stored credentials, WITHOUT persisting anything — so a
	// live preview of a specific stream never changes the camera's active RTSP URL
	// (which recording and detection read via SnapshotSource).
	PreviewSource(ctx context.Context, id uint64, rtspURL string) (SnapshotSource, error)
	// LPRCapability reports whether the camera can supply plate-legible frames and,
	// when ONVIF profiles are readable, the highest-resolution profile's RTSP URL so
	// LPR capture can auto-pick it. Cached; safe to call on the per-frame path.
	LPRCapability(ctx context.Context, id int64) LPRCapabilityResult
	// TalkCapability reports whether a camera supports two-way audio (talk-back)
	// and which transport/password it needs, cached for cheap UI polling.
	TalkCapability(ctx context.Context, id int64) TalkCapabilityResult
	// SaveTalkPassword stores the speaker/cloud password for the TP-Link talk
	// transport (admin-only).
	SaveTalkPassword(ctx context.Context, id uint64, password string) error
	// OpenTalkSession opens a live talk-back audio session to the camera speaker;
	// the caller must Close the returned session.
	OpenTalkSession(ctx context.Context, id uint64) (talk.Session, error)
	TestStream(ctx context.Context, id uint64) (*rtsp.ProbeResult, error)
	// TestStreamURL probes a specific detected-profile RTSP URL with the camera's stored
	// credentials, WITHOUT persisting — a per-stream connectivity check that leaves the
	// camera's saved RTSP URL (and thus recording/detection) untouched.
	TestStreamURL(ctx context.Context, id uint64, rtspURL string) (*rtsp.ProbeResult, error)
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

// CreateCameraUserRequest adds a local ONVIF user account on the camera.
type CreateCameraUserRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	UserLevel string `json:"userLevel"`
}

// SetCameraDateTimeRequest configures the camera clock (ONVIF SetSystemDateAndTime + SetNTP).
type SetCameraDateTimeRequest struct {
	DateTimeType    string   `json:"dateTimeType"` // "Manual" | "NTP"
	DaylightSavings bool     `json:"daylightSavings"`
	TimeZone        string   `json:"timeZone"`
	UTCDateTime     string   `json:"utcDateTime"` // RFC3339 UTC, required for Manual
	NTPFromDHCP     bool     `json:"ntpFromDhcp"`
	NTPServers      []string `json:"ntpServers"`
}

// CameraCapabilities surfaces a camera's firmware/device info + which ONVIF services it
// advertises, so the UI can hide operations the camera doesn't support.
type CameraCapabilities struct {
	Onvif     bool `json:"onvif"`
	PTZ       bool `json:"ptz"`
	Media     bool `json:"media"`
	Imaging   bool `json:"imaging"`
	Analytics bool `json:"analytics"`
	Events    bool `json:"events"`
	// Per-operation support, established by actually probing the read call (so the UI can
	// hide a management box the camera's firmware doesn't implement).
	UserMgmt        bool   `json:"userMgmt"`
	DateTime        bool   `json:"dateTime"`
	Network         bool   `json:"network"`
	Manufacturer    string `json:"manufacturer"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmwareVersion"`
	SerialNumber    string `json:"serialNumber"`
	HardwareID      string `json:"hardwareId"`
}

// CameraDeviceInfo is the read-only identity/inventory of a camera surfaced in the Live
// View → Camera Information panel (like ONVIF Device Manager's device page). The static
// fields come from the stored CameraDetail; MACAddress + ONVIFVersion are pulled live.
type CameraDeviceInfo struct {
	Manufacturer    string `json:"manufacturer"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmwareVersion"`
	HardwareID      string `json:"hardwareId"`
	SerialNumber    string `json:"serialNumber"`
	Location        string `json:"location"`     // parsed from ONVIF scopes (.../location/...)
	MACAddress      string `json:"macAddress"`   // NIC HwAddress (GetNetworkInterfaces)
	ONVIFVersion    string `json:"onvifVersion"` // device service version (GetServices)
	ONVIFUri        string `json:"onvifUri"`     // device service XAddr
}

// SetCameraNetworkRequest configures a camera NIC's IPv4 + gateway + DNS.
type SetCameraNetworkRequest struct {
	InterfaceToken string   `json:"interfaceToken"`
	DHCP           bool     `json:"dhcp"`
	IPAddress      string   `json:"ipAddress"`
	PrefixLength   int      `json:"prefixLength"`
	Gateway        string   `json:"gateway"`
	DNS            []string `json:"dns"`
}

type PTZMoveRequest struct {
	Direction  string  `json:"direction"`
	Speed      float64 `json:"speed"`
	DurationMs int64   `json:"durationMs"`
}

// ApplyCameraEncoderRequest pushes a recording codec + optional bitrate cap to the
// camera encoder via ONVIF (Phase 3 camera-side compression). Encoding is "h264" or
// "h265"; BitrateLimitKbps <= 0 leaves the camera's current bitrate unchanged.
type ApplyCameraEncoderRequest struct {
	Encoding         string `json:"encoding"`
	BitrateLimitKbps int    `json:"bitrateLimitKbps"`
}

type DetectionRuleRequest = vision.DetectionRuleRequest

type AlertEventRequest = vision.AlertEventRequest

// VisionMonitorSettings contains runtime-independent startup settings for the background detector worker.
type VisionMonitorSettings struct {
	Enabled                   bool
	Interval                  int64
	CaptureTimeout            int64
	DiagnosticCooldownSeconds int64
	// PersistSampledDiagnostics writes the noisy "sampled" heartbeat diagnostic to
	// the alert log. Off by default; capture/detect failures are logged regardless.
	PersistSampledDiagnostics bool
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
	// Metadata, when set, records "what objects each camera saw" as presence intervals
	// (the metadata recorder). The monitor samples metadata-enabled cameras even when
	// they have no alert rules, and runs a dedicated observe pass only for frames whose
	// inference wasn't already shared with a rule detection. nil disables it.
	Metadata *MetadataRecorder
	// Metrics records inference latency, frame outcomes and alert counts. Never nil in
	// practice (apphost supplies a no-op recorder when telemetry is off), but the monitor
	// guards anyway so tests can leave it unset.
	Metrics telemetry.Metrics
}

// INotificationDestinationsProvider supplies the configured delivery destinations
// so the vision-alert path can render and route a tailored payload per
// destination. Satisfied by INotificationSettingsService.
type INotificationDestinationsProvider interface {
	Destinations(ctx context.Context) []NotificationDestination
}

// RuntimeSettings contains runtime-editable mymatasan settings.
type RuntimeSettings struct {
	Decoder   DecoderSettings   `json:"decoder"`
	Stream    StreamSettings    `json:"stream"`
	Vision    VisionSettings    `json:"vision"`
	Recording RecordingSettings `json:"recording"`
}

// RecordingSettings holds runtime-editable NVR recording settings. Storage controls
// the at-rest video codec; changes take effect on the next per-camera recorder
// (re)configure or a restart, matching how decoder settings propagate.
type RecordingSettings struct {
	Storage RecordingStorageSettings `json:"storage"`
}

// RecordingStorageSettings controls how recorded segments are stored on disk.
type RecordingStorageSettings struct {
	// Codec is the at-rest video codec: "copy" (default — store the camera's native
	// codec, no re-encode), "h264", or "hevc" (re-encode each segment once at remux
	// on the GPU to shrink it).
	Codec string `json:"codec"`
	// Quality is the NVENC constant-quality (CQ) target when re-encoding (lower =
	// better/larger; ~23-28 typical). 0 = default.
	Quality int `json:"quality"`
	// MaxConcurrentEncodes caps simultaneous NVENC sessions shared by remux-time
	// re-encoding and playback transcode. 0 = default.
	MaxConcurrentEncodes int `json:"maxConcurrentEncodes"`
	// FallbackToCopy stores a segment as plain stream-copy when the GPU re-encode
	// can't run (no usable NVENC encoder, or a runtime failure) instead of dropping
	// it. Pointer so an omitted value defaults to enabled; ignored in copy mode.
	FallbackToCopy *bool `json:"fallbackToCopy"`
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
	// LPR holds parameters for the license-plate capture path (higher resolution).
	LPR CaptureLPRSettings `json:"lpr"`
}

// CaptureLPRSettings tunes the frame used for license-plate recognition. Plates
// need far more pixels than object detection, so LPR-enabled cameras capture a
// dedicated high-resolution standalone frame (the low-res siphon frame would be
// unreadable). This is the "capture high, share down" path.
type CaptureLPRSettings struct {
	// FrameWidth is the downscaled width (px) of the LPR frame (0 = default 1920).
	// The plate crop is OCR'd from this, so higher = more legible distant plates at
	// the cost of more decode/OCR time.
	FrameWidth int `json:"frameWidth"`
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

// IRuntimeSettingsService manages runtime-editable settings.
type IRuntimeSettingsService interface {
	Get(ctx context.Context) (RuntimeSettings, error)
	Save(ctx context.Context, settings RuntimeSettings) (RuntimeSettings, error)
	Reset(ctx context.Context) (RuntimeSettings, error)
	Stream(ctx context.Context) (StreamSettings, error)
	Decoder(ctx context.Context) (DecoderSettings, error)
	Recording(ctx context.Context) (RecordingSettings, error)
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
	// MetadataEnabled toggles the object metadata recorder for this camera;
	// MetadataGapSeconds overrides the presence-interval close window (0 = default).
	MetadataEnabled    bool `json:"metadataEnabled"`
	MetadataGapSeconds int  `json:"metadataGapSeconds"`
}

// IRecordingService manages per-camera recording configs and saved video segments.
type IRecordingService interface {
	// GetSegments returns segments filtered by camera, alert, and optional time range.
	// startedAfter / startedBefore are unix timestamps; 0 means no bound.
	GetSegments(ctx context.Context, limit, offset uint64, cameraId, alertId, startedAfter, startedBefore int64) ([]*entities.RecordingSegment, uint64, error)
	GetSegmentById(ctx context.Context, id uint64) (*entities.RecordingSegment, error)
	// Coverage reports how much of [from, to) actually has footage on disk, bucketed
	// hourly or daily. It is the read model behind the coverage screen and the
	// continuity monitor — see recording_coverage.go.
	Coverage(ctx context.Context, cameraId, from, to int64, bucket string) (CoverageReport, error)
	SaveSegment(ctx context.Context, seg recording.SegmentResult) error
	DeleteSegment(ctx context.Context, id uint64) error
	GetConfig(ctx context.Context, cameraId int64) (*entities.RecordingConfig, error)
	ListConfigs(ctx context.Context) ([]*entities.RecordingConfig, error)
	SaveConfig(ctx context.Context, req SaveRecordingConfigRequest) (*entities.RecordingConfig, error)
	PurgeOldSegments(ctx context.Context) (int, error)
	// PurgeAllForCamera deletes every recorded segment for one camera regardless of
	// expiry (files + rows). Powers the per-camera "Purge now" action.
	PurgeAllForCamera(ctx context.Context, cameraId int64) (int, error)
	// DeleteConfigForCamera removes a camera's recording config. Part of the
	// camera-delete cascade; call it only after that camera's segments are purged,
	// since retention is driven off this row.
	DeleteConfigForCamera(ctx context.Context, cameraId int64) error
	// PurgeOldestSegments deletes the oldest recorded segments regardless of
	// per-camera retention, oldest first, until roughly wantBytes have been freed.
	// Segments that started at or after keepAfter are never touched (safety
	// floor). Returns the segments deleted and the bytes freed. Used by disk
	// mitigation's overwrite-oldest (continuous recording) mode.
	PurgeOldestSegments(ctx context.Context, keepAfter int64, wantBytes int64) (int, int64, error)
}

// IVisionService manages AI detection rules and alert events.
type IVisionService interface {
	GetRules(ctx context.Context, limit uint64, offset uint64) ([]*entities.DetectionRule, uint64, error)
	SaveRule(ctx context.Context, req DetectionRuleRequest, userId int64) (*entities.DetectionRule, error)
	DeleteRule(ctx context.Context, id uint64) (uint64, error)
	// DeleteRulesForCamera removes every rule belonging to one camera. Part of the
	// camera-delete cascade: an orphaned rule keeps the vision monitor sampling a
	// camera that no longer exists.
	DeleteRulesForCamera(ctx context.Context, cameraId int64) (int, error)
	// MarkRuleTriggered persists the moment a rule fired so its cooldown survives a
	// restart. Never moves the stored time backwards.
	MarkRuleTriggered(ctx context.Context, ruleId int64, at int64) error
	GetAlerts(ctx context.Context, limit uint64, offset uint64, cameraId int64, status string, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.AlertEvent, uint64, error)
	GetAlertById(ctx context.Context, id uint64) (*entities.AlertEvent, error)
	CreateAlert(ctx context.Context, req AlertEventRequest, userId int64) (*entities.AlertEvent, error)
	AcknowledgeAlert(ctx context.Context, id uint64, userId int64) (*entities.AlertEvent, error)
	PurgeAlerts(ctx context.Context, olderThan int64, onlyDiagnostics bool) (int, error)
	PurgeAlertsOlderThanDays(ctx context.Context, days int, onlyDiagnostics bool) (int, error)
	// PurgeAlertsForCamera deletes every alert event (+ snapshot files) for one camera
	// regardless of age. Powers the per-camera "Purge now" action.
	PurgeAlertsForCamera(ctx context.Context, cameraId int64) (int, error)
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
	StoreCapture(ctx context.Context, datasetId int64, data []byte, seed []TrainingAnnotation, userId int64) (*entities.TrainingImage, error)
	StoreBackground(ctx context.Context, datasetId int64, data []byte, userId int64) (*entities.TrainingImage, error)
	AddFromAlert(ctx context.Context, datasetId int64, alertId int64, userId int64) (*entities.TrainingImage, error)
	SaveAnnotations(ctx context.Context, imageId int64, annotations []TrainingAnnotation, userId int64) (*entities.TrainingImage, error)
	AutoLabel(ctx context.Context, imageId int64, userId int64) (*entities.TrainingImage, error)
	DeleteImage(ctx context.Context, id uint64) (uint64, error)
	ExportZip(ctx context.Context, datasetId int64) (string, error)
	BuildExport(ctx context.Context, datasetId int64) (string, error)
	ReloadDetector()
	ListModels(ctx context.Context) ([]*entities.TrainingModel, error)
	GetModel(ctx context.Context, id uint64) (*entities.TrainingModel, error)
	ImportModel(ctx context.Context, req ImportModelRequest, weights []byte, userId int64) (*entities.TrainingModel, error)
	ActivateModel(ctx context.Context, id uint64, userId int64) (*entities.TrainingModel, error)
	DeactivateModel(ctx context.Context, userId int64) error
	DeleteModel(ctx context.Context, id uint64) (uint64, error)
	MachineCapability(ctx context.Context) MachineCapability
	StartTraining(ctx context.Context, req StartTrainingRequest, userId int64) (*entities.TrainingModel, error)
	EvaluateHoldout(ctx context.Context, datasetId int64, weightsPath string) ([]EvalPrediction, error)
	StartDepsSetup(ctx context.Context) error
	DepsSetupStatus() DepsSetupState
	GetStockModel(ctx context.Context) StockModelInfo
	SetStockModel(ctx context.Context, model string, userId int64) error
	// License-plate (LPR) model slot — a second-stage plate detector, separate from
	// the stock/custom general-detection models.
	GetLPRModel(ctx context.Context) LPRModelInfo
	SetLPRModel(ctx context.Context, value string, userId int64) error
	ImportLPRModel(ctx context.Context, name string, weights []byte, userId int64) (LPRModelInfo, error)
	DeactivateLPRModel(ctx context.Context, userId int64) error
	// StartLPRDepsSetup installs the OCR dependencies (easyocr) into the app's
	// Python, streaming to the shared installer log (poll via DepsSetupStatus).
	StartLPRDepsSetup(ctx context.Context) error
}

// ITeachService manages Teach-wizard skills: plain-language camera-taught
// detection classes. T1 covers the wizard shell (draft CRUD through the ROI
// step); capture, training, and activation build on it in later phases.
type ITeachService interface {
	ListSkills(ctx context.Context) ([]*entities.TeachSkill, error)
	GetSkill(ctx context.Context, id uint64) (*entities.TeachSkill, error)
	CreateSkill(ctx context.Context, req TeachSkillRequest, userId int64) (*entities.TeachSkill, error)
	UpdateSkill(ctx context.Context, id uint64, req TeachSkillRequest, userId int64) (*entities.TeachSkill, error)
	DeleteSkill(ctx context.Context, id uint64) (uint64, error)
	// Teaching sessions (capture engine): one at a time, presence-gated frames
	// stored into the skill's dataset under the session's class label.
	StartSession(ctx context.Context, skillId uint64, classLabel string, userId int64) (*TeachSessionInfo, error)
	StopSession(ctx context.Context, skillId uint64, userId int64) (*TeachSessionInfo, error)
	SessionInfo(ctx context.Context, skillId uint64) (*TeachSessionInfo, error)
	ActiveSessions(ctx context.Context) []TeachActiveSession
	// Accuracy check: quick-train on the skill's samples, then grade the model
	// on the holdout split and persist a plain-language report.
	StartEvaluation(ctx context.Context, skillId uint64, userId int64) (*TeachEvalState, error)
	EvaluationState(ctx context.Context, skillId uint64) (*TeachEvalState, error)
	// Test drive: a second detector worker on the candidate weights, live
	// annotated frames, no impact on the live pipeline.
	StartTestDrive(ctx context.Context, skillId uint64, userId int64) error
	StopTestDrive(ctx context.Context, skillId uint64)
	TestDriveFrame(ctx context.Context, skillId uint64) (*TeachDriveFrame, error)
	// Activation: hot-swap the checked model + auto-create the detection rule;
	// deactivation undoes both (samples and model stay).
	ActivateSkill(ctx context.Context, skillId uint64, userId int64) (*TeachActivateResult, error)
	DeactivateSkill(ctx context.Context, skillId uint64, userId int64) (*entities.TeachSkill, error)
	// Cross-node sharing: passphrase-encrypted .mmskill export/preview/import.
	ExportSkill(ctx context.Context, skillId uint64, passphrase string, includeImages bool) (string, []byte, error)
	PreviewSkillPackage(ctx context.Context, sealed []byte, passphrase string) (*TeachSkillManifest, error)
	ImportSkill(ctx context.Context, sealed []byte, passphrase string, userId int64) (*entities.TeachSkill, error)
	// Keep-teaching feedback loop: confirm/correct live alerts from an active
	// skill back into its dataset.
	ListSkillFeedback(ctx context.Context, skillId uint64) ([]*TeachFeedbackAlert, error)
	AddSkillFeedback(ctx context.Context, skillId uint64, alertId int64, verdict string, userId int64) error
}

// INotificationService is the unified notification feed: it publishes events to
// the hub and exposes persisted history plus the live SSE stream. The domain
// notification.Service satisfies it.
type INotificationService interface {
	INotificationPublisher
	List(ctx context.Context, limit, offset uint64, cameraId int64, unreadOnly bool, category, source string) ([]*sharedentities.Notification, uint64, error)
	// ListSince returns notifications created at/after `since` (unix seconds), oldest-first,
	// for the fleet control plane to replay events it missed while disconnected.
	ListSince(ctx context.Context, since int64, limit uint64) ([]*sharedentities.Notification, uint64, error)
	Stats(ctx context.Context, from, to, bucketSeconds, tzOffsetSec int64) (*notification.Stats, error)
	Heatmap(ctx context.Context, from, to, cameraId, tzOffsetSec int64) (*notification.Heatmap, error)
	Baseline(ctx context.Context, from, to, bucketSeconds, tzOffsetSec, cameraId int64, source string) (*notification.Baseline, error)
	CameraReliability(ctx context.Context, from, to int64) (*notification.Reliability, error)
	NoisyCameras(ctx context.Context, from, to int64, limit int) (*notification.Noise, error)
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

// IAnomalySettingsService reads and persists the statistical anomaly monitor's
// runtime configuration.
type IAnomalySettingsService interface {
	Get(ctx context.Context) (AnomalySettings, error)
	Save(ctx context.Context, settings AnomalySettings) (AnomalySettings, error)
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
	// SaveDestination upserts a single delivery destination (create when Id is
	// empty, otherwise replace the row with that Id), leaving every other section
	// of the persisted settings untouched. Returns the saved destination (with its
	// assigned Id) and the full updated settings.
	SaveDestination(ctx context.Context, dest NotificationDestination) (NotificationDestination, NotificationSettings, error)
	// DeleteDestination removes the destination with the given Id, leaving the rest
	// of the settings untouched. Returns the full updated settings.
	DeleteDestination(ctx context.Context, id string) (NotificationSettings, error)
	// SaveRetention persists only the retention section, leaving destinations (and
	// legacy singletons) untouched. Returns the full updated settings.
	SaveRetention(ctx context.Context, retention NotificationRetentionSettings) (NotificationSettings, error)
	// SaveSmtp persists only the shared mail-relay section, leaving destinations
	// and retention untouched. A blank password keeps the stored one.
	SaveSmtp(ctx context.Context, smtp NotificationSmtpSettings) (NotificationSettings, error)
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
