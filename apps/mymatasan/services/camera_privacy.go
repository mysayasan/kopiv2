package services

import (
	"context"
	"errors"
	"strings"

	"github.com/mysayasan/kopiv2/infra/onvif"
)

// The camera service's privacy-mask legs (W3-6), beside the PTZ and relay ones and for the
// same reason: they need the stored credentials and a camera that is actually ONVIF-managed,
// and a second place that resolves those is a second place to get them wrong.

// MaskOptions reports what this camera can do with privacy masks.
//
// A camera that cannot is not an error: "this camera cannot mask anything itself" is an
// answer the product has to show, and turning it into a failure would make the privacy
// screen refuse to load for exactly the cameras that need the warning most.
func (s *cameraService) MaskOptions(ctx context.Context, id uint64) (*onvif.MaskOptions, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	// A camera that is not ONVIF-managed at all — a plain RTSP URL somebody typed in —
	// CANNOT mask, and that is an answer rather than a failure to reach it. Returning an
	// error here made the privacy screen report "the camera could not be reached" about a
	// camera that was working perfectly and simply has no ONVIF at all, which sends
	// somebody to check the network for a fact about the camera.
	if strings.TrimSpace(detail.XAddr) == "" {
		return &onvif.MaskOptions{Supported: false}, nil
	}
	token, err := s.videoSourceToken(ctx, detail)
	if err != nil {
		return &onvif.MaskOptions{Supported: false}, nil
	}
	return s.client.GetMaskOptions(ctx, onvif.MaskRequest{
		DeviceServiceURL:   detail.XAddr,
		Credentials:        s.cameraOnvifCredentials(detail),
		ConfigurationToken: token,
	})
}

// CameraMasks lists the masks currently stored on the camera — including any set up from
// the camera's own web page, which the product must not silently remove.
func (s *cameraService) CameraMasks(ctx context.Context, id uint64) ([]onvif.Mask, error) {
	detail, err := s.onvifDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	token, err := s.videoSourceToken(ctx, detail)
	if err != nil {
		return nil, err
	}
	masks, err := s.client.GetMasks(ctx, onvif.MaskRequest{
		DeviceServiceURL:   detail.XAddr,
		Credentials:        s.cameraOnvifCredentials(detail),
		ConfigurationToken: token,
	})
	if err != nil {
		return nil, err
	}
	if masks == nil {
		masks = []onvif.Mask{}
	}
	return masks, nil
}

func (s *cameraService) CreateCameraMask(ctx context.Context, id uint64, mask onvif.Mask) (string, error) {
	detail, err := s.onvifDetail(ctx, id)
	if err != nil {
		return "", err
	}
	return s.client.CreateMask(ctx, onvif.MaskRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
		Mask:             mask,
	})
}

func (s *cameraService) SetCameraMask(ctx context.Context, id uint64, mask onvif.Mask) error {
	detail, err := s.onvifDetail(ctx, id)
	if err != nil {
		return err
	}
	return s.client.SetMask(ctx, onvif.MaskRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
		Mask:             mask,
	})
}

func (s *cameraService) DeleteCameraMask(ctx context.Context, id uint64, token string) error {
	detail, err := s.onvifDetail(ctx, id)
	if err != nil {
		return err
	}
	return s.client.DeleteMask(ctx, onvif.MaskRequest{
		DeviceServiceURL: detail.XAddr,
		Credentials:      s.cameraOnvifCredentials(detail),
		Token:            strings.TrimSpace(token),
	})
}

// VideoSourceToken returns the video source configuration a mask attaches to.
func (s *cameraService) VideoSourceToken(ctx context.Context, id uint64) (string, error) {
	detail, err := s.onvifDetail(ctx, id)
	if err != nil {
		return "", err
	}
	return s.videoSourceToken(ctx, detail)
}

// videoSourceToken resolves the VideoSourceConfiguration token for a camera's recording
// profile.
//
// A mask belongs to a video source CONFIGURATION, not to a profile and not to the device —
// which matters on a multi-sensor camera, where masking the wrong configuration masks a
// lens nobody was worried about and leaves the one they were worried about clear.
func (s *cameraService) videoSourceToken(ctx context.Context, detail *CameraDetail) (string, error) {
	profiles, err := s.client.GetProfileDetails(ctx, onvif.StreamURIRequest{
		DeviceServiceURL: detail.XAddr,
		MediaServiceURL:  detail.MediaXAddr,
		ProfileToken:     detail.ProfileToken,
		Credentials:      s.cameraOnvifCredentials(detail),
	})
	if err != nil {
		return "", err
	}
	// Prefer the profile the appliance actually records, so the mask covers the pixels
	// that reach the disk.
	for _, p := range profiles {
		if p.Token == detail.ProfileToken && strings.TrimSpace(p.VideoSourceToken) != "" {
			return p.VideoSourceToken, nil
		}
	}
	for _, p := range profiles {
		if strings.TrimSpace(p.VideoSourceToken) != "" {
			return p.VideoSourceToken, nil
		}
	}
	return "", errors.New("this camera did not report a video source configuration, which is what a privacy mask attaches to")
}
