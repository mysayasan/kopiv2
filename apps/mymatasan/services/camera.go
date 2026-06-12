package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/rtsp"
)

type onvifClient interface {
	Discover(ctx context.Context, timeout time.Duration) ([]onvif.Device, error)
	Probe(ctx context.Context, address string) (*onvif.Device, error)
	GetCapabilities(ctx context.Context, deviceServiceURL string, credentials onvif.Credentials) (*onvif.CapabilitiesResult, error)
	GetStreamURI(ctx context.Context, req onvif.StreamURIRequest) (*onvif.StreamURIResult, error)
	GetStreamOptions(ctx context.Context, req onvif.StreamURIRequest) (*onvif.StreamOptionsResult, error)
	GetSnapshotURI(ctx context.Context, req onvif.StreamURIRequest) (*onvif.SnapshotURIResult, error)
	ChangeUserPassword(ctx context.Context, req onvif.ChangeUserPasswordRequest) error
	PTZMove(ctx context.Context, req onvif.PTZMoveRequest) error
	PTZStop(ctx context.Context, req onvif.PTZMoveRequest) error
}

type cameraService struct {
	cameraRepo dbsql.IGenericRepo[entities.Camera]
	onvifRepo  dbsql.IGenericRepo[entities.CameraOnvif]
	client     onvifClient
	rtspClient rtsp.Client
}

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
	}
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

	return uint64(cam.Id), nil
}

func (s *cameraService) SaveCredentials(ctx context.Context, id uint64, credentials onvif.Credentials) (*CameraDetail, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(credentials.Username) != "" {
		detail.Username = strings.TrimSpace(credentials.Username)
	}
	if credentials.Password != "" {
		detail.Password = credentials.Password
	}
	if err := s.refreshCapabilities(ctx, detail, onvif.Credentials{Username: detail.Username, Password: detail.Password}); err != nil {
		if strings.TrimSpace(detail.MediaXAddr) == "" {
			return nil, err
		}
	}
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
	detail.Username = targetUsername
	detail.Password = req.NewPassword
	_ = s.refreshCapabilities(ctx, detail, onvif.Credentials{Username: detail.Username, Password: detail.Password})
	if err := s.saveDetail(ctx, detail); err != nil {
		return nil, err
	}
	return detail, nil
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
	_ = s.refreshCapabilities(ctx, detail, credentials)

	snapshot, snapshotErr := s.client.GetSnapshotURI(ctx, onvif.StreamURIRequest{
		DeviceServiceURL: detail.XAddr,
		MediaServiceURL:  detail.MediaXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      credentials,
	})
	var streamErr error
	if strings.TrimSpace(detail.Camera.RTSPUrl) == "" {
		var stream *onvif.StreamURIResult
		stream, streamErr = s.client.GetStreamURI(ctx, onvif.StreamURIRequest{
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

// — Delete -------------------------------------------------------------------

func (s *cameraService) Delete(ctx context.Context, id uint64) (uint64, error) {
	// Remove ONVIF child row first (if any).
	if ov, err := s.onvifRepo.GetByUnique(ctx, "", "camera_id", int64(id)); err == nil && ov != nil {
		_, _ = s.onvifRepo.DeleteById(ctx, "", uint64(ov.Id))
	}
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
