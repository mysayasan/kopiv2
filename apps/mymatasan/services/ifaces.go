package services

import (
	"context"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
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
}

// RuntimeSettings contains runtime-editable mymatasan settings.
type RuntimeSettings struct {
	Decoder DecoderSettings `json:"decoder"`
	Stream  StreamSettings  `json:"stream"`
	Vision  VisionSettings  `json:"vision"`
}

// VisionSettings holds AI detection tuning parameters that can be changed at runtime.
type VisionSettings struct {
	Yolo YoloInferenceSettings `json:"yolo"`
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
	Id          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	IsAdmin     bool   `json:"isAdmin"`
	SessionHash string `json:"-"`
}

type CreateLocalUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
	IsAdmin     bool   `json:"isAdmin"`
	IsActive    bool   `json:"isActive"`
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
	Delete(ctx context.Context, id uint64) (uint64, error)
}

// SaveRecordingConfigRequest is the request body for creating or updating a per-camera NVR recording config.
type SaveRecordingConfigRequest struct {
	CameraId       int64  `json:"cameraId"`
	Enabled        bool   `json:"enabled"`
	PreRollSec     int    `json:"preRollSec"`
	PostRollSec    int    `json:"postRollSec"`
	StoragePath    string `json:"storagePath"`
	RetentionDays  int    `json:"retentionDays"`
	SegmentMinutes int    `json:"segmentMinutes"`
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
	GetAlerts(ctx context.Context, limit uint64, offset uint64, cameraId int64, createdAfter int64, createdBefore int64) ([]*entities.AlertEvent, uint64, error)
	GetAlertById(ctx context.Context, id uint64) (*entities.AlertEvent, error)
	CreateAlert(ctx context.Context, req AlertEventRequest, userId int64) (*entities.AlertEvent, error)
	AcknowledgeAlert(ctx context.Context, id uint64, userId int64) (*entities.AlertEvent, error)
}
