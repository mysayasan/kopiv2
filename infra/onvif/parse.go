package onvif

import (
	"encoding/xml"
	"strings"
	"time"
)

type probeEnvelopeXML struct {
	Body probeBodyXML `xml:"Body"`
}

type probeBodyXML struct {
	ProbeMatches probeMatchesXML `xml:"ProbeMatches"`
}

type probeMatchesXML struct {
	Matches []probeMatchXML `xml:"ProbeMatch"`
}

type probeMatchXML struct {
	Types  string `xml:"Types"`
	Scopes string `xml:"Scopes"`
	XAddrs string `xml:"XAddrs"`
}

type deviceInformationEnvelopeXML struct {
	Body deviceInformationBodyXML `xml:"Body"`
}

type deviceInformationBodyXML struct {
	Response DeviceInformation `xml:"GetDeviceInformationResponse"`
}

type capabilitiesEnvelopeXML struct {
	Body capabilitiesBodyXML `xml:"Body"`
}

type capabilitiesBodyXML struct {
	Response capabilitiesResponseXML `xml:"GetCapabilitiesResponse"`
}

type capabilitiesResponseXML struct {
	Capabilities capabilitiesXML `xml:"Capabilities"`
}

type capabilitiesXML struct {
	Media mediaCapabilityXML `xml:"Media"`
	PTZ   ptzCapabilityXML   `xml:"PTZ"`
}

type mediaCapabilityXML struct {
	XAddr string `xml:"XAddr"`
}

type ptzCapabilityXML struct {
	XAddr string `xml:"XAddr"`
}

type profilesEnvelopeXML struct {
	Body profilesBodyXML `xml:"Body"`
}

type profilesBodyXML struct {
	Response profilesResponseXML `xml:"GetProfilesResponse"`
}

type profilesResponseXML struct {
	Profiles []profileXML `xml:"Profiles"`
}

type profileXML struct {
	Token                     string                       `xml:"token,attr"`
	Name                      string                       `xml:"Name"`
	VideoEncoderConfiguration videoEncoderConfigurationXML `xml:"VideoEncoderConfiguration"`
}

type videoEncoderConfigurationXML struct {
	Encoding   string        `xml:"Encoding"`
	Resolution resolutionXML `xml:"Resolution"`
}

type resolutionXML struct {
	Width  int `xml:"Width"`
	Height int `xml:"Height"`
}

type streamURIEnvelopeXML struct {
	Body streamURIBodyXML `xml:"Body"`
}

type streamURIBodyXML struct {
	Response streamURIResponseXML `xml:"GetStreamUriResponse"`
}

type streamURIResponseXML struct {
	MediaURI mediaURIXML `xml:"MediaUri"`
}

type mediaURIXML struct {
	URI string `xml:"Uri"`
}

type snapshotURIEnvelopeXML struct {
	Body snapshotURIBodyXML `xml:"Body"`
}

type snapshotURIBodyXML struct {
	Response snapshotURIResponseXML `xml:"GetSnapshotUriResponse"`
}

type snapshotURIResponseXML struct {
	MediaURI mediaURIXML `xml:"MediaUri"`
}

// DeviceInformation contains the standard ONVIF device information response.
type DeviceInformation struct {
	Manufacturer    string `json:"manufacturer" xml:"Manufacturer"`
	Model           string `json:"model" xml:"Model"`
	FirmwareVersion string `json:"firmwareVersion" xml:"FirmwareVersion"`
	SerialNumber    string `json:"serialNumber" xml:"SerialNumber"`
	HardwareID      string `json:"hardwareId" xml:"HardwareId"`
}

// ParseProbeMatches parses a WS-Discovery ProbeMatches SOAP response.
func ParseProbeMatches(data []byte) ([]Device, error) {
	var envelope probeEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Unix()
	devices := make([]Device, 0, len(envelope.Body.ProbeMatches.Matches))
	for _, match := range envelope.Body.ProbeMatches.Matches {
		xaddr := firstToken(match.XAddrs)
		if xaddr == "" {
			continue
		}
		device := DeviceFromXAddr(xaddr)
		device.Types = fields(match.Types)
		device.Scopes = fields(match.Scopes)
		device.LastSeenAt = now
		applyScopeHints(&device)
		devices = append(devices, device)
	}

	return devices, nil
}

// ParseDeviceInformation parses an ONVIF GetDeviceInformation SOAP response.
func ParseDeviceInformation(data []byte) (DeviceInformation, error) {
	var envelope deviceInformationEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return DeviceInformation{}, err
	}
	return envelope.Body.Response, nil
}

// ParseMediaXAddr parses the ONVIF media service URL from GetCapabilities.
func ParseMediaXAddr(data []byte) (string, error) {
	capabilities, err := ParseServiceXAddrs(data)
	if err != nil {
		return "", err
	}
	return capabilities.MediaXAddr, nil
}

// ServiceXAddrs contains ONVIF service endpoint URLs from GetCapabilities.
type ServiceXAddrs struct {
	MediaXAddr string `json:"mediaXAddr"`
	PTZXAddr   string `json:"ptzXAddr"`
}

// ParseServiceXAddrs parses ONVIF service URLs from GetCapabilities.
func ParseServiceXAddrs(data []byte) (ServiceXAddrs, error) {
	var envelope capabilitiesEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return ServiceXAddrs{}, err
	}
	return ServiceXAddrs{
		MediaXAddr: strings.TrimSpace(envelope.Body.Response.Capabilities.Media.XAddr),
		PTZXAddr:   strings.TrimSpace(envelope.Body.Response.Capabilities.PTZ.XAddr),
	}, nil
}

// ParseFirstProfileToken parses the first media profile token from GetProfiles.
func ParseFirstProfileToken(data []byte) (string, error) {
	var envelope profilesEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return "", err
	}
	for _, profile := range envelope.Body.Response.Profiles {
		if strings.TrimSpace(profile.Token) != "" {
			return strings.TrimSpace(profile.Token), nil
		}
	}
	return "", nil
}

// ParsePreferredProfileToken parses the lowest-cost media profile token from GetProfiles.
func ParsePreferredProfileToken(data []byte) (string, error) {
	var envelope profilesEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return "", err
	}

	bestToken := ""
	bestScore := int64(1<<63 - 1)
	for idx, profile := range envelope.Body.Response.Profiles {
		token := strings.TrimSpace(profile.Token)
		if token == "" {
			continue
		}
		score := profileCost(profile, idx)
		if score < bestScore {
			bestToken = token
			bestScore = score
		}
	}
	return bestToken, nil
}

// ParseStreamURI parses the RTSP URI from GetStreamUri.
func ParseStreamURI(data []byte) (string, error) {
	var envelope streamURIEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return "", err
	}
	return strings.TrimSpace(envelope.Body.Response.MediaURI.URI), nil
}

func profileCost(profile profileXML, idx int) int64 {
	width := profile.VideoEncoderConfiguration.Resolution.Width
	height := profile.VideoEncoderConfiguration.Resolution.Height
	score := int64(1_000_000_000_000 + idx)
	if width > 0 && height > 0 {
		score = int64(width)*int64(height) + int64(idx)
	} else if lowProfileHint(profile) {
		score = int64(500_000_000_000 + idx)
	}
	encoding := strings.ToLower(strings.TrimSpace(profile.VideoEncoderConfiguration.Encoding))
	if strings.Contains(encoding, "265") || strings.Contains(encoding, "hevc") {
		score += 10_000_000_000
	}
	if strings.Contains(encoding, "jpeg") || strings.Contains(encoding, "jpg") {
		score += 20_000_000_000
	}
	return score
}

func lowProfileHint(profile profileXML) bool {
	value := strings.ToLower(strings.Join([]string{profile.Token, profile.Name}, " "))
	for _, hint := range []string{"sub", "low", "minor", "secondary", "stream2", "channel2"} {
		if strings.Contains(value, hint) {
			return true
		}
	}
	return false
}

// ParseSnapshotURI parses the JPEG snapshot URI from GetSnapshotUri.
func ParseSnapshotURI(data []byte) (string, error) {
	var envelope snapshotURIEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return "", err
	}
	return strings.TrimSpace(envelope.Body.Response.MediaURI.URI), nil
}

func applyScopeHints(device *Device) {
	for _, scope := range device.Scopes {
		value := scopeValue(scope)
		switch {
		case strings.Contains(scope, "/name/") && device.Name == device.Host:
			device.Name = value
		case strings.Contains(scope, "/hardware/") && device.HardwareID == "":
			device.HardwareID = value
		}
	}
}

func scopeValue(scope string) string {
	idx := strings.LastIndex(scope, "/")
	if idx < 0 || idx == len(scope)-1 {
		return scope
	}
	value := scope[idx+1:]
	value = strings.ReplaceAll(value, "%20", " ")
	value = strings.ReplaceAll(value, "_", " ")
	return value
}

func firstToken(value string) string {
	parts := fields(value)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func fields(value string) []string {
	raw := strings.Fields(strings.TrimSpace(value))
	result := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
