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
	GetSnapshotURI(ctx context.Context, req onvif.StreamURIRequest) (*onvif.SnapshotURIResult, error)
	ChangeUserPassword(ctx context.Context, req onvif.ChangeUserPasswordRequest) error
	PTZMove(ctx context.Context, req onvif.PTZMoveRequest) error
	PTZStop(ctx context.Context, req onvif.PTZMoveRequest) error
}

type onvifDeviceService struct {
	repo       dbsql.IGenericRepo[entities.OnvifDevice]
	client     onvifClient
	rtspClient rtsp.Client
}

// NewOnvifDeviceService creates a service for ONVIF discovery and saved devices.
func NewOnvifDeviceService(repo dbsql.IGenericRepo[entities.OnvifDevice], client onvifClient, rtspClient rtsp.Client) IOnvifDeviceService {
	return &onvifDeviceService{repo: repo, client: client, rtspClient: rtspClient}
}

func (s *onvifDeviceService) Discover(ctx context.Context, timeoutMs int64) ([]onvif.Device, error) {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	return s.client.Discover(ctx, timeout)
}

func (s *onvifDeviceService) Probe(ctx context.Context, address string) (*onvif.Device, error) {
	return s.client.Probe(ctx, address)
}

func (s *onvifDeviceService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.OnvifDevice, uint64, error) {
	sorters := []sqldataenums.Sorter{{FieldName: "LastSeenAt", Sort: sqldataenums.DESC}}
	return s.repo.Get(ctx, "", limit, offset, nil, sorters)
}

func (s *onvifDeviceService) Save(ctx context.Context, model entities.OnvifDevice) (uint64, error) {
	if strings.TrimSpace(model.XAddr) == "" {
		return 0, errors.New("xAddr is required")
	}
	if strings.TrimSpace(model.Host) == "" {
		return 0, errors.New("host is required")
	}
	now := time.Now().UTC().Unix()
	if model.LastSeenAt == 0 {
		model.LastSeenAt = now
	}
	if model.CreatedAt == 0 {
		model.CreatedAt = now
	}
	model.UpdatedAt = now
	model.IsActive = true

	existing, err := s.repo.GetByUnique(ctx, "", "xaddr", model.XAddr)
	if err == nil && existing != nil {
		model.Id = existing.Id
		model.CreatedAt = existing.CreatedAt
		model.CreatedBy = existing.CreatedBy
		if strings.TrimSpace(model.Description) == "" {
			model.Description = existing.Description
		}
		if strings.TrimSpace(model.Password) == "" {
			model.Password = existing.Password
		}
		if strings.TrimSpace(model.Username) == "" {
			model.Username = existing.Username
		}
		if strings.TrimSpace(model.MediaXAddr) == "" {
			model.MediaXAddr = existing.MediaXAddr
		}
		if strings.TrimSpace(model.PTZXAddr) == "" {
			model.PTZXAddr = existing.PTZXAddr
		}
		if !model.PTZSupported {
			model.PTZSupported = existing.PTZSupported
		}
		if strings.TrimSpace(model.ProfileToken) == "" {
			model.ProfileToken = existing.ProfileToken
		}
		if strings.TrimSpace(model.RTSPUrl) == "" {
			model.RTSPUrl = existing.RTSPUrl
		}
		if strings.TrimSpace(model.SnapshotURI) == "" {
			model.SnapshotURI = existing.SnapshotURI
		}
		if strings.TrimSpace(model.RTSPStatus) == "" {
			model.RTSPStatus = existing.RTSPStatus
		}
		if strings.TrimSpace(model.RTSPTransport) == "" {
			model.RTSPTransport = existing.RTSPTransport
		}
		if strings.TrimSpace(model.RTSPTracks) == "" {
			model.RTSPTracks = existing.RTSPTracks
		}
		if model.LastStreamCheckAt == 0 {
			model.LastStreamCheckAt = existing.LastStreamCheckAt
		}
		return s.repo.UpdateById(ctx, "", model)
	}
	if err != nil && !isNoResultFoundErr(err) {
		return 0, fmt.Errorf("lookup ONVIF device failed: %w", err)
	}

	return s.repo.Create(ctx, "", model)
}

func (s *onvifDeviceService) SaveDiscovered(ctx context.Context, device onvif.Device) (uint64, error) {
	return s.Save(ctx, deviceToEntity(device))
}

func (s *onvifDeviceService) SaveCredentials(ctx context.Context, id uint64, credentials onvif.Credentials) (*entities.OnvifDevice, error) {
	device, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(credentials.Username) != "" {
		device.Username = strings.TrimSpace(credentials.Username)
	}
	if credentials.Password != "" {
		device.Password = credentials.Password
	}
	if err := s.refreshCapabilities(ctx, device, onvif.Credentials{Username: device.Username, Password: device.Password}); err != nil {
		if strings.TrimSpace(device.MediaXAddr) == "" {
			return nil, err
		}
	}
	device.UpdatedAt = time.Now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *device); err != nil {
		return nil, err
	}
	return device, nil
}

func (s *onvifDeviceService) ChangeCameraPassword(ctx context.Context, id uint64, req ChangeCameraPasswordRequest) (*entities.OnvifDevice, error) {
	device, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	current := onvif.Credentials{
		Username: firstNonEmpty(req.CurrentUsername, device.Username),
		Password: firstNonEmpty(req.CurrentPassword, device.Password),
	}
	targetUsername := firstNonEmpty(req.TargetUsername, current.Username)
	if strings.TrimSpace(targetUsername) == "" {
		return nil, errors.New("targetUsername is required")
	}
	if strings.TrimSpace(req.NewPassword) == "" {
		return nil, errors.New("newPassword is required")
	}
	if err := s.client.ChangeUserPassword(ctx, onvif.ChangeUserPasswordRequest{
		DeviceServiceURL: device.XAddr,
		Credentials:      current,
		TargetUsername:   targetUsername,
		NewPassword:      req.NewPassword,
		UserLevel:        req.UserLevel,
	}); err != nil {
		return nil, err
	}

	device.Username = targetUsername
	device.Password = req.NewPassword
	_ = s.refreshCapabilities(ctx, device, onvif.Credentials{Username: device.Username, Password: device.Password})
	device.UpdatedAt = time.Now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *device); err != nil {
		return nil, err
	}
	return device, nil
}

func (s *onvifDeviceService) ResolveStream(ctx context.Context, id uint64, credentials onvif.Credentials) (*entities.OnvifDevice, error) {
	device, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(credentials.Username) == "" {
		credentials.Username = device.Username
	}
	if credentials.Password == "" {
		credentials.Password = device.Password
	}
	_ = s.refreshCapabilities(ctx, device, credentials)

	res, err := s.client.GetStreamURI(ctx, onvif.StreamURIRequest{
		DeviceServiceURL: device.XAddr,
		MediaServiceURL:  device.MediaXAddr,
		Credentials:      credentials,
	})
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Unix()
	device.Username = credentials.Username
	device.Password = credentials.Password
	device.MediaXAddr = res.MediaXAddr
	device.ProfileToken = res.ProfileToken
	device.RTSPUrl = res.RTSPURL
	device.RTSPStatus = "resolved"
	if snapshot, snapshotErr := s.client.GetSnapshotURI(ctx, onvif.StreamURIRequest{
		DeviceServiceURL: device.XAddr,
		MediaServiceURL:  device.MediaXAddr,
		ProfileToken:     device.ProfileToken,
		Credentials:      credentials,
	}); snapshotErr == nil {
		device.MediaXAddr = firstNonEmpty(snapshot.MediaXAddr, device.MediaXAddr)
		device.ProfileToken = firstNonEmpty(snapshot.ProfileToken, device.ProfileToken)
		device.SnapshotURI = snapshot.SnapshotURI
	}
	device.UpdatedAt = now
	if _, err := s.repo.UpdateById(ctx, "", *device); err != nil {
		return nil, err
	}
	return device, nil
}

func (s *onvifDeviceService) ResolveLiveView(ctx context.Context, id uint64, credentials onvif.Credentials) (*entities.OnvifDevice, error) {
	device, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(credentials.Username) == "" {
		credentials.Username = device.Username
	}
	if credentials.Password == "" {
		credentials.Password = device.Password
	}
	_ = s.refreshCapabilities(ctx, device, credentials)

	snapshot, snapshotErr := s.client.GetSnapshotURI(ctx, onvif.StreamURIRequest{
		DeviceServiceURL: device.XAddr,
		MediaServiceURL:  device.MediaXAddr,
		ProfileToken:     device.ProfileToken,
		Credentials:      credentials,
	})
	stream, streamErr := s.client.GetStreamURI(ctx, onvif.StreamURIRequest{
		DeviceServiceURL: device.XAddr,
		MediaServiceURL:  device.MediaXAddr,
		Credentials:      credentials,
	})
	if streamErr == nil {
		device.MediaXAddr = firstNonEmpty(stream.MediaXAddr, device.MediaXAddr)
		device.ProfileToken = firstNonEmpty(stream.ProfileToken, device.ProfileToken)
		device.RTSPUrl = stream.RTSPURL
		device.RTSPStatus = "resolved"
	}
	if snapshotErr != nil && streamErr != nil && strings.TrimSpace(device.RTSPUrl) == "" {
		return nil, fmt.Errorf("resolve live view failed; snapshot: %v; rtsp: %v", snapshotErr, streamErr)
	}
	if snapshotErr != nil && strings.TrimSpace(device.RTSPUrl) == "" {
		return nil, snapshotErr
	}

	device.Username = credentials.Username
	device.Password = credentials.Password
	if snapshotErr == nil {
		device.MediaXAddr = firstNonEmpty(snapshot.MediaXAddr, device.MediaXAddr)
		device.ProfileToken = firstNonEmpty(snapshot.ProfileToken, device.ProfileToken)
		device.SnapshotURI = snapshot.SnapshotURI
	}
	device.UpdatedAt = time.Now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *device); err != nil {
		return nil, err
	}
	return device, nil
}

func (s *onvifDeviceService) PTZMove(ctx context.Context, id uint64, req PTZMoveRequest) (*entities.OnvifDevice, error) {
	device, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	pan, tilt, zoom, err := ptzVelocity(req.Direction, req.Speed)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePTZReady(ctx, device); err != nil {
		return nil, err
	}
	if err := s.client.PTZMove(ctx, onvif.PTZMoveRequest{
		DeviceServiceURL: device.XAddr,
		PTZServiceURL:    device.PTZXAddr,
		ProfileToken:     device.ProfileToken,
		Credentials:      onvif.Credentials{Username: device.Username, Password: device.Password},
		Pan:              pan,
		Tilt:             tilt,
		Zoom:             zoom,
		Duration:         time.Duration(req.DurationMs) * time.Millisecond,
	}); err != nil {
		return nil, err
	}
	return device, nil
}

func (s *onvifDeviceService) PTZStop(ctx context.Context, id uint64) (*entities.OnvifDevice, error) {
	device, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePTZReady(ctx, device); err != nil {
		return nil, err
	}
	if err := s.client.PTZStop(ctx, onvif.PTZMoveRequest{
		DeviceServiceURL: device.XAddr,
		PTZServiceURL:    device.PTZXAddr,
		ProfileToken:     device.ProfileToken,
		Credentials:      onvif.Credentials{Username: device.Username, Password: device.Password},
	}); err != nil {
		return nil, err
	}
	return device, nil
}

func (s *onvifDeviceService) SnapshotSource(ctx context.Context, id uint64) (SnapshotSource, error) {
	device, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return SnapshotSource{}, err
	}
	if strings.TrimSpace(device.SnapshotURI) == "" || strings.TrimSpace(device.RTSPUrl) == "" {
		device, err = s.ResolveLiveView(ctx, id, onvif.Credentials{})
		if err != nil {
			return SnapshotSource{}, err
		}
	}
	if strings.TrimSpace(device.SnapshotURI) == "" && strings.TrimSpace(device.RTSPUrl) == "" {
		return SnapshotSource{}, errors.New("snapshotUri or rtspUrl is required; resolve live view first")
	}
	return SnapshotSource{
		URI:      device.SnapshotURI,
		RTSPURI:  rtspURIWithCredentials(device.RTSPUrl, device.Username, device.Password),
		Username: device.Username,
		Password: device.Password,
	}, nil
}

func (s *onvifDeviceService) TestStream(ctx context.Context, id uint64) (*rtsp.ProbeResult, error) {
	device, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(device.RTSPUrl) == "" {
		return nil, errors.New("rtspUrl is required; resolve stream URI first")
	}

	result, err := s.rtspClient.Probe(ctx, rtspURIWithCredentials(device.RTSPUrl, device.Username, device.Password), rtsp.OpenOptions{})
	now := time.Now().UTC().Unix()
	device.LastStreamCheckAt = now
	device.UpdatedAt = now
	if err != nil {
		device.RTSPStatus = "offline"
		_, _ = s.repo.UpdateById(ctx, "", *device)
		return nil, err
	}

	device.RTSPStatus = "online"
	device.RTSPTransport = result.Transport
	if tracks, marshalErr := json.Marshal(result.Tracks); marshalErr == nil {
		device.RTSPTracks = string(tracks)
	}
	if _, err := s.repo.UpdateById(ctx, "", *device); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *onvifDeviceService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return s.repo.DeleteById(ctx, "", id)
}

func deviceToEntity(device onvif.Device) entities.OnvifDevice {
	return entities.OnvifDevice{
		Name:            device.Name,
		Host:            device.Host,
		Port:            device.Port,
		XAddr:           device.XAddr,
		Types:           strings.Join(device.Types, " "),
		Scopes:          strings.Join(device.Scopes, " "),
		HardwareID:      device.HardwareID,
		Manufacturer:    device.Manufacturer,
		Model:           device.Model,
		FirmwareVersion: device.FirmwareVersion,
		SerialNumber:    device.SerialNumber,
		MediaXAddr:      device.MediaXAddr,
		PTZXAddr:        device.PTZXAddr,
		PTZSupported:    device.PTZSupported,
		ProfileToken:    device.ProfileToken,
		RTSPUrl:         device.RTSPURL,
		SnapshotURI:     device.SnapshotURI,
		LastSeenAt:      device.LastSeenAt,
		IsActive:        true,
	}
}

func (s *onvifDeviceService) refreshCapabilities(ctx context.Context, device *entities.OnvifDevice, credentials onvif.Credentials) error {
	if device == nil || strings.TrimSpace(device.XAddr) == "" {
		return nil
	}
	capabilities, err := s.client.GetCapabilities(ctx, device.XAddr, credentials)
	if err != nil {
		return err
	}
	if strings.TrimSpace(capabilities.MediaXAddr) != "" {
		device.MediaXAddr = capabilities.MediaXAddr
	}
	device.PTZXAddr = capabilities.PTZXAddr
	device.PTZSupported = capabilities.PTZSupported
	return nil
}

func (s *onvifDeviceService) ensurePTZReady(ctx context.Context, device *entities.OnvifDevice) error {
	if device == nil {
		return errors.New("device is required")
	}
	credentials := onvif.Credentials{Username: device.Username, Password: device.Password}
	if strings.TrimSpace(device.PTZXAddr) == "" {
		if err := s.refreshCapabilities(ctx, device, credentials); err != nil {
			return err
		}
		device.UpdatedAt = time.Now().UTC().Unix()
		_, _ = s.repo.UpdateById(ctx, "", *device)
	}
	if strings.TrimSpace(device.PTZXAddr) == "" || !device.PTZSupported {
		return errors.New("camera does not expose ONVIF PTZ service")
	}
	if strings.TrimSpace(device.ProfileToken) == "" {
		res, err := s.client.GetStreamURI(ctx, onvif.StreamURIRequest{
			DeviceServiceURL: device.XAddr,
			MediaServiceURL:  device.MediaXAddr,
			Credentials:      credentials,
		})
		if err != nil {
			return err
		}
		device.MediaXAddr = res.MediaXAddr
		device.ProfileToken = res.ProfileToken
		if strings.TrimSpace(device.RTSPUrl) == "" {
			device.RTSPUrl = res.RTSPURL
		}
		device.UpdatedAt = time.Now().UTC().Unix()
		_, _ = s.repo.UpdateById(ctx, "", *device)
	}
	if strings.TrimSpace(device.ProfileToken) == "" {
		return errors.New("ONVIF profile token is required for PTZ")
	}
	return nil
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

func rtspURIWithCredentials(rawURI string, username string, password string) string {
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
