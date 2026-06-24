package onvif

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

// VideoEncoderConfig is the subset of an ONVIF VideoEncoderConfiguration we read
// and write to push a recording codec + bitrate cap to a camera's encoder. It is
// read from GetProfiles (which embeds the full configuration) and round-tripped
// back via SetVideoEncoderConfiguration so unrelated fields are preserved.
type VideoEncoderConfig struct {
	Token            string `json:"token"`    // configuration token (required to Set)
	Name             string `json:"name"`     // human label
	Encoding         string `json:"encoding"` // JPEG | H264 | H265
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	Quality          string `json:"quality"`          // camera quality scalar (kept as-is)
	FrameRateLimit   int    `json:"frameRateLimit"`   // fps cap
	EncodingInterval int    `json:"encodingInterval"` // encode 1 of N frames
	BitrateLimit     int    `json:"bitrateLimit"`     // kbps
	GovLength        int    `json:"govLength"`        // keyframe interval
	H264Profile      string `json:"h264Profile"`      // Baseline|Main|High (H264 only)
	SessionTimeout   string `json:"sessionTimeout"`   // ISO-8601 duration
	ProfileToken     string `json:"profileToken"`     // media profile this came from
}

// ConfigureRecordingRequest pushes a recording codec + optional bitrate cap to the
// camera's encoder for one media profile. Encoding is "H264" or "H265";
// BitrateLimitKbps <= 0 leaves the camera's current bitrate untouched.
type ConfigureRecordingRequest struct {
	DeviceServiceURL string      `json:"deviceServiceUrl"`
	MediaServiceURL  string      `json:"mediaServiceUrl"`
	ProfileToken     string      `json:"profileToken"`
	Credentials      Credentials `json:"credentials"`
	Encoding         string      `json:"encoding"`
	BitrateLimitKbps int         `json:"bitrateLimitKbps"`
}

// GetVideoEncoderConfig reads the current encoder configuration for a media
// profile (resolving the media service URL and a preferred profile when unset).
func (c *Client) GetVideoEncoderConfig(ctx context.Context, req StreamURIRequest) (*VideoEncoderConfig, error) {
	mediaURL, profileToken, err := c.resolveMediaProfile(ctx, req.DeviceServiceURL, req.MediaServiceURL, req.ProfileToken, req.Credentials)
	if err != nil {
		return nil, err
	}
	cfg, err := c.readVideoEncoderConfig(ctx, mediaURL, profileToken, req.Credentials)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// ConfigureRecording applies the requested codec (+ optional bitrate cap) to the
// camera by reading its current encoder config and writing it back with only the
// encoding/bitrate changed. Best-effort: cameras that don't support the requested
// codec return a SOAP fault, surfaced as the error.
func (c *Client) ConfigureRecording(ctx context.Context, req ConfigureRecordingRequest) (*VideoEncoderConfig, error) {
	encoding := normalizeEncoding(req.Encoding)
	if encoding == "" {
		return nil, fmt.Errorf("unsupported recording encoding %q (want H264 or H265)", req.Encoding)
	}
	mediaURL, profileToken, err := c.resolveMediaProfile(ctx, req.DeviceServiceURL, req.MediaServiceURL, req.ProfileToken, req.Credentials)
	if err != nil {
		return nil, err
	}
	cfg, err := c.readVideoEncoderConfig(ctx, mediaURL, profileToken, req.Credentials)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("camera did not report a video encoder configuration token; cannot apply recording quality")
	}

	target := *cfg
	target.Encoding = encoding
	if req.BitrateLimitKbps > 0 {
		target.BitrateLimit = req.BitrateLimitKbps
	}

	// Apply. H.265 is only defined in ONVIF Media2 (ver20) — many cameras accept a
	// Media1 (ver10) set with HTTP 200 but silently keep H.264 — so try Media2 first
	// when the camera advertises it, then fall back to Media1. Collect errors so a
	// rejection is reported rather than masked by a misleading success.
	var applyErrs []string
	appliedVia := ""
	media2URL, _ := c.getMedia2ServiceURL(ctx, req.DeviceServiceURL, req.Credentials)
	media2Available := strings.TrimSpace(media2URL) != ""
	if media2Available {
		if _, _, err := c.postSOAP(ctx, media2URL, setVideoEncoder2ConfigurationBody(target), req.Credentials); err != nil {
			applyErrs = append(applyErrs, fmt.Sprintf("media2: %v", err))
		} else {
			appliedVia = "media2"
		}
	}
	if appliedVia == "" {
		if _, _, err := c.postSOAP(ctx, mediaURL, setVideoEncoderConfigurationBody(target), req.Credentials); err != nil {
			applyErrs = append(applyErrs, fmt.Sprintf("media1: %v", err))
			return nil, fmt.Errorf("apply camera recording encoder failed (the camera may not support %s): %s", encoding, strings.Join(applyErrs, "; "))
		}
		appliedVia = "media1"
	}

	// Re-read what the camera actually kept (via Media1 GetProfiles) so the UI reflects
	// the truth. Cameras often clamp or ignore an unsupported codec while returning
	// success on the Media1 set, so a Media1 write that doesn't stick is a hard failure.
	// A Media2 write is authoritative for H.265 even if the Media1 profile view differs
	// (some cameras expose the two profile sets independently), so we trust an accepted
	// Media2 set and only use the re-read to refresh the displayed details.
	actual, rerr := c.readVideoEncoderConfig(ctx, mediaURL, profileToken, req.Credentials)
	if rerr != nil {
		return &target, nil // couldn't re-read; trust the accepted set
	}
	actual.ProfileToken = profileToken
	kept := !strings.EqualFold(canonicalEncoding(actual.Encoding), canonicalEncoding(encoding))
	if kept && appliedVia == "media1" {
		extra := ""
		if len(applyErrs) > 0 {
			extra = " (" + strings.Join(applyErrs, "; ") + ")"
		}
		// Tailor the reason: H.265 over ONVIF needs Media2 (ver20); if the camera
		// doesn't even expose it, that's the definitive cause.
		reason := "This camera only allows codec changes from its own web UI, or doesn't support changing the codec on this profile over ONVIF"
		if canonicalEncoding(encoding) == "h265" && !media2Available {
			reason = "switching to H.265 over ONVIF requires the Media2 (ver20) service, which this camera does not expose"
		}
		return actual, fmt.Errorf("the camera did not apply %s — it kept %s. %s. Tip: set the storage codec to HEVC in Settings → Recording to compress on the host (GPU) instead%s",
			encoding, strings.ToUpper(orDefault(actual.Encoding, "its previous codec")), reason, extra)
	}
	if kept {
		// Media2 accepted it but the Media1 profile view still shows the old codec —
		// reflect what we applied so the UI is consistent with the accepted change.
		actual.Encoding = target.Encoding
		if req.BitrateLimitKbps > 0 {
			actual.BitrateLimit = target.BitrateLimit
		}
	}
	return actual, nil
}

// canonicalEncoding normalizes an ONVIF encoding label for comparison (H265/HEVC
// both map to "h265", etc.).
func canonicalEncoding(encoding string) string {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "h264", "avc", "h.264":
		return "h264"
	case "h265", "hevc", "h.265":
		return "h265"
	}
	return strings.ToLower(strings.TrimSpace(encoding))
}

// resolveMediaProfile fills in the media service URL and a profile token when the
// caller left them blank, reusing the same resolution as the streaming paths.
func (c *Client) resolveMediaProfile(ctx context.Context, deviceServiceURL, mediaServiceURL, profileToken string, creds Credentials) (string, string, error) {
	mediaURL := strings.TrimSpace(mediaServiceURL)
	if mediaURL == "" {
		deviceURL, err := NormalizeDeviceServiceURL(deviceServiceURL)
		if err != nil {
			return "", "", err
		}
		if mediaURL, err = c.getMediaServiceURL(ctx, deviceURL, creds); err != nil {
			return "", "", err
		}
	}
	token := strings.TrimSpace(profileToken)
	if token == "" {
		var err error
		if token, err = c.getPreferredProfileToken(ctx, mediaURL, creds); err != nil {
			return "", "", err
		}
	}
	return mediaURL, token, nil
}

// readVideoEncoderConfig pulls the full VideoEncoderConfiguration for a profile out
// of GetProfiles (which embeds it), avoiding a dependency on the camera honoring a
// separate GetVideoEncoderConfiguration call.
func (c *Client) readVideoEncoderConfig(ctx context.Context, mediaURL, profileToken string, creds Credentials) (*VideoEncoderConfig, error) {
	body, _, err := c.postSOAP(ctx, mediaURL, getProfilesBody(), creds)
	if err != nil {
		return nil, fmt.Errorf("get ONVIF media profiles failed: %w", err)
	}
	cfg, err := ParseVideoEncoderConfig(body, profileToken)
	if err != nil {
		return nil, err
	}
	cfg.ProfileToken = profileToken
	return cfg, nil
}

// getMedia2ServiceURL returns the ONVIF Media2 (ver20) service XAddr via GetServices,
// or "" when the camera does not advertise Media2. Media2 is the only ONVIF media
// profile that defines H.265, so it is preferred for codec changes.
func (c *Client) getMedia2ServiceURL(ctx context.Context, deviceServiceURL string, creds Credentials) (string, error) {
	deviceURL, err := NormalizeDeviceServiceURL(deviceServiceURL)
	if err != nil {
		return "", err
	}
	body, _, err := c.postSOAP(ctx, deviceURL, getServicesBody(), creds)
	if err != nil {
		return "", err
	}
	return ParseMedia2ServiceXAddr(body), nil
}

// ParseMedia2ServiceXAddr finds the ver20 media service XAddr in a GetServices
// response. Returns "" when not present.
func ParseMedia2ServiceXAddr(data []byte) string {
	var env servicesEnvelopeXML
	if err := xml.Unmarshal(data, &env); err != nil {
		return ""
	}
	for _, svc := range env.Body.Response.Services {
		if strings.Contains(strings.ToLower(svc.Namespace), "ver20/media") {
			return strings.TrimSpace(svc.XAddr)
		}
	}
	return ""
}

func getServicesBody() string {
	return `
    <tds:GetServices>
      <tds:IncludeCapability>false</tds:IncludeCapability>
    </tds:GetServices>`
}

type servicesEnvelopeXML struct {
	Body struct {
		Response struct {
			Services []struct {
				Namespace string `xml:"Namespace"`
				XAddr     string `xml:"XAddr"`
			} `xml:"Service"`
		} `xml:"GetServicesResponse"`
	} `xml:"Body"`
}

// setVideoEncoder2ConfigurationBody emits a Media2 (ver20) SetVideoEncoderConfiguration.
// Media2's VideoEncoder2Configuration carries GovLength and Profile as attributes and
// is the profile in which H.265 is valid. Profile defaults to "Main" for the codec.
func setVideoEncoder2ConfigurationBody(c VideoEncoderConfig) string {
	frameRate := c.FrameRateLimit
	if frameRate <= 0 {
		frameRate = 15
	}
	gov := c.GovLength
	if gov <= 0 {
		gov = 30
	}
	profile := c.H264Profile
	if profile == "" {
		profile = "Main"
	}
	quality := strings.TrimSpace(c.Quality)
	if quality == "" {
		quality = "3"
	}
	return fmt.Sprintf(`
    <tr2:SetVideoEncoderConfiguration>
      <tr2:Configuration token="%s" GovLength="%d" Profile="%s">
        <tt:Name>%s</tt:Name>
        <tt:Encoding>%s</tt:Encoding>
        <tt:Resolution>
          <tt:Width>%d</tt:Width>
          <tt:Height>%d</tt:Height>
        </tt:Resolution>
        <tt:RateControl ConstantBitRate="false" FrameRateLimit="%d" BitrateLimit="%d"/>
        <tt:Quality>%s</tt:Quality>
      </tr2:Configuration>
    </tr2:SetVideoEncoderConfiguration>`,
		xmlEscape(c.Token), gov, xmlEscape(profile),
		xmlEscape(orDefault(c.Name, "VideoEncoder")),
		xmlEscape(c.Encoding),
		c.Width, c.Height,
		frameRate, c.BitrateLimit,
		xmlEscape(quality),
	)
}

// normalizeEncoding maps user/codec spellings to the ONVIF Encoding token.
func normalizeEncoding(encoding string) string {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "h264", "avc", "h.264":
		return "H264"
	case "h265", "hevc", "h.265":
		return "H265"
	}
	return ""
}

// — XML parsing --------------------------------------------------------------

type fullProfilesEnvelopeXML struct {
	Body struct {
		Response struct {
			Profiles []fullProfileXML `xml:"Profiles"`
		} `xml:"GetProfilesResponse"`
	} `xml:"Body"`
}

type fullProfileXML struct {
	Token string     `xml:"token,attr"`
	Name  string     `xml:"Name"`
	VEC   fullVECXML `xml:"VideoEncoderConfiguration"`
}

type fullVECXML struct {
	Token       string        `xml:"token,attr"`
	Name        string        `xml:"Name"`
	Encoding    string        `xml:"Encoding"`
	Resolution  resolutionXML `xml:"Resolution"`
	Quality     string        `xml:"Quality"`
	RateControl struct {
		FrameRateLimit   int `xml:"FrameRateLimit"`
		EncodingInterval int `xml:"EncodingInterval"`
		BitrateLimit     int `xml:"BitrateLimit"`
	} `xml:"RateControl"`
	H264 struct {
		GovLength   int    `xml:"GovLength"`
		H264Profile string `xml:"H264Profile"`
	} `xml:"H264"`
	SessionTimeout string `xml:"SessionTimeout"`
}

// ParseVideoEncoderConfig extracts the VideoEncoderConfiguration for the given
// profile token from a GetProfiles SOAP response. When profileToken is empty it
// returns the first profile that carries a video encoder configuration.
func ParseVideoEncoderConfig(data []byte, profileToken string) (*VideoEncoderConfig, error) {
	var env fullProfilesEnvelopeXML
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	want := strings.TrimSpace(profileToken)
	for _, p := range env.Body.Response.Profiles {
		if want != "" && strings.TrimSpace(p.Token) != want {
			continue
		}
		if strings.TrimSpace(p.VEC.Encoding) == "" && strings.TrimSpace(p.VEC.Token) == "" {
			continue
		}
		v := p.VEC
		return &VideoEncoderConfig{
			Token:            strings.TrimSpace(v.Token),
			Name:             strings.TrimSpace(v.Name),
			Encoding:         strings.TrimSpace(v.Encoding),
			Width:            v.Resolution.Width,
			Height:           v.Resolution.Height,
			Quality:          strings.TrimSpace(v.Quality),
			FrameRateLimit:   v.RateControl.FrameRateLimit,
			EncodingInterval: v.RateControl.EncodingInterval,
			BitrateLimit:     v.RateControl.BitrateLimit,
			GovLength:        v.H264.GovLength,
			H264Profile:      strings.TrimSpace(v.H264.H264Profile),
			SessionTimeout:   strings.TrimSpace(v.SessionTimeout),
		}, nil
	}
	if want != "" {
		return nil, fmt.Errorf("ONVIF media profile %q not found or has no video encoder configuration", want)
	}
	return nil, errors.New("no ONVIF video encoder configuration found")
}

// — SOAP body ----------------------------------------------------------------

// setVideoEncoderConfigurationBody emits a Media (ver10) SetVideoEncoderConfiguration
// round-tripping the read configuration with only Encoding/BitrateLimit changed. The
// H264 sub-element is only emitted for H264 (cameras reject an H264 block on an H265
// configuration); H265 cameras keep their existing GOP defaults. ForcePersistence
// makes the change survive a camera reboot.
func setVideoEncoderConfigurationBody(c VideoEncoderConfig) string {
	frameRate := c.FrameRateLimit
	if frameRate <= 0 {
		frameRate = 15
	}
	interval := c.EncodingInterval
	if interval <= 0 {
		interval = 1
	}
	quality := strings.TrimSpace(c.Quality)
	if quality == "" {
		quality = "3"
	}
	session := strings.TrimSpace(c.SessionTimeout)
	if session == "" {
		session = "PT60S"
	}
	codecBlock := ""
	if strings.EqualFold(c.Encoding, "H264") {
		gov := c.GovLength
		if gov <= 0 {
			gov = 30
		}
		profile := c.H264Profile
		if profile == "" {
			profile = "Main"
		}
		codecBlock = fmt.Sprintf(`
        <tt:H264>
          <tt:GovLength>%d</tt:GovLength>
          <tt:H264Profile>%s</tt:H264Profile>
        </tt:H264>`, gov, xmlEscape(profile))
	}
	return fmt.Sprintf(`
    <trt:SetVideoEncoderConfiguration>
      <trt:Configuration token="%s">
        <tt:Name>%s</tt:Name>
        <tt:UseCount>1</tt:UseCount>
        <tt:Encoding>%s</tt:Encoding>
        <tt:Resolution>
          <tt:Width>%d</tt:Width>
          <tt:Height>%d</tt:Height>
        </tt:Resolution>
        <tt:Quality>%s</tt:Quality>
        <tt:RateControl>
          <tt:FrameRateLimit>%d</tt:FrameRateLimit>
          <tt:EncodingInterval>%d</tt:EncodingInterval>
          <tt:BitrateLimit>%d</tt:BitrateLimit>
        </tt:RateControl>%s
        <tt:SessionTimeout>%s</tt:SessionTimeout>
      </trt:Configuration>
      <trt:ForcePersistence>true</trt:ForcePersistence>
    </trt:SetVideoEncoderConfiguration>`,
		xmlEscape(c.Token),
		xmlEscape(orDefault(c.Name, "VideoEncoder")),
		xmlEscape(c.Encoding),
		c.Width, c.Height,
		xmlEscape(quality),
		frameRate, interval, c.BitrateLimit,
		codecBlock,
		xmlEscape(session),
	)
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
