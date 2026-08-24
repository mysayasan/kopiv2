package onvif

import (
	"encoding/xml"
	"fmt"
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
	VideoSourceConfiguration  videoSourceConfigurationXML  `xml:"VideoSourceConfiguration"`
}

type videoSourceConfigurationXML struct {
	Token string `xml:"token,attr"`
}

type videoEncoderConfigurationXML struct {
	Encoding   string        `xml:"Encoding"`
	Resolution resolutionXML `xml:"Resolution"`
}

type resolutionXML struct {
	Width  int `xml:"Width"`
	Height int `xml:"Height"`
}

// MediaProfile is a selectable ONVIF media profile exposed by GetProfiles.
type MediaProfile struct {
	Token    string `json:"profileToken"`
	Name     string `json:"name"`
	Encoding string `json:"encoding"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	// VideoSourceToken is the VideoSourceConfiguration this profile draws from, which is
	// what a privacy mask attaches to (W3-6).
	//
	// It matters most on a MULTI-SENSOR camera: masking the wrong configuration masks a
	// lens nobody was worried about and leaves the one they were worried about clear, and
	// both look like success from here.
	VideoSourceToken string `json:"videoSourceToken"`
}

// User is a local ONVIF camera account (Device Management GetUsers).
type User struct {
	Username  string `json:"username"`
	UserLevel string `json:"userLevel"`
}

type usersEnvelopeXML struct {
	Body usersBodyXML `xml:"Body"`
}

type usersBodyXML struct {
	Response usersResponseXML `xml:"GetUsersResponse"`
}

type usersResponseXML struct {
	Users []userXML `xml:"User"`
}

type userXML struct {
	Username  string `xml:"Username"`
	UserLevel string `xml:"UserLevel"`
}

// ParseUsers parses the local user accounts from a GetUsers response.
func ParseUsers(data []byte) ([]User, error) {
	var envelope usersEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	users := make([]User, 0, len(envelope.Body.Response.Users))
	for _, u := range envelope.Body.Response.Users {
		username := strings.TrimSpace(u.Username)
		if username == "" {
			continue
		}
		users = append(users, User{Username: username, UserLevel: strings.TrimSpace(u.UserLevel)})
	}
	return users, nil
}

// SystemDateTime is the camera's clock configuration (GetSystemDateAndTime + GetNTP).
type SystemDateTime struct {
	DateTimeType    string   `json:"dateTimeType"` // "Manual" | "NTP"
	DaylightSavings bool     `json:"daylightSavings"`
	TimeZone        string   `json:"timeZone"`    // POSIX TZ, e.g. "GMT+08:00"
	UTCDateTime     string   `json:"utcDateTime"` // RFC3339 UTC (when the camera reports it)
	NTPFromDHCP     bool     `json:"ntpFromDhcp"`
	NTPServers      []string `json:"ntpServers"` // NTP hosts/IPs (from GetNTP)
}

// Service is an ONVIF service the device advertises (GetServices).
type Service struct {
	Namespace string `json:"namespace"`
	XAddr     string `json:"xAddr"`
	Version   string `json:"version"` // "Major.Minor" from the service's <Version> (best-effort)
}

// NetworkInterface is one NIC's IPv4 config.
type NetworkInterface struct {
	Token        string `json:"token"`
	Name         string `json:"name"`
	MAC          string `json:"mac"`
	Enabled      bool   `json:"enabled"`
	DHCP         bool   `json:"dhcp"`
	IPAddress    string `json:"ipAddress"`
	PrefixLength int    `json:"prefixLength"`
}

// NetworkConfig is the camera's network configuration.
type NetworkConfig struct {
	Interfaces  []NetworkInterface `json:"interfaces"`
	Gateway     string             `json:"gateway"`
	DNS         []string           `json:"dns"`
	DNSFromDHCP bool               `json:"dnsFromDhcp"`
}

// ParseRebootMessage extracts the reboot Message (best-effort).
func ParseRebootMessage(data []byte) string {
	var env struct {
		Body struct {
			Response struct {
				Message string `xml:"Message"`
			} `xml:"SystemRebootResponse"`
		} `xml:"Body"`
	}
	_ = xml.Unmarshal(data, &env)
	return strings.TrimSpace(env.Body.Response.Message)
}

// ParseSystemDateTime parses a GetSystemDateAndTimeResponse.
func ParseSystemDateTime(data []byte) (*SystemDateTime, error) {
	var env struct {
		Body struct {
			Response struct {
				SDT struct {
					DateTimeType    string `xml:"DateTimeType"`
					DaylightSavings string `xml:"DaylightSavings"`
					TimeZone        struct {
						TZ string `xml:"TZ"`
					} `xml:"TimeZone"`
					UTCDateTime struct {
						Time struct {
							Hour   int `xml:"Hour"`
							Minute int `xml:"Minute"`
							Second int `xml:"Second"`
						} `xml:"Time"`
						Date struct {
							Year  int `xml:"Year"`
							Month int `xml:"Month"`
							Day   int `xml:"Day"`
						} `xml:"Date"`
					} `xml:"UTCDateTime"`
				} `xml:"SystemDateAndTime"`
			} `xml:"GetSystemDateAndTimeResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	sdt := env.Body.Response.SDT
	res := &SystemDateTime{
		DateTimeType:    strings.TrimSpace(sdt.DateTimeType),
		DaylightSavings: strings.EqualFold(strings.TrimSpace(sdt.DaylightSavings), "true"),
		TimeZone:        strings.TrimSpace(sdt.TimeZone.TZ),
	}
	d := sdt.UTCDateTime.Date
	tm := sdt.UTCDateTime.Time
	if d.Year > 0 {
		res.UTCDateTime = time.Date(d.Year, time.Month(d.Month), d.Day, tm.Hour, tm.Minute, tm.Second, 0, time.UTC).Format(time.RFC3339)
	}
	return res, nil
}

// ParseNetworkInterfaces parses a GetNetworkInterfacesResponse.
func ParseNetworkInterfaces(data []byte) (*NetworkConfig, error) {
	var env struct {
		Body struct {
			Response struct {
				Interfaces []struct {
					Token   string `xml:"token,attr"`
					Enabled string `xml:"Enabled"`
					Info    struct {
						Name      string `xml:"Name"`
						HwAddress string `xml:"HwAddress"`
					} `xml:"Info"`
					IPv4 struct {
						Config struct {
							Manual []struct {
								Address      string `xml:"Address"`
								PrefixLength int    `xml:"PrefixLength"`
							} `xml:"Manual"`
							DHCP     string `xml:"DHCP"`
							FromDHCP struct {
								Address      string `xml:"Address"`
								PrefixLength int    `xml:"PrefixLength"`
							} `xml:"FromDHCP"`
						} `xml:"Config"`
					} `xml:"IPv4"`
				} `xml:"NetworkInterfaces"`
			} `xml:"GetNetworkInterfacesResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	cfg := &NetworkConfig{}
	for _, ni := range env.Body.Response.Interfaces {
		iface := NetworkInterface{
			Token:   strings.TrimSpace(ni.Token),
			Name:    strings.TrimSpace(ni.Info.Name),
			MAC:     strings.TrimSpace(ni.Info.HwAddress),
			Enabled: strings.EqualFold(strings.TrimSpace(ni.Enabled), "true"),
			DHCP:    strings.EqualFold(strings.TrimSpace(ni.IPv4.Config.DHCP), "true"),
		}
		if iface.DHCP {
			iface.IPAddress = strings.TrimSpace(ni.IPv4.Config.FromDHCP.Address)
			iface.PrefixLength = ni.IPv4.Config.FromDHCP.PrefixLength
		} else if len(ni.IPv4.Config.Manual) > 0 {
			iface.IPAddress = strings.TrimSpace(ni.IPv4.Config.Manual[0].Address)
			iface.PrefixLength = ni.IPv4.Config.Manual[0].PrefixLength
		}
		cfg.Interfaces = append(cfg.Interfaces, iface)
	}
	return cfg, nil
}

// ParseNetworkGateway parses a GetNetworkDefaultGatewayResponse (IPv4).
func ParseNetworkGateway(data []byte) string {
	var env struct {
		Body struct {
			Response struct {
				Gateway struct {
					IPv4Address string `xml:"IPv4Address"`
				} `xml:"NetworkGateway"`
			} `xml:"GetNetworkDefaultGatewayResponse"`
		} `xml:"Body"`
	}
	_ = xml.Unmarshal(data, &env)
	return strings.TrimSpace(env.Body.Response.Gateway.IPv4Address)
}

// ParseDNS parses a GetDNSResponse; returns the IPv4 servers + whether DNS is from DHCP.
func ParseDNS(data []byte) ([]string, bool) {
	var env struct {
		Body struct {
			Response struct {
				Info struct {
					FromDHCP string `xml:"FromDHCP"`
					Manual   []struct {
						IPv4Address string `xml:"IPv4Address"`
					} `xml:"DNSManual"`
					FromDHCPSrv []struct {
						IPv4Address string `xml:"IPv4Address"`
					} `xml:"DNSFromDHCP"`
				} `xml:"DNSInformation"`
			} `xml:"GetDNSResponse"`
		} `xml:"Body"`
	}
	_ = xml.Unmarshal(data, &env)
	info := env.Body.Response.Info
	fromDHCP := strings.EqualFold(strings.TrimSpace(info.FromDHCP), "true")
	src := info.Manual
	if fromDHCP && len(info.FromDHCPSrv) > 0 {
		src = info.FromDHCPSrv
	}
	var servers []string
	for _, s := range src {
		if a := strings.TrimSpace(s.IPv4Address); a != "" {
			servers = append(servers, a)
		}
	}
	return servers, fromDHCP
}

// ParseNTP parses a GetNTPResponse; returns the NTP hosts/IPs + whether from DHCP.
func ParseNTP(data []byte) ([]string, bool) {
	type ntpEntry struct {
		IPv4Address string `xml:"IPv4Address"`
		DNSname     string `xml:"DNSname"`
	}
	var env struct {
		Body struct {
			Response struct {
				Info struct {
					FromDHCP    string     `xml:"FromDHCP"`
					Manual      []ntpEntry `xml:"NTPManual"`
					FromDHCPSrv []ntpEntry `xml:"NTPFromDHCP"`
				} `xml:"NTPInformation"`
			} `xml:"GetNTPResponse"`
		} `xml:"Body"`
	}
	_ = xml.Unmarshal(data, &env)
	info := env.Body.Response.Info
	fromDHCP := strings.EqualFold(strings.TrimSpace(info.FromDHCP), "true")
	src := info.Manual
	if fromDHCP && len(info.FromDHCPSrv) > 0 {
		src = info.FromDHCPSrv
	}
	var servers []string
	for _, s := range src {
		if v := strings.TrimSpace(firstNonEmptyStr(s.DNSname, s.IPv4Address)); v != "" {
			servers = append(servers, v)
		}
	}
	return servers, fromDHCP
}

// ParseServices parses the service namespaces a device advertises (GetServices).
func ParseServices(data []byte) ([]Service, error) {
	var env struct {
		Body struct {
			Response struct {
				Services []struct {
					Namespace string `xml:"Namespace"`
					XAddr     string `xml:"XAddr"`
					Version   struct {
						Major int `xml:"Major"`
						Minor int `xml:"Minor"`
					} `xml:"Version"`
				} `xml:"Service"`
			} `xml:"GetServicesResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	services := make([]Service, 0, len(env.Body.Response.Services))
	for _, s := range env.Body.Response.Services {
		ns := strings.TrimSpace(s.Namespace)
		if ns == "" {
			continue
		}
		ver := ""
		if s.Version.Major > 0 || s.Version.Minor > 0 {
			ver = fmt.Sprintf("%d.%02d", s.Version.Major, s.Version.Minor)
		}
		services = append(services, Service{Namespace: ns, XAddr: strings.TrimSpace(s.XAddr), Version: ver})
	}
	return services, nil
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
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

// ParseProfiles parses all ONVIF media profiles returned by GetProfiles.
func ParseProfiles(data []byte) ([]MediaProfile, error) {
	var envelope profilesEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	profiles := make([]MediaProfile, 0, len(envelope.Body.Response.Profiles))
	for _, profile := range envelope.Body.Response.Profiles {
		token := strings.TrimSpace(profile.Token)
		if token == "" {
			continue
		}
		profiles = append(profiles, MediaProfile{
			Token:    token,
			Name:     strings.TrimSpace(profile.Name),
			Encoding: strings.TrimSpace(profile.VideoEncoderConfiguration.Encoding),
			Width:    profile.VideoEncoderConfiguration.Resolution.Width,
			Height:   profile.VideoEncoderConfiguration.Resolution.Height,

			VideoSourceToken: strings.TrimSpace(profile.VideoSourceConfiguration.Token),
		})
	}
	return profiles, nil
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
