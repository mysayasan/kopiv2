package services

import (
	"context"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/onvif"
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

// IOnvifDeviceService discovers and persists standalone ONVIF devices.
type IOnvifDeviceService interface {
	Discover(ctx context.Context, timeoutMs int64) ([]onvif.Device, error)
	Probe(ctx context.Context, address string) (*onvif.Device, error)
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.OnvifDevice, uint64, error)
	Save(ctx context.Context, model entities.OnvifDevice) (uint64, error)
	SaveDiscovered(ctx context.Context, device onvif.Device) (uint64, error)
	SaveCredentials(ctx context.Context, id uint64, credentials onvif.Credentials) (*entities.OnvifDevice, error)
	ChangeCameraPassword(ctx context.Context, id uint64, req ChangeCameraPasswordRequest) (*entities.OnvifDevice, error)
	ResolveStream(ctx context.Context, id uint64, credentials onvif.Credentials) (*entities.OnvifDevice, error)
	ResolveLiveView(ctx context.Context, id uint64, credentials onvif.Credentials) (*entities.OnvifDevice, error)
	PTZMove(ctx context.Context, id uint64, req PTZMoveRequest) (*entities.OnvifDevice, error)
	PTZStop(ctx context.Context, id uint64) (*entities.OnvifDevice, error)
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

// RuntimeSettings contains runtime-editable mymatasan settings.
type RuntimeSettings struct {
	Decoder DecoderSettings `json:"decoder"`
	Stream  StreamSettings  `json:"stream"`
}

type DecoderSettings struct {
	MJPEG MJPEGDecoderSettings `json:"mjpeg"`
}

type MJPEGDecoderSettings struct {
	FFmpegPath string `json:"ffmpegPath"`
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

// IVisionService manages AI detection rules and alert events.
type IVisionService interface {
	GetRules(ctx context.Context, limit uint64, offset uint64) ([]*entities.DetectionRule, uint64, error)
	SaveRule(ctx context.Context, req DetectionRuleRequest, userId int64) (*entities.DetectionRule, error)
	DeleteRule(ctx context.Context, id uint64) (uint64, error)
	GetAlerts(ctx context.Context, limit uint64, offset uint64) ([]*entities.AlertEvent, uint64, error)
	CreateAlert(ctx context.Context, req AlertEventRequest, userId int64) (*entities.AlertEvent, error)
	AcknowledgeAlert(ctx context.Context, id uint64, userId int64) (*entities.AlertEvent, error)
}
