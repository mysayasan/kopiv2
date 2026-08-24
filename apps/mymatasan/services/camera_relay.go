package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/onvif"
)

// The camera service's relay and event legs (W3-5b).
//
// They sit here, beside the PTZ ones, because they need the same three things every ONVIF
// call on this service needs — the stored credentials, the device service URL, and a camera
// that is actually ONVIF-managed — and because the alternative is a second place that
// resolves a camera's credentials.

// RelayOutputs lists the dry contacts on a camera's terminal block.
func (s *cameraService) RelayOutputs(ctx context.Context, id uint64) ([]onvif.RelayOutput, error) {
	detail, err := s.onvifDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	outputs, err := s.client.GetRelayOutputs(ctx, onvif.RelayRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
	})
	if err != nil {
		return nil, err
	}
	if outputs == nil {
		outputs = []onvif.RelayOutput{}
	}
	return outputs, nil
}

// SetRelayState drives one output to active or idle.
func (s *cameraService) SetRelayState(ctx context.Context, id uint64, token string, active bool) error {
	detail, err := s.onvifDetail(ctx, id)
	if err != nil {
		return err
	}
	return s.client.SetRelayOutputState(ctx, onvif.RelayRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
		Token:            strings.TrimSpace(token),
		Active:           active,
	})
}

// SetRelayPulseMode asks the camera to run an output as a timed pulse of its own.
//
// This is what lets the relay service hand the responsibility for switching off to the
// DEVICE: a monostable output releases itself after its delay whether or not this appliance
// is still running. Cameras that will not be reconfigured surface their own refusal, and the
// caller falls back to holding the output from here.
func (s *cameraService) SetRelayPulseMode(ctx context.Context, id uint64, token string, seconds int) error {
	detail, err := s.onvifDetail(ctx, id)
	if err != nil {
		return err
	}
	return s.client.SetRelayOutputSettings(ctx, onvif.RelayRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
		Token:            strings.TrimSpace(token),
		Mode:             onvif.RelayModeMonostable,
		DelaySeconds:     seconds,
		IdleState:        onvif.RelayIdleClosed,
	})
}

// EventEndpoint resolves a camera's ONVIF event service URL, so the event monitor can
// subscribe without re-resolving credentials for itself.
func (s *cameraService) EventEndpoint(ctx context.Context, id uint64) (string, onvif.Credentials, error) {
	detail, err := s.onvifDetail(ctx, id)
	if err != nil {
		return "", onvif.Credentials{}, err
	}
	credentials := s.cameraOnvifCredentials(detail)
	services, err := s.client.GetServices(ctx, onvif.DeviceRequest{
		DeviceServiceURL: detail.XAddr, Credentials: credentials,
	})
	if err != nil {
		return "", credentials, err
	}
	for _, svc := range services {
		if strings.Contains(strings.ToLower(svc.Namespace), "/events") && strings.TrimSpace(svc.XAddr) != "" {
			return strings.TrimSpace(svc.XAddr), credentials, nil
		}
	}
	return "", credentials, errors.New("this camera does not offer an ONVIF event service")
}

// onvifDetail loads a camera and refuses one that is not ONVIF-managed, which is the only
// precondition the relay and event calls share.
func (s *cameraService) onvifDetail(ctx context.Context, id uint64) (*CameraDetail, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(detail.XAddr) == "" {
		return nil, errors.New("camera is not ONVIF-managed")
	}
	return detail, nil
}

// relayStuckNotification is what an operator is told when an output was energised and this
// appliance could not switch it back off.
//
// It is the worst outcome in relay.go, so it is a NOTIFICATION rather than a log line:
// something in the building is running, nothing is going to stop it, and only a person can
// now. Written for whoever reads it at four in the morning — what is happening, on what,
// and what to do — rather than for whoever wrote the code.
func relayStuckNotification(cameraId int64, cameraName string, token string, cause error) notification.Notification {
	return notification.Notification{
		Category: notification.CategoryDeviceAlert,
		Severity: notification.Critical,
		Title:    "Output could not be switched off",
		Body: fmt.Sprintf(
			"%s left output %s switched on: the camera did not accept the command to release it (%v). "+
				"It will stay on until it is switched off by hand or the camera is restarted.",
			cameraName, token, cause),
		Source:   "relay",
		CameraId: cameraId,
		RefType:  "camera",
		RefId:    cameraId,
		Data: map[string]any{
			"cameraId": cameraId, "cameraName": cameraName,
			"relayToken": token, "reason": cause.Error(),
		},
	}
}
