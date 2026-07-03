package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/talk"
)

// TalkCapabilityResult reports whether a camera supports two-way audio
// (talk-back — sending the browser mic to the camera speaker). Only the standard
// ONVIF audio backchannel is supported (Hikvision/Dahua/Axis/most Profile-T
// cameras); it reuses the stored camera credentials, so no extra password.
type TalkCapabilityResult struct {
	Supported bool `json:"supported"`
	// Transport is the delivery path: "onvif" or "none".
	Transport string `json:"transport"`
	// Detail carries a human-readable note when unsupported.
	Detail string `json:"detail,omitempty"`
}

type cachedTalkCapability struct {
	cap TalkCapabilityResult
	at  int64
}

// talkCapabilityCacheTTL bounds how long a resolved talk capability is reused
// before the camera is re-probed (an RTSP DESCRIBE with the backchannel header).
const talkCapabilityCacheTTL = 10 * time.Minute

// TalkCapability reports the camera's talk-back support, cached so the UI can
// poll cheaply.
func (s *cameraService) TalkCapability(ctx context.Context, id int64) TalkCapabilityResult {
	if id <= 0 {
		return TalkCapabilityResult{Detail: "invalid camera id"}
	}
	now := time.Now().Unix()
	ttl := int64(talkCapabilityCacheTTL.Seconds())

	s.talkCapMu.Lock()
	cached, ok := s.talkCapById[id]
	s.talkCapMu.Unlock()
	if ok && now-cached.at < ttl {
		return cached.cap
	}

	result := s.resolveTalkCapability(ctx, id)
	s.talkCapMu.Lock()
	s.talkCapById[id] = cachedTalkCapability{cap: result, at: now}
	s.talkCapMu.Unlock()
	return result
}

func (s *cameraService) resolveTalkCapability(ctx context.Context, id int64) TalkCapabilityResult {
	detail, err := s.loadDetail(ctx, uint64(id))
	if err != nil || detail == nil {
		return TalkCapabilityResult{Detail: "camera not found"}
	}
	if strings.TrimSpace(detail.Camera.RTSPUrl) == "" {
		return TalkCapabilityResult{Transport: "none", Detail: "no RTSP stream to probe for a two-way-audio channel"}
	}

	rtspURI := RTSPURIWithCredentials(detail.Camera.RTSPUrl, detail.Username, detail.Password)
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	hasBackchannel := talk.HasBackchannel(probeCtx, rtspURI)
	cancel()
	if hasBackchannel {
		return TalkCapabilityResult{Supported: true, Transport: "onvif"}
	}
	return TalkCapabilityResult{Transport: "none", Detail: "camera does not expose an ONVIF two-way-audio backchannel"}
}

// OpenTalkSession opens a live talk-back audio session to the camera speaker over
// the ONVIF backchannel using the stored credentials. The caller must Close it.
func (s *cameraService) OpenTalkSession(ctx context.Context, id uint64) (talk.Session, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil || detail == nil {
		return nil, errors.New("camera not found")
	}
	cap := s.TalkCapability(ctx, int64(id))
	if !cap.Supported {
		return nil, errors.New("this camera does not support two-way audio")
	}
	rtspURI := RTSPURIWithCredentials(detail.Camera.RTSPUrl, detail.Username, detail.Password)
	return talk.DialONVIF(rtspURI)
}
