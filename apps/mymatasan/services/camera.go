package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/rtsp"
)

// cameraProbeTimeout bounds the ONVIF probe portion of credential/live-view
// resolution so an offline or slow camera cannot stack several per-call HTTP
// timeouts (15–30s) and hang the request. The database write that follows uses
// the original request context, so persistence still succeeds when the probe is
// cancelled.
const cameraProbeTimeout = 8 * time.Second

// Credential-verification statuses. "unauthorized" is a definitive login rejection
// (401/NotAuthorized); "unreachable" means we couldn't reach the camera to decide, so it
// must NOT be treated as bad credentials.
const (
	CameraAuthOK           = "ok"
	CameraAuthUnauthorized = "unauthorized"
	CameraAuthUnreachable  = "unreachable"
)

// ErrCameraUnauthorized is returned when a camera actively rejects the supplied
// credentials, so callers (save / credential update) can refuse to persist them.
var ErrCameraUnauthorized = errors.New("camera rejected the credentials")

type onvifClient interface {
	Discover(ctx context.Context, timeout time.Duration) ([]onvif.Device, error)
	Probe(ctx context.Context, address string) (*onvif.Device, error)
	GetCapabilities(ctx context.Context, deviceServiceURL string, credentials onvif.Credentials) (*onvif.CapabilitiesResult, error)
	GetStreamURI(ctx context.Context, req onvif.StreamURIRequest) (*onvif.StreamURIResult, error)
	GetStreamOptions(ctx context.Context, req onvif.StreamURIRequest) (*onvif.StreamOptionsResult, error)
	GetSnapshotURI(ctx context.Context, req onvif.StreamURIRequest) (*onvif.SnapshotURIResult, error)
	ChangeUserPassword(ctx context.Context, req onvif.ChangeUserPasswordRequest) error
	GetUsers(ctx context.Context, req onvif.UsersRequest) ([]onvif.User, error)
	CreateUser(ctx context.Context, req onvif.UserRequest) error
	DeleteUser(ctx context.Context, req onvif.UserRequest) error
	SystemReboot(ctx context.Context, req onvif.DeviceRequest) (string, error)
	SetSystemFactoryDefault(ctx context.Context, req onvif.FactoryDefaultRequest) error
	GetSystemDateAndTime(ctx context.Context, req onvif.DeviceRequest) (*onvif.SystemDateTime, error)
	SetSystemDateAndTime(ctx context.Context, req onvif.SystemDateTimeUpdate) error
	GetNetwork(ctx context.Context, req onvif.DeviceRequest) (*onvif.NetworkConfig, error)
	SetNetwork(ctx context.Context, req onvif.NetworkUpdate) error
	GetNTP(ctx context.Context, req onvif.DeviceRequest) ([]string, bool, error)
	SetNTP(ctx context.Context, req onvif.NTPUpdate) error
	GetServices(ctx context.Context, req onvif.DeviceRequest) ([]onvif.Service, error)
	PTZMove(ctx context.Context, req onvif.PTZMoveRequest) error
	PTZStop(ctx context.Context, req onvif.PTZMoveRequest) error
	GetVideoEncoderConfig(ctx context.Context, req onvif.StreamURIRequest) (*onvif.VideoEncoderConfig, error)
	ConfigureRecording(ctx context.Context, req onvif.ConfigureRecordingRequest) (*onvif.VideoEncoderConfig, error)
}

type cameraService struct {
	cameraRepo dbsql.IGenericRepo[entities.Camera]
	onvifRepo  dbsql.IGenericRepo[entities.CameraOnvif]
	client     onvifClient
	rtspClient rtsp.Client
	nameMu     sync.Mutex
	nameCache  map[int64]cachedCameraName
	lprCapMu   sync.Mutex
	lprCapById map[int64]cachedLPRCapability
}

// cachedCameraName memoizes a camera's resolved display name so callers (e.g. the
// vision monitor, which resolves a name per detection) do not hit the database
// every time. Entries are invalidated on any camera write, so renames reflect
// immediately; the TTL is only a backstop against out-of-band database changes.
type cachedCameraName struct {
	name string
	at   int64 // unix seconds when cached
}

const cameraNameCacheTTL = 30 * time.Minute

// NewCameraService creates a service that manages cameras across all protocols.
func NewCameraService(
	cameraRepo dbsql.IGenericRepo[entities.Camera],
	onvifRepo dbsql.IGenericRepo[entities.CameraOnvif],
	client onvifClient,
	rtspClient rtsp.Client,
) ICameraService {
	return &cameraService{
		cameraRepo: cameraRepo,
		onvifRepo:  onvifRepo,
		client:     client,
		rtspClient: rtspClient,
		nameCache:  map[int64]cachedCameraName{},
		lprCapById: map[int64]cachedLPRCapability{},
	}
}

// minLPRWidth is the source width (px) below which license plates are effectively
// unreadable; a camera whose highest profile is narrower than this is reported as
// not LPR-capable so the UI can hide the option.
const minLPRWidth = 1280

// lprCapabilityCacheTTL bounds how long a resolved capability is reused before the
// ONVIF profiles are re-read. Resolutions rarely change, so this can be generous;
// it keeps the per-frame LPR capture path from making an ONVIF call every sample.
const lprCapabilityCacheTTL = 15 * time.Minute

type cachedLPRCapability struct {
	cap LPRCapabilityResult
	at  int64
}

// LPRCapability reports whether a camera can supply frames legible enough for
// license-plate recognition and, when ONVIF profiles are readable, the RTSP URL of
// its highest-resolution profile (so LPR capture can auto-pick it). The result is
// cached so the per-frame capture path never makes a live ONVIF call. Non-ONVIF
// cameras (no profile data) are reported Supported with Onvif=false and no
// auto-pick URL, so the option stays available but capture uses the existing stream.
func (s *cameraService) LPRCapability(ctx context.Context, id int64) LPRCapabilityResult {
	if id <= 0 {
		return LPRCapabilityResult{}
	}
	now := time.Now().Unix()
	ttl := int64(lprCapabilityCacheTTL.Seconds())
	s.lprCapMu.Lock()
	if cached, ok := s.lprCapById[id]; ok && now-cached.at < ttl {
		s.lprCapMu.Unlock()
		return cached.cap
	}
	s.lprCapMu.Unlock()

	result := s.resolveLPRCapability(ctx, id)
	s.lprCapMu.Lock()
	s.lprCapById[id] = cachedLPRCapability{cap: result, at: now}
	s.lprCapMu.Unlock()
	return result
}

func (s *cameraService) resolveLPRCapability(ctx context.Context, id int64) LPRCapabilityResult {
	detail, err := s.loadDetail(ctx, uint64(id))
	if err != nil || detail == nil {
		return LPRCapabilityResult{Supported: false, Detail: "camera not found"}
	}
	// No ONVIF endpoint → we cannot read resolutions. Keep LPR available but flag
	// that the resolution is unknown and we cannot auto-pick a high-res stream.
	if strings.TrimSpace(detail.XAddr) == "" {
		return LPRCapabilityResult{
			Supported: true,
			Onvif:     false,
			Detail:    "non-ONVIF camera: resolution unknown — ensure its stream is high-resolution",
		}
	}
	credentials := onvif.Credentials{Username: detail.Username, Password: detail.Password}
	res, err := s.client.GetStreamOptions(ctx, onvif.StreamURIRequest{
		DeviceServiceURL: detail.XAddr,
		MediaServiceURL:  detail.MediaXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      credentials,
	})
	if err != nil || res == nil || len(res.Options) == 0 {
		// Probe failed (offline/credentials) — don't block LPR, just can't auto-pick.
		return LPRCapabilityResult{
			Supported: true,
			Onvif:     false,
			Detail:    "could not read camera profiles — using the existing stream",
		}
	}
	best := res.Options[0]
	for _, opt := range res.Options {
		if opt.Width*opt.Height > best.Width*best.Height {
			best = opt
		}
	}
	supported := best.Width >= minLPRWidth
	detailMsg := fmt.Sprintf("highest profile %dx%d", best.Width, best.Height)
	if !supported {
		detailMsg += " — too low for reliable plate reading"
	}
	return LPRCapabilityResult{
		Supported: supported,
		Onvif:     true,
		Width:     best.Width,
		Height:    best.Height,
		RTSPURL:   strings.TrimSpace(best.RTSPURL),
		Detail:    detailMsg,
	}
}

// DisplayName returns the camera's human-readable name, served from a cache that
// is invalidated whenever the camera is written.
func (s *cameraService) DisplayName(ctx context.Context, id int64) string {
	if id <= 0 {
		return ""
	}
	now := time.Now().Unix()
	ttl := int64(cameraNameCacheTTL.Seconds())

	s.nameMu.Lock()
	if cached, ok := s.nameCache[id]; ok && now-cached.at < ttl {
		s.nameMu.Unlock()
		return cached.name
	}
	s.nameMu.Unlock()

	// Only the Camera row is needed (name/model/host); skip the ONVIF lookup.
	cam, err := s.cameraRepo.GetById(ctx, "", uint64(id))
	if err != nil || cam == nil {
		// Don't cache failures; retry on the next call.
		return ""
	}
	name := CameraDisplayName(cam.Name, cam.Model, cam.Host)

	s.nameMu.Lock()
	s.nameCache[id] = cachedCameraName{name: name, at: now}
	s.nameMu.Unlock()
	return name
}

// invalidateName drops a camera's cached display name so the next DisplayName
// call re-reads it from the database.
func (s *cameraService) invalidateName(id int64) {
	if id <= 0 {
		return
	}
	s.nameMu.Lock()
	delete(s.nameCache, id)
	s.nameMu.Unlock()
	// A camera write may change stream profiles/credentials, so drop the cached LPR
	// capability too — it is re-resolved (ONVIF) on the next request.
	s.lprCapMu.Lock()
	delete(s.lprCapById, id)
	s.lprCapMu.Unlock()
}

// — Discovery ----------------------------------------------------------------

func (s *cameraService) Discover(ctx context.Context, timeoutMs int64) ([]onvif.Device, error) {
	return s.client.Discover(ctx, time.Duration(timeoutMs)*time.Millisecond)
}

func (s *cameraService) Probe(ctx context.Context, address string) (*onvif.Device, error) {
	return s.client.Probe(ctx, address)
}

// — Read ---------------------------------------------------------------------

func (s *cameraService) Get(ctx context.Context, limit uint64, offset uint64) ([]*CameraDetail, uint64, error) {
	sorters := []sqldataenums.Sorter{{FieldName: "LastSeenAt", Sort: sqldataenums.DESC}}
	cameras, total, err := s.cameraRepo.Get(ctx, "", limit, offset, nil, sorters)
	if err != nil {
		return nil, 0, err
	}
	details := make([]*CameraDetail, 0, len(cameras))
	for _, cam := range cameras {
		detail := &CameraDetail{Camera: *cam}
		s.attachOnvif(ctx, detail)
		details = append(details, detail)
	}
	return details, total, nil
}

func (s *cameraService) GetById(ctx context.Context, id uint64) (*CameraDetail, error) {
	return s.loadDetail(ctx, id)
}

// — Write --------------------------------------------------------------------

// Save creates or updates a camera. Identity resolution order:
//  1. Id > 0 → update by primary key
//  2. XAddr set → look up via camera_onvif.xaddr (ONVIF cameras)
//  3. Host set → look up by host (non-ONVIF cameras)
//  4. Otherwise → create new record
func (s *cameraService) Save(ctx context.Context, detail CameraDetail) (uint64, error) {
	now := time.Now().UTC().Unix()
	cam := detail.Camera
	cam.IsActive = true
	cam.UpdatedAt = now
	if cam.CreatedAt == 0 {
		cam.CreatedAt = now
	}
	if cam.LastSeenAt == 0 {
		cam.LastSeenAt = now
	}

	// Resolve existing camera record.
	var existingOnvif *entities.CameraOnvif

	if cam.Id == 0 && strings.TrimSpace(detail.XAddr) != "" {
		// ONVIF path: look up by x_addr.
		if ov, err := s.onvifRepo.GetByUnique(ctx, "", "xaddr", detail.XAddr); err == nil && ov != nil {
			existingOnvif = ov
			if existing, err := s.cameraRepo.GetById(ctx, "", uint64(ov.CameraId)); err == nil && existing != nil {
				cam.Id = existing.Id
				cam.CreatedAt = existing.CreatedAt
				cam.CreatedBy = existing.CreatedBy
				preserveCameraFields(&cam, existing)
			}
		}
	} else if cam.Id == 0 && strings.TrimSpace(cam.Host) != "" {
		// Non-ONVIF path: look up by host.
		filters := []sqldataenums.Filter{{FieldName: "Host", Compare: sqldataenums.Equal, Value: cam.Host}}
		if existing, err := s.cameraRepo.GetSingle(ctx, "", filters); err == nil && existing != nil {
			cam.Id = existing.Id
			cam.CreatedAt = existing.CreatedAt
			cam.CreatedBy = existing.CreatedBy
			preserveCameraFields(&cam, existing)
		}
	}

	// Persist camera row.
	if cam.Id > 0 {
		if _, err := s.cameraRepo.UpdateById(ctx, "", cam); err != nil {
			return 0, fmt.Errorf("update camera failed: %w", err)
		}
	} else {
		id, err := s.cameraRepo.Create(ctx, "", cam)
		if err != nil {
			return 0, fmt.Errorf("create camera failed: %w", err)
		}
		cam.Id = int64(id)
	}

	// Persist ONVIF child row if this is an ONVIF camera.
	if strings.TrimSpace(detail.XAddr) != "" {
		ovData := s.buildOnvif(detail, cam.Id)
		if existingOnvif != nil {
			ovData.Id = existingOnvif.Id
			if ovData.Password == "" {
				ovData.Password = existingOnvif.Password
			}
			if ovData.Username == "" {
				ovData.Username = existingOnvif.Username
			}
			_, _ = s.onvifRepo.UpdateById(ctx, "", ovData)
		} else {
			// Check if a record for this camera_id already exists (update path when Id was set).
			if ov, err := s.onvifRepo.GetByUnique(ctx, "", "camera_id", cam.Id); err == nil && ov != nil {
				ovData.Id = ov.Id
				if ovData.Password == "" {
					ovData.Password = ov.Password
				}
				if ovData.Username == "" {
					ovData.Username = ov.Username
				}
				_, _ = s.onvifRepo.UpdateById(ctx, "", ovData)
			} else {
				_, _ = s.onvifRepo.Create(ctx, "", ovData)
			}
		}
	}

	s.invalidateName(cam.Id)
	return uint64(cam.Id), nil
}

func (s *cameraService) SaveCredentials(ctx context.Context, id uint64, credentials onvif.Credentials) (*CameraDetail, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	// Verify before persisting so a wrong password can't overwrite a working one and the
	// node's credential gate rejects bad creds. Only a definitive rejection blocks; an
	// unreachable camera still lets the operator store (possibly-correct) credentials.
	if status, verr := s.verifyCredentials(ctx, *detail, credentials); status == CameraAuthUnauthorized {
		return nil, fmt.Errorf("%w: %v", ErrCameraUnauthorized, verr)
	}
	if strings.TrimSpace(credentials.Username) != "" {
		detail.Username = strings.TrimSpace(credentials.Username)
	}
	if credentials.Password != "" {
		detail.Password = credentials.Password
	}
	// Best-effort: update MediaXAddr/PTZ capabilities. Ignore failure so credentials
	// are always persisted even when GetCapabilities requires auth first (chicken-and-egg).
	// Bounded so an unreachable camera cannot hang the request.
	probeCtx, cancel := context.WithTimeout(ctx, cameraProbeTimeout)
	_ = s.refreshCapabilities(probeCtx, detail, onvif.Credentials{Username: detail.Username, Password: detail.Password})
	cancel()
	if err := s.saveDetail(ctx, detail); err != nil {
		return nil, err
	}
	return detail, nil
}

func (s *cameraService) ChangeCameraPassword(ctx context.Context, id uint64, req ChangeCameraPasswordRequest) (*CameraDetail, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	current := onvif.Credentials{
		Username: firstNonEmpty(req.CurrentUsername, detail.Username),
		Password: firstNonEmpty(req.CurrentPassword, detail.Password),
	}
	targetUsername := firstNonEmpty(req.TargetUsername, current.Username)
	if strings.TrimSpace(targetUsername) == "" {
		return nil, errors.New("targetUsername is required")
	}
	if strings.TrimSpace(req.NewPassword) == "" {
		return nil, errors.New("newPassword is required")
	}
	if err := s.client.ChangeUserPassword(ctx, onvif.ChangeUserPasswordRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      current,
		TargetUsername:   targetUsername,
		NewPassword:      req.NewPassword,
		UserLevel:        req.UserLevel,
	}); err != nil {
		return nil, err
	}
	// Only re-sync the app's stored credential when we changed the very account it logs
	// in with — changing some OTHER user's password must not overwrite our credential.
	if strings.EqualFold(strings.TrimSpace(targetUsername), strings.TrimSpace(current.Username)) {
		detail.Username = targetUsername
		detail.Password = req.NewPassword
		_ = s.refreshCapabilities(ctx, detail, onvif.Credentials{Username: detail.Username, Password: detail.Password})
		if err := s.saveDetail(ctx, detail); err != nil {
			return nil, err
		}
	}
	return detail, nil
}

// cameraOnvifCredentials returns the camera's stored ONVIF admin credentials, used to
// authenticate user-management calls against the device.
func (s *cameraService) cameraOnvifCredentials(detail *CameraDetail) onvif.Credentials {
	return onvif.Credentials{Username: detail.Username, Password: detail.Password}
}

func (s *cameraService) ListCameraUsers(ctx context.Context, id uint64) ([]onvif.User, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(detail.XAddr) == "" {
		return nil, errors.New("camera is not an ONVIF device")
	}
	return s.client.GetUsers(ctx, onvif.UsersRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
	})
}

func (s *cameraService) CreateCameraUser(ctx context.Context, id uint64, req CreateCameraUserRequest) error {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(detail.XAddr) == "" {
		return errors.New("camera is not an ONVIF device")
	}
	return s.client.CreateUser(ctx, onvif.UserRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
		Username:         req.Username,
		Password:         req.Password,
		UserLevel:        req.UserLevel,
	})
}

func (s *cameraService) DeleteCameraUser(ctx context.Context, id uint64, username string) error {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(detail.XAddr) == "" {
		return errors.New("camera is not an ONVIF device")
	}
	if strings.TrimSpace(username) == "" {
		return errors.New("username is required")
	}
	// Guard against removing the account these API calls authenticate with, which would
	// lock the app out of the camera.
	if strings.EqualFold(strings.TrimSpace(username), strings.TrimSpace(detail.Username)) {
		return errors.New("cannot delete the camera's active credential user")
	}
	return s.client.DeleteUser(ctx, onvif.UserRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
		Username:         username,
	})
}

func (s *cameraService) onvifDeviceRequest(detail *CameraDetail) onvif.DeviceRequest {
	return onvif.DeviceRequest{DeviceServiceURL: detail.XAddr, Credentials: s.cameraOnvifCredentials(detail)}
}

func (s *cameraService) requireOnvif(ctx context.Context, id uint64) (*CameraDetail, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(detail.XAddr) == "" {
		return nil, errors.New("camera is not an ONVIF device")
	}
	return detail, nil
}

func (s *cameraService) RebootCamera(ctx context.Context, id uint64) (string, error) {
	detail, err := s.requireOnvif(ctx, id)
	if err != nil {
		return "", err
	}
	return s.client.SystemReboot(ctx, s.onvifDeviceRequest(detail))
}

func (s *cameraService) FactoryDefaultCamera(ctx context.Context, id uint64, hard bool) error {
	detail, err := s.requireOnvif(ctx, id)
	if err != nil {
		return err
	}
	return s.client.SetSystemFactoryDefault(ctx, onvif.FactoryDefaultRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
		Hard:             hard,
	})
}

func (s *cameraService) GetCameraDateTime(ctx context.Context, id uint64) (*onvif.SystemDateTime, error) {
	detail, err := s.requireOnvif(ctx, id)
	if err != nil {
		return nil, err
	}
	res, err := s.client.GetSystemDateAndTime(ctx, s.onvifDeviceRequest(detail))
	if err != nil {
		return nil, err
	}
	// NTP config is a separate ONVIF call; merge it in (best-effort).
	if servers, fromDHCP, ntpErr := s.client.GetNTP(ctx, s.onvifDeviceRequest(detail)); ntpErr == nil {
		res.NTPServers = servers
		res.NTPFromDHCP = fromDHCP
	}
	return res, nil
}

func (s *cameraService) SetCameraDateTime(ctx context.Context, id uint64, req SetCameraDateTimeRequest) error {
	detail, err := s.requireOnvif(ctx, id)
	if err != nil {
		return err
	}
	update := onvif.SystemDateTimeUpdate{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
		DateTimeType:     req.DateTimeType,
		DaylightSavings:  req.DaylightSavings,
		TimeZone:         req.TimeZone,
	}
	if strings.EqualFold(strings.TrimSpace(req.DateTimeType), "Manual") {
		when := time.Now().UTC()
		if strings.TrimSpace(req.UTCDateTime) != "" {
			if parsed, perr := time.Parse(time.RFC3339, strings.TrimSpace(req.UTCDateTime)); perr == nil {
				when = parsed.UTC()
			}
		}
		update.UTC = when
	}
	if err := s.client.SetSystemDateAndTime(ctx, update); err != nil {
		return err
	}
	// In NTP mode, also push the NTP servers (a separate ONVIF call).
	if strings.EqualFold(strings.TrimSpace(req.DateTimeType), "NTP") {
		return s.client.SetNTP(ctx, onvif.NTPUpdate{
			DeviceServiceURL: detail.XAddr,
			Credentials:      s.cameraOnvifCredentials(detail),
			FromDHCP:         req.NTPFromDHCP,
			Servers:          req.NTPServers,
		})
	}
	return nil
}

func (s *cameraService) GetCameraCapabilities(ctx context.Context, id uint64) (*CameraCapabilities, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	caps := &CameraCapabilities{
		Onvif:           strings.TrimSpace(detail.XAddr) != "",
		PTZ:             detail.PTZSupported,
		Manufacturer:    detail.Manufacturer,
		Model:           detail.Model,
		FirmwareVersion: detail.FirmwareVersion,
		SerialNumber:    detail.SerialNumber,
		HardwareID:      detail.HardwareID,
	}
	if !caps.Onvif {
		return caps, nil
	}
	// GetServices tells us which ONVIF services the camera advertises (best-effort).
	if services, svcErr := s.client.GetServices(ctx, s.onvifDeviceRequest(detail)); svcErr == nil {
		for _, svc := range services {
			ns := strings.ToLower(svc.Namespace)
			switch {
			case strings.Contains(ns, "/media"):
				caps.Media = true
			case strings.Contains(ns, "/ptz"):
				caps.PTZ = true
			case strings.Contains(ns, "/imaging"):
				caps.Imaging = true
			case strings.Contains(ns, "/analytics"):
				caps.Analytics = true
			case strings.Contains(ns, "/events"):
				caps.Events = true
			}
		}
	}
	// Device-Management operations are all in the (mandatory) device service, so GetServices
	// can't tell them apart — probe each read call and treat success as "supported" so the
	// UI hides the ones this camera's firmware doesn't actually implement.
	req := s.onvifDeviceRequest(detail)
	if _, e := s.client.GetUsers(ctx, onvif.UsersRequest{DeviceServiceURL: req.DeviceServiceURL, Credentials: req.Credentials}); e == nil {
		caps.UserMgmt = true
	}
	if _, e := s.client.GetSystemDateAndTime(ctx, req); e == nil {
		caps.DateTime = true
	}
	if _, e := s.client.GetNetwork(ctx, req); e == nil {
		caps.Network = true
	}
	return caps, nil
}

// GetCameraDeviceInfo returns the read-only device identity shown in Live View → Camera
// Information. Static fields come from the stored detail; MAC + ONVIF version are pulled
// live (best-effort) so they never block the response if the camera is slow/offline.
func (s *cameraService) GetCameraDeviceInfo(ctx context.Context, id uint64) (*CameraDeviceInfo, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	info := &CameraDeviceInfo{
		Manufacturer:    detail.Manufacturer,
		Model:           detail.Model,
		FirmwareVersion: detail.FirmwareVersion,
		HardwareID:      detail.HardwareID,
		SerialNumber:    detail.SerialNumber,
		Location:        onvifScopeLocation(detail.Scopes),
		ONVIFUri:        strings.TrimSpace(detail.XAddr),
	}
	if info.ONVIFUri == "" {
		return info, nil // non-ONVIF camera: static fields only
	}
	req := s.onvifDeviceRequest(detail)
	// MAC address from the first NIC that reports one (best-effort).
	if cfg, e := s.client.GetNetwork(ctx, req); e == nil && cfg != nil {
		for _, iface := range cfg.Interfaces {
			if mac := strings.TrimSpace(iface.MAC); mac != "" {
				info.MACAddress = mac
				break
			}
		}
	}
	// ONVIF version from the device service entry, falling back to any advertised version.
	if services, e := s.client.GetServices(ctx, req); e == nil {
		fallback := ""
		for _, svc := range services {
			if strings.TrimSpace(svc.Version) == "" {
				continue
			}
			if strings.Contains(strings.ToLower(svc.Namespace), "/device/") {
				info.ONVIFVersion = svc.Version
				break
			}
			if fallback == "" {
				fallback = svc.Version
			}
		}
		if info.ONVIFVersion == "" {
			info.ONVIFVersion = fallback
		}
	}
	return info, nil
}

// onvifScopeLocation extracts a readable location from a camera's ONVIF scopes
// (space-separated onvif:// URIs; the location sits under .../location/<value>).
func onvifScopeLocation(scopes string) string {
	const marker = "/location/"
	for _, scope := range strings.Fields(scopes) {
		if idx := strings.Index(scope, marker); idx >= 0 {
			val := strings.ReplaceAll(strings.Trim(scope[idx+len(marker):], "/"), "/", ", ")
			if dec, err := url.QueryUnescape(val); err == nil {
				return dec
			}
			return val
		}
	}
	return ""
}

func (s *cameraService) GetCameraNetwork(ctx context.Context, id uint64) (*onvif.NetworkConfig, error) {
	detail, err := s.requireOnvif(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.client.GetNetwork(ctx, s.onvifDeviceRequest(detail))
}

func (s *cameraService) SetCameraNetwork(ctx context.Context, id uint64, req SetCameraNetworkRequest) error {
	detail, err := s.requireOnvif(ctx, id)
	if err != nil {
		return err
	}
	return s.client.SetNetwork(ctx, onvif.NetworkUpdate{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
		InterfaceToken:   req.InterfaceToken,
		DHCP:             req.DHCP,
		IPAddress:        req.IPAddress,
		PrefixLength:     req.PrefixLength,
		Gateway:          req.Gateway,
		DNS:              req.DNS,
	})
}

// — Streaming ----------------------------------------------------------------

func (s *cameraService) StreamOptions(ctx context.Context, id uint64, credentials onvif.Credentials) (*onvif.StreamOptionsResult, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(credentials.Username) == "" {
		credentials.Username = detail.Username
	}
	if credentials.Password == "" {
		credentials.Password = detail.Password
	}
	if strings.TrimSpace(detail.XAddr) != "" && credentials.Username == "" && credentials.Password == "" {
		return nil, errors.New("camera credentials required: save username and password in the Credentials panel first")
	}
	_ = s.refreshCapabilities(ctx, detail, credentials)
	res, err := s.client.GetStreamOptions(ctx, onvif.StreamURIRequest{
		DeviceServiceURL: detail.XAddr,
		MediaServiceURL:  detail.MediaXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      credentials,
	})
	if err != nil {
		return nil, err
	}
	detail.Username = credentials.Username
	detail.Password = credentials.Password
	detail.MediaXAddr = firstNonEmpty(res.MediaXAddr, detail.MediaXAddr)
	_ = s.saveDetail(ctx, detail)
	return res, nil
}

func (s *cameraService) ResolveStream(ctx context.Context, id uint64, req StreamSelectionRequest) (*CameraDetail, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	credentials := req.Credentials
	if strings.TrimSpace(credentials.Username) == "" {
		credentials.Username = detail.Username
	}
	if credentials.Password == "" {
		credentials.Password = detail.Password
	}
	_ = s.refreshCapabilities(ctx, detail, credentials)

	profileToken := strings.TrimSpace(req.ProfileToken)
	rtspURL := strings.TrimSpace(req.RTSPURL)
	res, err := s.client.GetStreamURI(ctx, onvif.StreamURIRequest{
		DeviceServiceURL: detail.XAddr,
		MediaServiceURL:  detail.MediaXAddr,
		ProfileToken:     profileToken,
		Credentials:      credentials,
	})
	if err != nil && rtspURL == "" {
		return nil, err
	}
	if err == nil {
		profileToken = firstNonEmpty(res.ProfileToken, profileToken)
		rtspURL = firstNonEmpty(res.RTSPURL, rtspURL)
		detail.MediaXAddr = firstNonEmpty(res.MediaXAddr, detail.MediaXAddr)
	}
	if rtspURL == "" {
		return nil, errors.New("ONVIF stream URI not found")
	}

	now := time.Now().UTC().Unix()
	detail.Username = credentials.Username
	detail.Password = credentials.Password
	detail.ProfileToken = profileToken
	detail.Camera.RTSPUrl = rtspURL
	detail.Camera.RTSPStatus = "resolved"

	if result, successURI, probeErrs, probeErr := s.probeRTSPCandidates(ctx, detail, rtspURL, profileToken); probeErr == nil {
		detail.Camera.RTSPUrl = successURI
		detail.Camera.RTSPStatus = "online"
		detail.Camera.RTSPTransport = result.Transport
		detail.Camera.LastStreamCheckAt = now
		if tracks, marshalErr := json.Marshal(result.Tracks); marshalErr == nil {
			detail.Camera.RTSPTracks = string(tracks)
		}
	} else {
		detail.Camera.RTSPStatus = "offline"
		detail.Camera.LastStreamCheckAt = now
		detail.Camera.RTSPTransport = ""
		detail.Camera.RTSPTracks = ""
		_ = s.saveDetail(ctx, detail)
		if len(probeErrs) > 1 {
			return nil, fmt.Errorf("RTSP probe failed after trying %d candidate URLs: %s", len(probeErrs), strings.Join(probeErrs, "; "))
		}
		return nil, probeErr
	}

	if snapshot, snapshotErr := s.client.GetSnapshotURI(ctx, onvif.StreamURIRequest{
		DeviceServiceURL: detail.XAddr,
		MediaServiceURL:  detail.MediaXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      credentials,
	}); snapshotErr == nil {
		detail.MediaXAddr = firstNonEmpty(snapshot.MediaXAddr, detail.MediaXAddr)
		detail.ProfileToken = firstNonEmpty(snapshot.ProfileToken, detail.ProfileToken)
		detail.Camera.SnapshotURI = snapshot.SnapshotURI
	}
	if err := s.saveDetail(ctx, detail); err != nil {
		return nil, err
	}
	return detail, nil
}

func (s *cameraService) SetLiveStream(ctx context.Context, id uint64, rtspURL string) (*CameraDetail, error) {
	rtspURL = strings.TrimSpace(rtspURL)
	if rtspURL == "" {
		return nil, errors.New("rtspUrl is required")
	}
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	detail.Camera.RTSPUrl = rtspURL
	detail.Camera.RTSPStatus = "resolved"
	if err := s.saveDetail(ctx, detail); err != nil {
		return nil, err
	}
	return detail, nil
}

func (s *cameraService) ResolveLiveView(ctx context.Context, id uint64, credentials onvif.Credentials) (*CameraDetail, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(credentials.Username) == "" {
		credentials.Username = detail.Username
	}
	if credentials.Password == "" {
		credentials.Password = detail.Password
	}
	// Bound the ONVIF probe calls so an unreachable camera cannot stack per-call
	// timeouts and hang the request; the DB write below uses the original context.
	probeCtx, cancelProbe := context.WithTimeout(ctx, cameraProbeTimeout)
	defer cancelProbe()
	_ = s.refreshCapabilities(probeCtx, detail, credentials)

	snapshot, snapshotErr := s.client.GetSnapshotURI(probeCtx, onvif.StreamURIRequest{
		DeviceServiceURL: detail.XAddr,
		MediaServiceURL:  detail.MediaXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      credentials,
	})
	var streamErr error
	if strings.TrimSpace(detail.Camera.RTSPUrl) == "" {
		var stream *onvif.StreamURIResult
		stream, streamErr = s.client.GetStreamURI(probeCtx, onvif.StreamURIRequest{
			DeviceServiceURL: detail.XAddr,
			MediaServiceURL:  detail.MediaXAddr,
			ProfileToken:     detail.ProfileToken,
			Credentials:      credentials,
		})
		if streamErr == nil {
			detail.MediaXAddr = firstNonEmpty(stream.MediaXAddr, detail.MediaXAddr)
			detail.ProfileToken = firstNonEmpty(stream.ProfileToken, detail.ProfileToken)
			detail.Camera.RTSPUrl = stream.RTSPURL
			detail.Camera.RTSPStatus = "resolved"
		}
	}
	if snapshotErr != nil && streamErr != nil && strings.TrimSpace(detail.Camera.RTSPUrl) == "" {
		return nil, fmt.Errorf("resolve live view failed; snapshot: %v; rtsp: %v", snapshotErr, streamErr)
	}
	if snapshotErr != nil && strings.TrimSpace(detail.Camera.RTSPUrl) == "" {
		return nil, snapshotErr
	}

	detail.Username = credentials.Username
	detail.Password = credentials.Password
	if snapshotErr == nil {
		detail.MediaXAddr = firstNonEmpty(snapshot.MediaXAddr, detail.MediaXAddr)
		detail.ProfileToken = firstNonEmpty(snapshot.ProfileToken, detail.ProfileToken)
		detail.Camera.SnapshotURI = snapshot.SnapshotURI
	}
	if err := s.saveDetail(ctx, detail); err != nil {
		return nil, err
	}
	return detail, nil
}

func (s *cameraService) SnapshotSource(ctx context.Context, id uint64) (SnapshotSource, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return SnapshotSource{}, err
	}
	if strings.TrimSpace(detail.Camera.SnapshotURI) == "" && strings.TrimSpace(detail.Camera.RTSPUrl) == "" {
		detail, err = s.ResolveLiveView(ctx, id, onvif.Credentials{})
		if err != nil {
			return SnapshotSource{}, err
		}
		if strings.TrimSpace(detail.Camera.SnapshotURI) == "" && strings.TrimSpace(detail.Camera.RTSPUrl) == "" {
			return SnapshotSource{}, errors.New("snapshotUri or rtspUrl is required; resolve live view first")
		}
	}
	return SnapshotSource{
		URI:      detail.Camera.SnapshotURI,
		RTSPURI:  RTSPURIWithCredentials(detail.Camera.RTSPUrl, detail.Username, detail.Password),
		Username: detail.Username,
		Password: detail.Password,
	}, nil
}

// PreviewSource builds a playable source for an arbitrary detected-profile URL using the
// camera's stored credentials, WITHOUT persisting — the live-preview modal streams this
// under a separate source ID, so the camera's active RTSP URL (recording/detection) is
// never changed.
func (s *cameraService) PreviewSource(ctx context.Context, id uint64, rtspURL string) (SnapshotSource, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return SnapshotSource{}, err
	}
	rtspURL = strings.TrimSpace(rtspURL)
	if rtspURL == "" {
		return SnapshotSource{}, errors.New("rtspUrl is required")
	}
	return SnapshotSource{
		RTSPURI:  RTSPURIWithCredentials(rtspURL, detail.Username, detail.Password),
		Username: detail.Username,
		Password: detail.Password,
	}, nil
}

func (s *cameraService) TestStream(ctx context.Context, id uint64) (*rtsp.ProbeResult, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(detail.Camera.RTSPUrl) == "" {
		return nil, errors.New("rtspUrl is required; resolve stream URI first")
	}
	result, successURI, probeErrs, err := s.probeRTSPCandidates(ctx, detail, detail.Camera.RTSPUrl, detail.ProfileToken)
	now := time.Now().UTC().Unix()
	detail.Camera.LastStreamCheckAt = now
	if err != nil {
		detail.Camera.RTSPStatus = "offline"
		_ = s.saveDetail(ctx, detail)
		if len(probeErrs) > 1 {
			return nil, fmt.Errorf("RTSP probe failed after trying %d candidate URLs: %s", len(probeErrs), strings.Join(probeErrs, "; "))
		}
		return nil, err
	}
	detail.Camera.RTSPStatus = "online"
	if strings.TrimSpace(successURI) != "" {
		detail.Camera.RTSPUrl = successURI
	}
	detail.Camera.RTSPTransport = result.Transport
	if tracks, marshalErr := json.Marshal(result.Tracks); marshalErr == nil {
		detail.Camera.RTSPTracks = string(tracks)
	}
	if err := s.saveDetail(ctx, detail); err != nil {
		return nil, err
	}
	return result, nil
}

// TestStreamURL probes a specific detected-profile RTSP URL with the camera's credentials
// but does NOT persist anything (unlike TestStream, which writes back the resolved URL /
// status / tracks) — so a per-stream test never disturbs the camera's active RTSP URL
// that recording and detection rely on.
func (s *cameraService) TestStreamURL(ctx context.Context, id uint64, rtspURL string) (*rtsp.ProbeResult, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	rtspURL = strings.TrimSpace(rtspURL)
	if rtspURL == "" {
		return nil, errors.New("rtspUrl is required")
	}
	result, _, probeErrs, err := s.probeRTSPCandidates(ctx, detail, rtspURL, detail.ProfileToken)
	if err != nil {
		if len(probeErrs) > 1 {
			return nil, fmt.Errorf("RTSP probe failed after trying %d candidate URLs: %s", len(probeErrs), strings.Join(probeErrs, "; "))
		}
		return nil, err
	}
	return result, nil
}

// — PTZ ----------------------------------------------------------------------

func (s *cameraService) PTZMove(ctx context.Context, id uint64, req PTZMoveRequest) (*CameraDetail, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	pan, tilt, zoom, err := ptzVelocity(req.Direction, req.Speed)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePTZReady(ctx, detail); err != nil {
		return nil, err
	}
	if err := s.client.PTZMove(ctx, onvif.PTZMoveRequest{
		DeviceServiceURL: detail.XAddr,
		PTZServiceURL:    detail.PTZXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      onvif.Credentials{Username: detail.Username, Password: detail.Password},
		Pan:              pan,
		Tilt:             tilt,
		Zoom:             zoom,
		Duration:         time.Duration(req.DurationMs) * time.Millisecond,
	}); err != nil {
		return nil, err
	}
	return detail, nil
}

func (s *cameraService) PTZStop(ctx context.Context, id uint64) (*CameraDetail, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePTZReady(ctx, detail); err != nil {
		return nil, err
	}
	if err := s.client.PTZStop(ctx, onvif.PTZMoveRequest{
		DeviceServiceURL: detail.XAddr,
		PTZServiceURL:    detail.PTZXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      onvif.Credentials{Username: detail.Username, Password: detail.Password},
	}); err != nil {
		return nil, err
	}
	return detail, nil
}

// — Camera-side recording encoder (Phase 3: zero host-cost compression) -------

// GetCameraEncoder reads the camera's current video encoder configuration for its
// recording profile via ONVIF, so the UI can show the live codec/bitrate/resolution.
func (s *cameraService) GetCameraEncoder(ctx context.Context, id uint64) (*onvif.VideoEncoderConfig, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(detail.XAddr) == "" {
		return nil, errors.New("camera is not ONVIF-managed; cannot read encoder settings")
	}
	probeCtx, cancel := context.WithTimeout(ctx, cameraProbeTimeout)
	defer cancel()
	return s.client.GetVideoEncoderConfig(probeCtx, onvif.StreamURIRequest{
		DeviceServiceURL: detail.XAddr,
		MediaServiceURL:  detail.MediaXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      onvif.Credentials{Username: detail.Username, Password: detail.Password},
	})
}

// ApplyCameraEncoder pushes a recording codec (h264/h265) and optional bitrate cap
// to the camera's encoder via ONVIF. This is the zero-host-cost compression lever:
// the camera encodes smaller H.265, the host keeps recording with -c copy. Returns
// the resulting encoder config. Best-effort — cameras that can't be reconfigured
// surface the camera's own error.
func (s *cameraService) ApplyCameraEncoder(ctx context.Context, id uint64, req ApplyCameraEncoderRequest) (*onvif.VideoEncoderConfig, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(detail.XAddr) == "" {
		return nil, errors.New("camera is not ONVIF-managed; cannot change encoder settings")
	}
	probeCtx, cancel := context.WithTimeout(ctx, cameraProbeTimeout)
	defer cancel()
	return s.client.ConfigureRecording(probeCtx, onvif.ConfigureRecordingRequest{
		DeviceServiceURL: detail.XAddr,
		MediaServiceURL:  detail.MediaXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      onvif.Credentials{Username: detail.Username, Password: detail.Password},
		Encoding:         req.Encoding,
		BitrateLimitKbps: req.BitrateLimitKbps,
	})
}

// — Health -------------------------------------------------------------------

// UpdateHealth persists a camera's live reachability state recorded by the
// health monitor. On an "online" status it also advances LastSeenAt. Only the
// camera row is touched (no ONVIF child row), and the name cache is left intact
// since none of these fields feed the display name.
func (s *cameraService) UpdateHealth(ctx context.Context, id int64, status string, checkedAt int64) error {
	if id <= 0 {
		return errors.New("id is required")
	}
	cam, err := s.cameraRepo.GetById(ctx, "", uint64(id))
	if err != nil {
		return err
	}
	if cam == nil {
		return errors.New("camera not found")
	}
	cam.HealthStatus = status
	cam.LastHealthCheckAt = checkedAt
	if status == "online" {
		cam.LastSeenAt = checkedAt
	}
	cam.UpdatedAt = time.Now().UTC().Unix()
	_, err = s.cameraRepo.UpdateById(ctx, "", *cam)
	return err
}

// — Delete -------------------------------------------------------------------

func (s *cameraService) Delete(ctx context.Context, id uint64) (uint64, error) {
	// Remove ONVIF child row first (if any).
	if ov, err := s.onvifRepo.GetByUnique(ctx, "", "camera_id", int64(id)); err == nil && ov != nil {
		_, _ = s.onvifRepo.DeleteById(ctx, "", uint64(ov.Id))
	}
	s.invalidateName(int64(id))
	return s.cameraRepo.DeleteById(ctx, "", id)
}

// — Internal helpers ---------------------------------------------------------

// loadDetail fetches a Camera and its optional CameraOnvif into a CameraDetail.
func (s *cameraService) loadDetail(ctx context.Context, id uint64) (*CameraDetail, error) {
	cam, err := s.cameraRepo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	detail := &CameraDetail{Camera: *cam}
	s.attachOnvif(ctx, detail)
	return detail, nil
}

// attachOnvif enriches a CameraDetail with ONVIF data (no-op if no ONVIF row exists).
func (s *cameraService) attachOnvif(ctx context.Context, detail *CameraDetail) {
	ov, err := s.onvifRepo.GetByUnique(ctx, "", "camera_id", detail.Camera.Id)
	if err != nil || ov == nil {
		return
	}
	detail.XAddr = ov.XAddr
	detail.Types = ov.Types
	detail.Scopes = ov.Scopes
	detail.HardwareID = ov.HardwareID
	detail.MediaXAddr = ov.MediaXAddr
	detail.PTZXAddr = ov.PTZXAddr
	detail.PTZSupported = ov.PTZSupported
	detail.ProfileToken = ov.ProfileToken
	detail.Username = ov.Username
	detail.HasPassword = ov.Password != ""
	detail.Password = ov.Password
}

// saveDetail persists the Camera and (if ONVIF) the CameraOnvif rows from a loaded CameraDetail.
func (s *cameraService) saveDetail(ctx context.Context, detail *CameraDetail) error {
	now := time.Now().UTC().Unix()
	detail.Camera.UpdatedAt = now
	if _, err := s.cameraRepo.UpdateById(ctx, "", detail.Camera); err != nil {
		return err
	}
	s.invalidateName(detail.Camera.Id)
	if strings.TrimSpace(detail.XAddr) == "" {
		return nil
	}
	ovData := s.buildOnvif(*detail, detail.Camera.Id)
	if ov, err := s.onvifRepo.GetByUnique(ctx, "", "camera_id", detail.Camera.Id); err == nil && ov != nil {
		ovData.Id = ov.Id
		_, _ = s.onvifRepo.UpdateById(ctx, "", ovData)
	} else {
		_, _ = s.onvifRepo.Create(ctx, "", ovData)
	}
	return nil
}

// buildOnvif constructs a CameraOnvif entity from a CameraDetail.
func (s *cameraService) buildOnvif(detail CameraDetail, cameraId int64) entities.CameraOnvif {
	return entities.CameraOnvif{
		CameraId:     cameraId,
		XAddr:        detail.XAddr,
		Types:        detail.Types,
		Scopes:       detail.Scopes,
		HardwareID:   detail.HardwareID,
		MediaXAddr:   detail.MediaXAddr,
		PTZXAddr:     detail.PTZXAddr,
		PTZSupported: detail.PTZSupported,
		ProfileToken: detail.ProfileToken,
		Username:     detail.Username,
		Password:     detail.Password,
	}
}

// preserveCameraFields keeps non-empty existing values when incoming fields are blank.
func preserveCameraFields(dst *entities.Camera, src *entities.Camera) {
	if strings.TrimSpace(dst.Description) == "" {
		dst.Description = src.Description
	}
	if strings.TrimSpace(dst.RTSPUrl) == "" {
		dst.RTSPUrl = src.RTSPUrl
	}
	if strings.TrimSpace(dst.SnapshotURI) == "" {
		dst.SnapshotURI = src.SnapshotURI
	}
	if strings.TrimSpace(dst.RTSPStatus) == "" {
		dst.RTSPStatus = src.RTSPStatus
	}
	if strings.TrimSpace(dst.RTSPTransport) == "" {
		dst.RTSPTransport = src.RTSPTransport
	}
	if strings.TrimSpace(dst.RTSPTracks) == "" {
		dst.RTSPTracks = src.RTSPTracks
	}
	if dst.LastStreamCheckAt == 0 {
		dst.LastStreamCheckAt = src.LastStreamCheckAt
	}
}

func (s *cameraService) refreshCapabilities(ctx context.Context, detail *CameraDetail, credentials onvif.Credentials) error {
	if detail == nil || strings.TrimSpace(detail.XAddr) == "" {
		return nil
	}
	capabilities, err := s.client.GetCapabilities(ctx, detail.XAddr, credentials)
	if err != nil {
		return err
	}
	if strings.TrimSpace(capabilities.MediaXAddr) != "" {
		detail.MediaXAddr = capabilities.MediaXAddr
	}
	detail.PTZXAddr = capabilities.PTZXAddr
	detail.PTZSupported = capabilities.PTZSupported
	return nil
}

func (s *cameraService) ensurePTZReady(ctx context.Context, detail *CameraDetail) error {
	if detail == nil {
		return errors.New("device is required")
	}
	credentials := onvif.Credentials{Username: detail.Username, Password: detail.Password}
	if strings.TrimSpace(detail.PTZXAddr) == "" {
		if err := s.refreshCapabilities(ctx, detail, credentials); err != nil {
			return err
		}
		_ = s.saveDetail(ctx, detail)
	}
	if strings.TrimSpace(detail.PTZXAddr) == "" || !detail.PTZSupported {
		return errors.New("camera does not expose ONVIF PTZ service")
	}
	if strings.TrimSpace(detail.ProfileToken) == "" {
		res, err := s.client.GetStreamURI(ctx, onvif.StreamURIRequest{
			DeviceServiceURL: detail.XAddr,
			MediaServiceURL:  detail.MediaXAddr,
			Credentials:      credentials,
		})
		if err != nil {
			return err
		}
		detail.MediaXAddr = res.MediaXAddr
		detail.ProfileToken = res.ProfileToken
		if strings.TrimSpace(detail.Camera.RTSPUrl) == "" {
			detail.Camera.RTSPUrl = res.RTSPURL
		}
		_ = s.saveDetail(ctx, detail)
	}
	if strings.TrimSpace(detail.ProfileToken) == "" {
		return errors.New("ONVIF profile token is required for PTZ")
	}
	return nil
}

func (s *cameraService) probeRTSPCandidates(ctx context.Context, detail *CameraDetail, rtspURL string, profileToken string) (*rtsp.ProbeResult, string, []string, error) {
	var result *rtsp.ProbeResult
	var err error
	var probeErrs []string
	for _, candidate := range rtspProbeCandidates(rtspURL, profileToken) {
		result, err = s.rtspClient.Probe(ctx, RTSPURIWithCredentials(candidate, detail.Username, detail.Password), rtsp.OpenOptions{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
		})
		if err == nil {
			return result, candidate, probeErrs, nil
		}
		probeErrs = append(probeErrs, fmt.Sprintf("%s: %v", candidate, err))
	}
	return nil, "", probeErrs, err
}

// classifyCredError maps a verification failure to an auth status. An explicit login
// rejection is "unauthorized"; anything else (timeout, connection refused, DNS) is
// "unreachable" so a temporarily offline camera never gets mistaken for bad credentials.
// NB: many ONVIF cameras reject a bad WS-Security digest with HTTP 400 + a NotAuthorized
// SOAP fault (not 401), so "status 400" from a reachable camera is treated as an auth
// rejection too — this classifier is only ever called on a verification failure.
func classifyCredError(err error) string {
	if err == nil {
		return CameraAuthOK
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{"400", "401", "403", "unauthorized", "forbidden", "not authorized", "notauthorized", "authentication"} {
		if strings.Contains(msg, marker) {
			return CameraAuthUnauthorized
		}
	}
	return CameraAuthUnreachable
}

// verifyCredentials checks whether creds authenticate against the camera without writing
// to the DB. Returns one of CameraAuth{OK,Unauthorized,Unreachable}. For ONVIF cameras it
// resolves the stream URI (a 401 there is definitive); it then opens the RTSP stream with
// the creds — the DESCRIBE 401 being the only auth signal available for manual cameras.
func (s *cameraService) verifyCredentials(ctx context.Context, detail CameraDetail, creds onvif.Credentials) (string, error) {
	if strings.TrimSpace(creds.Username) == "" {
		creds.Username = detail.Username
	}
	if creds.Password == "" {
		creds.Password = detail.Password
	}
	// probeRTSPCandidates injects detail.Username/Password, so verify against the supplied
	// creds by using them on a local copy.
	detail.Username = creds.Username
	detail.Password = creds.Password

	probeCtx, cancel := context.WithTimeout(ctx, cameraProbeTimeout)
	defer cancel()

	rtspURL := strings.TrimSpace(detail.Camera.RTSPUrl)
	profileToken := detail.ProfileToken

	if strings.TrimSpace(detail.XAddr) != "" {
		stream, err := s.client.GetStreamURI(probeCtx, onvif.StreamURIRequest{
			DeviceServiceURL: detail.XAddr,
			MediaServiceURL:  detail.MediaXAddr,
			ProfileToken:     detail.ProfileToken,
			Credentials:      creds,
		})
		if err != nil {
			// A definitive rejection stops here; an unreachable ONVIF service falls through
			// to the RTSP probe when we already know a stream URL.
			if status := classifyCredError(err); status == CameraAuthUnauthorized {
				return status, err
			}
		} else if stream != nil {
			if u := strings.TrimSpace(stream.RTSPURL); u != "" {
				rtspURL = u
			}
			if pt := strings.TrimSpace(stream.ProfileToken); pt != "" {
				profileToken = pt
			}
		}
	}

	if rtspURL != "" {
		if _, _, _, err := s.probeRTSPCandidates(probeCtx, &detail, rtspURL, profileToken); err != nil {
			return classifyCredError(err), err
		}
		return CameraAuthOK, nil
	}

	// No stream URL to probe: if the ONVIF resolve above succeeded we accept it; otherwise
	// we simply couldn't reach the camera to decide.
	if strings.TrimSpace(detail.XAddr) != "" {
		return CameraAuthOK, nil
	}
	return CameraAuthUnreachable, nil
}

// VerifyDeviceCredentials verifies creds against a not-yet-saved discovered camera (the
// add flow, where there is no DB id yet).
func (s *cameraService) VerifyDeviceCredentials(ctx context.Context, detail CameraDetail, creds onvif.Credentials) (string, error) {
	return s.verifyCredentials(ctx, detail, creds)
}

// CameraAuthStatus verifies a saved camera's STORED credentials — the signal the camera
// node's access gate uses to decide whether to prompt for new credentials.
func (s *cameraService) CameraAuthStatus(ctx context.Context, id uint64) (string, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return "", err
	}
	// The probe error is only a diagnostic — classifying the credentials as "unauthorized"
	// (or "unreachable") is a successful determination, so it must NOT surface as an API
	// error (that would make the handler return 400 and the gate never appear).
	status, _ := s.verifyCredentials(ctx, *detail, onvif.Credentials{Username: detail.Username, Password: detail.Password})
	return status, nil
}

// RTSPURIWithCredentials injects username:password into an RTSP URI.
func RTSPURIWithCredentials(rawURI string, username string, password string) string {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" || strings.TrimSpace(username) == "" {
		return rawURI
	}
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.User != nil {
		return rawURI
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String()
}

func rtspProbeCandidates(rawURI string, profileToken string) []string {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" {
		return nil
	}
	candidates := []string{rawURI}
	parsed, err := url.Parse(rawURI)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return candidates
	}
	streamNumber := rtspStreamNumber(rawURI, profileToken)
	for _, number := range rtspCandidateStreamNumbers(streamNumber) {
		next := *parsed
		next.Path = "/stream" + number
		next.RawPath = ""
		next.RawQuery = ""
		next.ForceQuery = false
		candidates = appendUnique(candidates, next.String())
	}
	return candidates
}

func rtspCandidateStreamNumbers(preferred string) []string {
	switch strings.TrimSpace(preferred) {
	case "1":
		return []string{"1", "2"}
	case "2":
		return []string{"2", "1"}
	default:
		return []string{"1", "2"}
	}
}

func rtspStreamNumber(rawURI string, profileToken string) string {
	value := strings.ToLower(strings.Join([]string{rawURI, profileToken}, " "))
	switch {
	case strings.Contains(value, "stream2"), strings.Contains(value, "sub"), strings.Contains(value, "minor"), strings.Contains(value, "secondary"), strings.Contains(value, "channel2"):
		return "2"
	case strings.Contains(value, "stream1"), strings.Contains(value, "main"), strings.Contains(value, "major"), strings.Contains(value, "primary"), strings.Contains(value, "channel1"):
		return "1"
	default:
		return ""
	}
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), value) {
			return values
		}
	}
	return append(values, value)
}

func ptzVelocity(direction string, speed float64) (float64, float64, float64, error) {
	direction = strings.ToLower(strings.TrimSpace(direction))
	if speed <= 0 {
		speed = 0.35
	}
	if speed > 1 {
		speed = 1
	}
	switch direction {
	case "left":
		return -speed, 0, 0, nil
	case "right":
		return speed, 0, 0, nil
	case "up":
		return 0, speed, 0, nil
	case "down":
		return 0, -speed, 0, nil
	case "up-left", "left-up":
		return -speed, speed, 0, nil
	case "up-right", "right-up":
		return speed, speed, 0, nil
	case "down-left", "left-down":
		return -speed, -speed, 0, nil
	case "down-right", "right-down":
		return speed, -speed, 0, nil
	case "zoom-in", "in":
		return 0, 0, speed, nil
	case "zoom-out", "out":
		return 0, 0, -speed, nil
	default:
		return 0, 0, 0, fmt.Errorf("unsupported PTZ direction %q", direction)
	}
}

func isNoResultFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no result found")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// CameraFromDevice converts a discovered onvif.Device into a Camera entity.
func CameraFromDevice(device onvif.Device, name string, description string) entities.Camera {
	return entities.Camera{
		Name:            name,
		Description:     description,
		Host:            device.Host,
		Port:            device.Port,
		Manufacturer:    device.Manufacturer,
		Model:           device.Model,
		FirmwareVersion: device.FirmwareVersion,
		SerialNumber:    device.SerialNumber,
		RTSPUrl:         device.RTSPURL,
		SnapshotURI:     device.SnapshotURI,
		LastSeenAt:      device.LastSeenAt,
		IsActive:        true,
	}
}
