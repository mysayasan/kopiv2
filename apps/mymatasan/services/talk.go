package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/talk"
)

// TalkCapabilityResult reports whether a camera supports two-way audio
// (talk-back — sending the browser mic to the camera speaker) and how.
type TalkCapabilityResult struct {
	Supported bool `json:"supported"`
	// Transport is the resolved delivery path: "onvif" (RTSP audio backchannel),
	// "tapo"/"vigi" (TP-Link proprietary port-8800 protocol), or "none".
	Transport string `json:"transport"`
	// NeedsPassword is true when the transport requires a separately stored
	// speaker password (the TP-Link cloud-account password for Tapo). This also
	// gates the TP-Link speaker config in the UI — it is only ever true for a
	// camera that actually speaks the TP-Link talk protocol.
	NeedsPassword bool `json:"needsPassword"`
	// HasPassword reports whether such a password is already stored.
	HasPassword bool `json:"hasPassword"`
	// Detail carries a human-readable note when unsupported.
	Detail string `json:"detail,omitempty"`
}

type cachedTalkCapability struct {
	cap TalkCapabilityResult
	at  int64
}

// talkCapabilityCacheTTL bounds how long a resolved talk capability is reused
// before the camera is re-probed (RTSP DESCRIBE + a TCP touch of port 8800).
const talkCapabilityCacheTTL = 10 * time.Minute

// TalkCapability reports the camera's talk-back support, cached so the UI can
// poll cheaply. HasPassword is always read live so a just-saved password shows
// immediately.
func (s *cameraService) TalkCapability(ctx context.Context, id int64) TalkCapabilityResult {
	if id <= 0 {
		return TalkCapabilityResult{Detail: "invalid camera id"}
	}
	now := time.Now().Unix()
	ttl := int64(talkCapabilityCacheTTL.Seconds())

	s.talkCapMu.Lock()
	cached, ok := s.talkCapById[id]
	s.talkCapMu.Unlock()

	var result TalkCapabilityResult
	if ok && now-cached.at < ttl {
		result = cached.cap
	} else {
		result = s.resolveTalkCapability(ctx, id)
		s.talkCapMu.Lock()
		s.talkCapById[id] = cachedTalkCapability{cap: result, at: now}
		s.talkCapMu.Unlock()
	}

	// Refresh the live password flag regardless of cache age.
	if result.NeedsPassword {
		result.HasPassword = s.talkPasswordStored(ctx, id)
	}
	return result
}

func (s *cameraService) resolveTalkCapability(ctx context.Context, id int64) TalkCapabilityResult {
	detail, err := s.loadDetail(ctx, uint64(id))
	if err != nil || detail == nil {
		return TalkCapabilityResult{Detail: "camera not found"}
	}

	// Prefer the standard ONVIF backchannel — it reuses the RTSP credentials we
	// already have, so no extra password is needed.
	rtspURI := RTSPURIWithCredentials(detail.Camera.RTSPUrl, detail.Username, detail.Password)
	if strings.TrimSpace(detail.Camera.RTSPUrl) != "" {
		probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		hasBackchannel := talk.HasBackchannel(probeCtx, rtspURI)
		cancel()
		if hasBackchannel {
			return TalkCapabilityResult{Supported: true, Transport: "onvif"}
		}
	}

	// Fall back to the TP-Link proprietary protocol on port 8800 (Tapo/VIGI). The
	// probe only reports success for a genuine TP-Link "Streamd" service, so only
	// cameras that truly have this speaker function surface the password config.
	host := strings.TrimSpace(detail.Camera.Host)
	if host != "" {
		probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		probe := talk.Probe8800(probeCtx, host)
		cancel()
		if probe.Supported {
			transport := "tapo"
			if isVigiCamera(detail) {
				transport = "vigi"
			}
			return TalkCapabilityResult{
				Supported:     true,
				Transport:     transport,
				NeedsPassword: !probe.NoneAuth,
			}
		}
	}

	return TalkCapabilityResult{Transport: "none", Detail: "camera does not expose a supported two-way-audio channel"}
}

// isVigiCamera reports whether a camera is a TP-Link VIGI (vs Tapo) model, which
// derives its port-8800 password differently.
func isVigiCamera(detail *CameraDetail) bool {
	hay := strings.ToLower(strings.Join([]string{
		detail.Camera.Manufacturer, detail.Camera.Model, detail.HardwareID, detail.Scopes,
	}, " "))
	return strings.Contains(hay, "vigi")
}

func (s *cameraService) talkPasswordStored(ctx context.Context, id int64) bool {
	cam, err := s.cameraRepo.GetById(ctx, "", uint64(id))
	if err != nil || cam == nil {
		return false
	}
	return strings.TrimSpace(cam.TalkPassword) != ""
}

// SaveTalkPassword stores the speaker/cloud password used by the TP-Link talk
// transport and invalidates the cached capability so HasPassword refreshes.
func (s *cameraService) SaveTalkPassword(ctx context.Context, id uint64, password string) error {
	detail, err := s.loadDetail(ctx, id)
	if err != nil || detail == nil {
		return errors.New("camera not found")
	}
	detail.Camera.TalkPassword = strings.TrimSpace(password)
	if err := s.saveDetail(ctx, detail); err != nil {
		return err
	}
	s.talkCapMu.Lock()
	delete(s.talkCapById, int64(id))
	s.talkCapMu.Unlock()
	return nil
}

// OpenTalkSession opens a live talk-back audio session to the camera speaker
// using the resolved transport and stored credentials. The caller must Close it.
func (s *cameraService) OpenTalkSession(ctx context.Context, id uint64) (talk.Session, error) {
	detail, err := s.loadDetail(ctx, id)
	if err != nil || detail == nil {
		return nil, errors.New("camera not found")
	}
	cap := s.TalkCapability(ctx, int64(id))
	if !cap.Supported {
		return nil, errors.New("this camera does not support two-way audio")
	}

	switch cap.Transport {
	case "onvif":
		rtspURI := RTSPURIWithCredentials(detail.Camera.RTSPUrl, detail.Username, detail.Password)
		return talk.DialONVIF(rtspURI)
	case "tapo":
		pw := strings.TrimSpace(detail.Camera.TalkPassword)
		if pw == "" {
			return nil, errors.New("a TP-Link cloud password is required to talk to this camera; set it under the camera's Access tab")
		}
		return talk.DialTapo(talk.TapoConfig{
			Host:          detail.Camera.Host,
			Brand:         talk.BrandTapo,
			CloudPassword: pw,
		})
	case "vigi":
		// VIGI can reuse the admin password when no dedicated one is stored.
		pw := strings.TrimSpace(detail.Camera.TalkPassword)
		if pw == "" {
			pw = detail.Password
		}
		if pw == "" {
			return nil, errors.New("a speaker password is required to talk to this camera")
		}
		return talk.DialTapo(talk.TapoConfig{
			Host:     detail.Camera.Host,
			Brand:    talk.BrandVigi,
			Username: detail.Username,
			Password: pw,
		})
	default:
		return nil, errors.New("this camera does not support two-way audio")
	}
}
