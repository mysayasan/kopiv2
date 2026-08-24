package services

import (
	"context"
	"errors"
	"strings"

	"github.com/mysayasan/kopiv2/infra/onvif"
)

// Named PTZ positions on a saved camera (W3-5).
//
// These sit beside PTZMove/PTZStop in camera.go rather than in ptz.go because they need the
// same three things every ONVIF call on this service needs — the stored credentials, the
// resolved PTZ service URL, and a media profile token — and ensurePTZReady is what supplies
// all three, refreshing and persisting the ones a camera was saved without.
//
// The presets themselves are NOT stored here. See infra/onvif/ptz.go.

// ptzPresetMaxNameLength bounds a preset name before it reaches the device. Cameras vary
// wildly in what they accept and a device that truncates silently gives two presets the
// same visible name; refusing early says which name was the problem.
const ptzPresetMaxNameLength = 64

// manualPTZHold is how long an operator keeps a camera after their last manual command.
//
// It is what stops a guard tour stepping the camera away while somebody is looking through
// it. Thirty seconds is long enough to cover the pause between "pan a bit further" and
// "now zoom in", and short enough that a camera nobody is driving goes back on tour before
// the next patrol would have been missed.
const manualPTZHold = 30

// PTZPresets lists the positions this camera has stored.
func (s *cameraService) PTZPresets(ctx context.Context, id uint64) ([]onvif.PTZPreset, error) {
	_, req, err := s.ptzRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	presets, err := s.client.GetPresets(ctx, req)
	if err != nil {
		return nil, err
	}
	if presets == nil {
		// A camera with no presets returns an empty list, never nil: an empty list is a
		// fact ("this camera has nowhere saved yet") and nil renders as a missing field
		// that a client cannot tell from an error.
		presets = []onvif.PTZPreset{}
	}
	return presets, nil
}

// PTZSavePreset stores the camera's current position. An empty presetToken creates a new
// preset; a token overwrites that one.
func (s *cameraService) PTZSavePreset(ctx context.Context, id uint64, name string, presetToken string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errors.New("a preset needs a name")
	}
	if len(trimmed) > ptzPresetMaxNameLength {
		return "", errors.New("that preset name is too long for a camera to store")
	}
	_, req, err := s.ptzRequest(ctx, id)
	if err != nil {
		return "", err
	}
	req.PresetName = trimmed
	req.PresetToken = strings.TrimSpace(presetToken)
	return s.client.SetPreset(ctx, req)
}

// PTZDeletePreset removes a stored position from the camera.
func (s *cameraService) PTZDeletePreset(ctx context.Context, id uint64, presetToken string) error {
	_, req, err := s.ptzRequest(ctx, id)
	if err != nil {
		return err
	}
	req.PresetToken = strings.TrimSpace(presetToken)
	return s.client.RemovePreset(ctx, req)
}

// PTZGotoPreset sends the camera to a stored position.
func (s *cameraService) PTZGotoPreset(ctx context.Context, id uint64, presetToken string, speed float64) error {
	_, req, err := s.ptzRequest(ctx, id)
	if err != nil {
		return err
	}
	req.PresetToken = strings.TrimSpace(presetToken)
	req.Speed = speed
	if err := s.client.GotoPreset(ctx, req); err != nil {
		return err
	}
	// Recorded only on SUCCESS. A refused move did not change the view, and telling the
	// tamper monitor to forget this camera's baseline for a move that never happened
	// would blind it for the next half-window for nothing.
	s.noteCommandedMove(int64(id))
	return nil
}

// PTZHome sends the camera to its home position.
func (s *cameraService) PTZHome(ctx context.Context, id uint64) error {
	_, req, err := s.ptzRequest(ctx, id)
	if err != nil {
		return err
	}
	if err := s.client.GotoHome(ctx, req); err != nil {
		return err
	}
	s.noteCommandedMove(int64(id))
	return nil
}

// PTZSetHome makes the camera's current position its home.
func (s *cameraService) PTZSetHome(ctx context.Context, id uint64) error {
	_, req, err := s.ptzRequest(ctx, id)
	if err != nil {
		return err
	}
	// Deliberately NOT a commanded move: setting home records where the camera already
	// is and does not move it, so the picture has not changed and the tamper baseline is
	// still valid.
	return s.client.SetHome(ctx, req)
}

// PTZStatus reads where the camera is pointing and whether it is still moving.
func (s *cameraService) PTZStatus(ctx context.Context, id uint64) (*onvif.PTZStatus, error) {
	_, req, err := s.ptzRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.client.PTZGetStatus(ctx, req)
}

// ptzRequest resolves one camera into an addressed PTZ request, refreshing and persisting
// the PTZ service URL and profile token if the camera was saved without them.
func (s *cameraService) ptzRequest(ctx context.Context, id uint64) (*CameraDetail, onvif.PTZPresetRequest, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, onvif.PTZPresetRequest{}, err
	}
	if err := s.ensurePTZReady(ctx, detail); err != nil {
		return nil, onvif.PTZPresetRequest{}, err
	}
	return detail, onvif.PTZPresetRequest{
		DeviceServiceURL: detail.XAddr,
		PTZServiceURL:    detail.PTZXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      onvif.Credentials{Username: detail.Username, Password: detail.Password},
	}, nil
}

func (s *cameraService) noteCommandedMove(cameraId int64) {
	s.ptzJournal.NoteCommandedMove(cameraId)
}
