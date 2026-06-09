package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultDiscoveryAddress = "239.255.255.250:3702"
	defaultDiscoveryTimeout = 3 * time.Second
	discoveryEnrichTimeout  = 2 * time.Second
	discoveryEnrichWorkers  = 4
	defaultHTTPTimeout      = 5 * time.Second
)

var manualProbeFallbackPorts = []int{8899, 8080, 8000, 5000, 2020}

// Device is the normalized ONVIF device shape returned by discovery and probe calls.
type Device struct {
	Name            string   `json:"name"`
	Host            string   `json:"host"`
	Port            int      `json:"port"`
	XAddr           string   `json:"xAddr"`
	Types           []string `json:"types"`
	Scopes          []string `json:"scopes"`
	HardwareID      string   `json:"hardwareId"`
	Manufacturer    string   `json:"manufacturer"`
	Model           string   `json:"model"`
	FirmwareVersion string   `json:"firmwareVersion"`
	SerialNumber    string   `json:"serialNumber"`
	MediaXAddr      string   `json:"mediaXAddr"`
	PTZXAddr        string   `json:"ptzXAddr"`
	PTZSupported    bool     `json:"ptzSupported"`
	ProfileToken    string   `json:"profileToken"`
	RTSPURL         string   `json:"rtspUrl"`
	SnapshotURI     string   `json:"snapshotUri"`
	LastSeenAt      int64    `json:"lastSeenAt"`
}

// Credentials contains optional ONVIF device credentials.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// StreamURIRequest describes how to resolve a device's RTSP stream URI.
type StreamURIRequest struct {
	DeviceServiceURL string      `json:"deviceServiceUrl"`
	MediaServiceURL  string      `json:"mediaServiceUrl"`
	ProfileToken     string      `json:"profileToken"`
	Credentials      Credentials `json:"credentials"`
}

// StreamURIResult contains the resolved ONVIF media service, profile and RTSP URI.
type StreamURIResult struct {
	MediaXAddr   string `json:"mediaXAddr"`
	ProfileToken string `json:"profileToken"`
	RTSPURL      string `json:"rtspUrl"`
}

// SnapshotURIResult contains the resolved ONVIF media snapshot URI.
type SnapshotURIResult struct {
	MediaXAddr   string `json:"mediaXAddr"`
	ProfileToken string `json:"profileToken"`
	SnapshotURI  string `json:"snapshotUri"`
}

// CapabilitiesResult contains service URLs and high-level feature flags.
type CapabilitiesResult struct {
	MediaXAddr   string `json:"mediaXAddr"`
	PTZXAddr     string `json:"ptzXAddr"`
	PTZSupported bool   `json:"ptzSupported"`
}

// ChangeUserPasswordRequest updates an ONVIF local user password on the camera.
type ChangeUserPasswordRequest struct {
	DeviceServiceURL string      `json:"deviceServiceUrl"`
	Credentials      Credentials `json:"credentials"`
	TargetUsername   string      `json:"targetUsername"`
	NewPassword      string      `json:"newPassword"`
	UserLevel        string      `json:"userLevel"`
}

// PTZMoveRequest controls one PTZ movement.
type PTZMoveRequest struct {
	DeviceServiceURL string      `json:"deviceServiceUrl"`
	PTZServiceURL    string      `json:"ptzServiceUrl"`
	ProfileToken     string      `json:"profileToken"`
	Credentials      Credentials `json:"credentials"`
	Pan              float64     `json:"pan"`
	Tilt             float64     `json:"tilt"`
	Zoom             float64     `json:"zoom"`
	Duration         time.Duration
}

// Client performs lightweight ONVIF discovery and device-service probes.
type Client struct {
	DiscoveryAddress string
	DiscoveryTimeout time.Duration
	HTTPClient       *http.Client
}

// NewClient creates an ONVIF client with small-device friendly defaults.
func NewClient() *Client {
	return &Client{
		DiscoveryAddress: defaultDiscoveryAddress,
		DiscoveryTimeout: defaultDiscoveryTimeout,
		HTTPClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

// Discover sends a WS-Discovery Probe and returns normalized ProbeMatch devices.
func (c *Client) Discover(ctx context.Context, timeout time.Duration) ([]Device, error) {
	if timeout <= 0 {
		timeout = c.discoveryTimeout()
	}

	addr, err := net.ResolveUDPAddr("udp4", c.discoveryAddress())
	if err != nil {
		return nil, fmt.Errorf("resolve discovery address failed: %w", err)
	}

	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	devicesByAddr := make(map[string]Device)
	listenAddrs := discoveryListenAddrs()
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make(chan error, len(listenAddrs))

	for _, listenAddr := range listenAddrs {
		listenAddr := listenAddr
		conn, err := net.ListenUDP("udp4", listenAddr)
		if err != nil {
			if listenAddr.IP == nil {
				return nil, fmt.Errorf("open discovery socket failed: %w", err)
			}
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer conn.Close()

			devices, err := discoverOnConn(ctx, conn, addr, deadline)
			if err != nil {
				errs <- err
				return
			}

			mu.Lock()
			for _, device := range devices {
				if device.XAddr != "" {
					devicesByAddr[device.XAddr] = device
				}
			}
			mu.Unlock()
		}()
	}

	wg.Wait()
	close(errs)

	if len(devicesByAddr) == 0 {
		for err := range errs {
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
		}
	}

	return c.enrichDiscoveredDevices(ctx, mapDevices(devicesByAddr)), nil
}

func discoverOnConn(ctx context.Context, conn *net.UDPConn, addr *net.UDPAddr, deadline time.Time) ([]Device, error) {
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set discovery deadline failed: %w", err)
	}

	for _, probe := range probeEnvelopes() {
		if _, err := conn.WriteToUDP([]byte(probe), addr); err != nil {
			return nil, fmt.Errorf("send discovery probe failed: %w", err)
		}
	}

	devicesByAddr := make(map[string]Device)
	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				break
			}
			return nil, fmt.Errorf("read discovery response failed: %w", err)
		}

		matches, err := ParseProbeMatches(buf[:n])
		if err != nil {
			continue
		}
		for _, device := range matches {
			if device.XAddr == "" {
				continue
			}
			devicesByAddr[device.XAddr] = device
		}
	}

	return mapDevices(devicesByAddr), nil
}

func discoveryListenAddrs() []*net.UDPAddr {
	addrs := []*net.UDPAddr{{Port: 0}}

	ifaces, err := net.Interfaces()
	if err != nil {
		return addrs
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ifaceAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, ifaceAddr := range ifaceAddrs {
			var ip net.IP
			switch v := ifaceAddr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			ipv4 := ip.To4()
			if ipv4 == nil {
				continue
			}
			addrs = append(addrs, &net.UDPAddr{IP: ipv4, Port: 0})
		}
	}

	return addrs
}

// Probe checks one manual address or URL and enriches it through GetDeviceInformation when possible.
func (c *Client) Probe(ctx context.Context, address string) (*Device, error) {
	candidates, err := ProbeDeviceServiceURLs(address)
	if err != nil {
		return nil, err
	}

	var lastErr error
	var lastStatus int
	for _, xaddr := range candidates {
		device := DeviceFromXAddr(xaddr)
		info, statusCode, err := c.requestDeviceInformation(ctx, xaddr)
		if err != nil {
			lastErr = err
			continue
		}
		if statusCode >= 200 && statusCode < 300 {
			applyDeviceInformation(&device, info)
			device = c.enrichDevice(ctx, device, Credentials{})
			return &device, nil
		}
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == http.StatusMethodNotAllowed {
			return &device, nil
		}
		lastStatus = statusCode
		lastErr = fmt.Errorf("ONVIF device service returned status %d", statusCode)
	}

	if lastErr != nil {
		if len(candidates) > 1 {
			return nil, fmt.Errorf("probe ONVIF device failed after trying %d device-service URLs: %w", len(candidates), lastErr)
		}
		return nil, fmt.Errorf("probe ONVIF device failed: %w", lastErr)
	}

	return nil, fmt.Errorf("ONVIF device service returned status %d", lastStatus)
}

// GetStreamURI resolves a camera profile to an RTSP URI through ONVIF media services.
func (c *Client) GetStreamURI(ctx context.Context, req StreamURIRequest) (*StreamURIResult, error) {
	deviceURL, err := NormalizeDeviceServiceURL(req.DeviceServiceURL)
	if err != nil {
		return nil, err
	}

	mediaURL := strings.TrimSpace(req.MediaServiceURL)
	if mediaURL == "" {
		mediaURL, err = c.getMediaServiceURL(ctx, deviceURL, req.Credentials)
		if err != nil {
			return nil, err
		}
	}

	profileToken := strings.TrimSpace(req.ProfileToken)
	if profileToken == "" {
		profileToken, err = c.getPreferredProfileToken(ctx, mediaURL, req.Credentials)
		if err != nil {
			return nil, err
		}
	}

	rtspURL, err := c.getStreamURI(ctx, mediaURL, profileToken, req.Credentials)
	if err != nil {
		return nil, err
	}

	return &StreamURIResult{
		MediaXAddr:   mediaURL,
		ProfileToken: profileToken,
		RTSPURL:      rtspURL,
	}, nil
}

// GetSnapshotURI resolves a camera profile to a JPEG snapshot URI through ONVIF media services.
func (c *Client) GetSnapshotURI(ctx context.Context, req StreamURIRequest) (*SnapshotURIResult, error) {
	deviceURL, err := NormalizeDeviceServiceURL(req.DeviceServiceURL)
	if err != nil {
		return nil, err
	}

	mediaURL := strings.TrimSpace(req.MediaServiceURL)
	if mediaURL == "" {
		mediaURL, err = c.getMediaServiceURL(ctx, deviceURL, req.Credentials)
		if err != nil {
			return nil, err
		}
	}

	profileToken := strings.TrimSpace(req.ProfileToken)
	if profileToken == "" {
		profileToken, err = c.getPreferredProfileToken(ctx, mediaURL, req.Credentials)
		if err != nil {
			return nil, err
		}
	}

	snapshotURI, err := c.getSnapshotURI(ctx, mediaURL, profileToken, req.Credentials)
	if err != nil {
		return nil, err
	}

	return &SnapshotURIResult{
		MediaXAddr:   mediaURL,
		ProfileToken: profileToken,
		SnapshotURI:  snapshotURI,
	}, nil
}

// GetCapabilities resolves ONVIF service URLs and supported feature flags.
func (c *Client) GetCapabilities(ctx context.Context, deviceServiceURL string, credentials Credentials) (*CapabilitiesResult, error) {
	deviceURL, err := NormalizeDeviceServiceURL(deviceServiceURL)
	if err != nil {
		return nil, err
	}
	services, err := c.getServiceXAddrs(ctx, deviceURL, credentials)
	if err != nil {
		return nil, err
	}
	return &CapabilitiesResult{
		MediaXAddr:   services.MediaXAddr,
		PTZXAddr:     services.PTZXAddr,
		PTZSupported: strings.TrimSpace(services.PTZXAddr) != "",
	}, nil
}

// ChangeUserPassword updates a local ONVIF camera user's password through Device Management SetUser.
func (c *Client) ChangeUserPassword(ctx context.Context, req ChangeUserPasswordRequest) error {
	deviceURL, err := NormalizeDeviceServiceURL(req.DeviceServiceURL)
	if err != nil {
		return err
	}
	username := strings.TrimSpace(req.TargetUsername)
	password := strings.TrimSpace(req.NewPassword)
	if username == "" {
		return errors.New("targetUsername is required")
	}
	if password == "" {
		return errors.New("newPassword is required")
	}
	userLevel := strings.TrimSpace(req.UserLevel)
	if userLevel == "" {
		userLevel = "Administrator"
	}
	if _, _, err := c.postSOAP(ctx, deviceURL, setUserBody(username, password, userLevel), req.Credentials); err != nil {
		return fmt.Errorf("change ONVIF camera password failed: %w", err)
	}
	return nil
}

// PTZMove starts a continuous PTZ movement and optionally stops it after Duration.
func (c *Client) PTZMove(ctx context.Context, req PTZMoveRequest) error {
	if strings.TrimSpace(req.ProfileToken) == "" {
		return errors.New("profileToken is required")
	}
	ptzURL, err := c.resolvePTZServiceURL(ctx, req.DeviceServiceURL, req.PTZServiceURL, req.Credentials)
	if err != nil {
		return err
	}
	if _, _, err := c.postSOAP(ctx, ptzURL, continuousMoveBody(req.ProfileToken, req.Pan, req.Tilt, req.Zoom), req.Credentials); err != nil {
		return fmt.Errorf("ONVIF PTZ move failed: %w", err)
	}
	if req.Duration > 0 {
		timer := time.NewTimer(req.Duration)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		return c.PTZStop(ctx, req)
	}
	return nil
}

// PTZStop stops pan/tilt and zoom movement for a profile.
func (c *Client) PTZStop(ctx context.Context, req PTZMoveRequest) error {
	if strings.TrimSpace(req.ProfileToken) == "" {
		return errors.New("profileToken is required")
	}
	ptzURL, err := c.resolvePTZServiceURL(ctx, req.DeviceServiceURL, req.PTZServiceURL, req.Credentials)
	if err != nil {
		return err
	}
	if _, _, err := c.postSOAP(ctx, ptzURL, stopPTZBody(req.ProfileToken), req.Credentials); err != nil {
		return fmt.Errorf("ONVIF PTZ stop failed: %w", err)
	}
	return nil
}

// NormalizeDeviceServiceURL accepts a host/IP or URL and returns a device-service URL.
func NormalizeDeviceServiceURL(address string) (string, error) {
	value := strings.TrimSpace(address)
	if value == "" {
		return "", errors.New("address is required")
	}

	if !strings.Contains(value, "://") {
		value = "http://" + value
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("invalid ONVIF address %q", address)
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/onvif/device_service"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}

// ProbeDeviceServiceURLs returns candidate ONVIF device-service URLs for a manual probe.
func ProbeDeviceServiceURLs(address string) ([]string, error) {
	primary, err := NormalizeDeviceServiceURL(address)
	if err != nil {
		return nil, err
	}

	candidates := []string{primary}
	if !shouldTryManualProbeFallbacks(address, primary) {
		return candidates, nil
	}

	parsed, err := url.Parse(primary)
	if err != nil {
		return candidates, nil
	}
	for _, port := range manualProbeFallbackPorts {
		next := *parsed
		next.Host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(port))
		candidates = appendUniqueString(candidates, next.String())
	}
	return candidates, nil
}

func shouldTryManualProbeFallbacks(address string, normalized string) bool {
	raw := strings.TrimSpace(address)
	if raw == "" {
		return false
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(normalized)
		if err != nil {
			return false
		}
		return parsed.Port() == "" && (parsed.Path == "" || parsed.Path == "/" || parsed.Path == "/onvif/device_service")
	}
	if strings.ContainsAny(raw, "/?#") {
		return false
	}
	hostPortValue := raw
	if strings.Count(raw, ":") == 1 {
		if _, _, err := net.SplitHostPort(hostPortValue); err == nil {
			return false
		}
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return false
	}
	return parsed.Port() == ""
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// DeviceFromXAddr creates a normalized device shell from a service XAddr.
func DeviceFromXAddr(xaddr string) Device {
	device := Device{
		XAddr:      strings.TrimSpace(xaddr),
		LastSeenAt: time.Now().UTC().Unix(),
	}
	parsed, err := url.Parse(device.XAddr)
	if err != nil {
		return device
	}
	device.Host = parsed.Hostname()
	if port := parsed.Port(); port != "" {
		if parsedPort, err := strconv.Atoi(port); err == nil {
			device.Port = parsedPort
		}
	}
	if device.Port == 0 {
		if parsed.Scheme == "https" {
			device.Port = 443
		} else {
			device.Port = 80
		}
	}
	device.Name = device.Host
	return device
}

func (c *Client) discoveryAddress() string {
	if strings.TrimSpace(c.DiscoveryAddress) == "" {
		return defaultDiscoveryAddress
	}
	return strings.TrimSpace(c.DiscoveryAddress)
}

func (c *Client) discoveryTimeout() time.Duration {
	if c.DiscoveryTimeout <= 0 {
		return defaultDiscoveryTimeout
	}
	return c.DiscoveryTimeout
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

func (c *Client) postSOAP(ctx context.Context, endpoint string, body string, credentials Credentials) ([]byte, int, error) {
	envelope, err := soapEnvelope(body, credentials)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(envelope))
	if err != nil {
		return nil, 0, fmt.Errorf("build ONVIF SOAP request failed: %w", err)
	}
	req.Header.Set("Content-Type", `application/soap+xml; charset="utf-8"`)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("send ONVIF SOAP request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if readErr != nil {
		return nil, resp.StatusCode, fmt.Errorf("read ONVIF SOAP response failed: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseBody, resp.StatusCode, fmt.Errorf("ONVIF SOAP endpoint returned status %d", resp.StatusCode)
	}
	return responseBody, resp.StatusCode, nil
}

func (c *Client) requestDeviceInformation(ctx context.Context, endpoint string) (DeviceInformation, int, error) {
	body := []byte(deviceInformationEnvelope())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return DeviceInformation{}, 0, fmt.Errorf("build ONVIF device information request failed: %w", err)
	}
	req.Header.Set("Content-Type", `application/soap+xml; charset="utf-8"`)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return DeviceInformation{}, 0, err
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if readErr != nil {
		return DeviceInformation{}, resp.StatusCode, fmt.Errorf("read ONVIF device information response failed: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DeviceInformation{}, resp.StatusCode, nil
	}
	info, err := ParseDeviceInformation(responseBody)
	if err != nil {
		return DeviceInformation{}, resp.StatusCode, nil
	}
	return info, resp.StatusCode, nil
}

func (c *Client) enrichDiscoveredDevices(ctx context.Context, devices []Device) []Device {
	if len(devices) == 0 {
		return devices
	}

	result := make([]Device, len(devices))
	copy(result, devices)
	workers := discoveryEnrichWorkers
	if len(devices) < workers {
		workers = len(devices)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				enrichCtx, cancel := context.WithTimeout(ctx, discoveryEnrichTimeout)
				result[idx] = c.enrichDevice(enrichCtx, result[idx], Credentials{})
				cancel()
			}
		}()
	}

	for idx := range result {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return result
		case jobs <- idx:
		}
	}
	close(jobs)
	wg.Wait()
	return result
}

func (c *Client) enrichDevice(ctx context.Context, device Device, credentials Credentials) Device {
	if strings.TrimSpace(device.XAddr) == "" {
		return device
	}
	if info, statusCode, err := c.requestDeviceInformation(ctx, device.XAddr); err == nil && statusCode >= 200 && statusCode < 300 {
		applyDeviceInformation(&device, info)
	}

	if capabilities, err := c.GetCapabilities(ctx, device.XAddr, credentials); err == nil {
		device.MediaXAddr = firstNonEmpty(capabilities.MediaXAddr, device.MediaXAddr)
		device.PTZXAddr = firstNonEmpty(capabilities.PTZXAddr, device.PTZXAddr)
		device.PTZSupported = capabilities.PTZSupported
	}
	if stream, err := c.GetStreamURI(ctx, StreamURIRequest{
		DeviceServiceURL: device.XAddr,
		MediaServiceURL:  device.MediaXAddr,
		ProfileToken:     device.ProfileToken,
		Credentials:      credentials,
	}); err == nil {
		device.MediaXAddr = firstNonEmpty(stream.MediaXAddr, device.MediaXAddr)
		device.ProfileToken = firstNonEmpty(stream.ProfileToken, device.ProfileToken)
		device.RTSPURL = stream.RTSPURL
	}
	if snapshot, err := c.GetSnapshotURI(ctx, StreamURIRequest{
		DeviceServiceURL: device.XAddr,
		MediaServiceURL:  device.MediaXAddr,
		ProfileToken:     device.ProfileToken,
		Credentials:      credentials,
	}); err == nil {
		device.MediaXAddr = firstNonEmpty(snapshot.MediaXAddr, device.MediaXAddr)
		device.ProfileToken = firstNonEmpty(snapshot.ProfileToken, device.ProfileToken)
		device.SnapshotURI = snapshot.SnapshotURI
	}
	return device
}

func applyDeviceInformation(device *Device, info DeviceInformation) {
	if device == nil {
		return
	}
	device.Manufacturer = firstNonEmpty(info.Manufacturer, device.Manufacturer)
	device.Model = firstNonEmpty(info.Model, device.Model)
	device.FirmwareVersion = firstNonEmpty(info.FirmwareVersion, device.FirmwareVersion)
	device.SerialNumber = firstNonEmpty(info.SerialNumber, device.SerialNumber)
	device.HardwareID = firstNonEmpty(info.HardwareID, device.HardwareID)
	device.Name = firstNonEmpty(info.Model, info.Manufacturer, device.Name, device.Host)
}

func (c *Client) getMediaServiceURL(ctx context.Context, deviceURL string, credentials Credentials) (string, error) {
	services, err := c.getServiceXAddrs(ctx, deviceURL, credentials)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(services.MediaXAddr) == "" {
		return "", errors.New("ONVIF media service URL not found")
	}
	return services.MediaXAddr, nil
}

func (c *Client) getServiceXAddrs(ctx context.Context, deviceURL string, credentials Credentials) (ServiceXAddrs, error) {
	services, allErr := c.getServiceXAddrsForCategory(ctx, deviceURL, "All", credentials)
	if allErr == nil && strings.TrimSpace(services.MediaXAddr) != "" {
		return services, nil
	}

	mediaServices, mediaErr := c.getServiceXAddrsForCategory(ctx, deviceURL, "Media", credentials)
	if mediaErr == nil {
		services.MediaXAddr = firstNonEmpty(services.MediaXAddr, mediaServices.MediaXAddr)
	}

	ptzServices, ptzErr := c.getServiceXAddrsForCategory(ctx, deviceURL, "PTZ", credentials)
	if ptzErr == nil {
		services.PTZXAddr = firstNonEmpty(services.PTZXAddr, ptzServices.PTZXAddr)
	}

	if strings.TrimSpace(services.MediaXAddr) != "" {
		return services, nil
	}
	if allErr != nil && mediaErr != nil {
		return ServiceXAddrs{}, fmt.Errorf("get ONVIF capabilities failed: all: %v; media: %v", allErr, mediaErr)
	}
	if allErr != nil {
		return ServiceXAddrs{}, fmt.Errorf("get ONVIF capabilities failed: %w", allErr)
	}
	return ServiceXAddrs{}, errors.New("ONVIF media service URL not found")
}

func (c *Client) getServiceXAddrsForCategory(ctx context.Context, deviceURL string, category string, credentials Credentials) (ServiceXAddrs, error) {
	body, _, err := c.postSOAP(ctx, deviceURL, getCapabilitiesBody(category), credentials)
	if err != nil {
		return ServiceXAddrs{}, err
	}
	return ParseServiceXAddrs(body)
}

func (c *Client) resolvePTZServiceURL(ctx context.Context, deviceServiceURL string, ptzServiceURL string, credentials Credentials) (string, error) {
	ptzURL := strings.TrimSpace(ptzServiceURL)
	if ptzURL != "" {
		return ptzURL, nil
	}
	deviceURL, err := NormalizeDeviceServiceURL(deviceServiceURL)
	if err != nil {
		return "", err
	}
	services, err := c.getServiceXAddrs(ctx, deviceURL, credentials)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(services.PTZXAddr) == "" {
		return "", errors.New("ONVIF PTZ service URL not found")
	}
	return services.PTZXAddr, nil
}

func (c *Client) getPreferredProfileToken(ctx context.Context, mediaURL string, credentials Credentials) (string, error) {
	body, _, err := c.postSOAP(ctx, mediaURL, getProfilesBody(), credentials)
	if err != nil {
		return "", fmt.Errorf("get ONVIF media profiles failed: %w", err)
	}
	token, err := ParsePreferredProfileToken(body)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", errors.New("ONVIF media profile token not found")
	}
	return token, nil
}

func (c *Client) getStreamURI(ctx context.Context, mediaURL string, profileToken string, credentials Credentials) (string, error) {
	body, _, err := c.postSOAP(ctx, mediaURL, getStreamURIBody(profileToken), credentials)
	if err != nil {
		return "", fmt.Errorf("get ONVIF stream URI failed: %w", err)
	}
	streamURL, err := ParseStreamURI(body)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(streamURL) == "" {
		return "", errors.New("ONVIF stream URI not found")
	}
	return streamURL, nil
}

func (c *Client) getSnapshotURI(ctx context.Context, mediaURL string, profileToken string, credentials Credentials) (string, error) {
	body, _, err := c.postSOAP(ctx, mediaURL, getSnapshotURIBody(profileToken), credentials)
	if err != nil {
		return "", fmt.Errorf("get ONVIF snapshot URI failed: %w", err)
	}
	snapshotURI, err := ParseSnapshotURI(body)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(snapshotURI) == "" {
		return "", errors.New("ONVIF snapshot URI not found")
	}
	return snapshotURI, nil
}

func mapDevices(devicesByAddr map[string]Device) []Device {
	devices := make([]Device, 0, len(devicesByAddr))
	for _, device := range devicesByAddr {
		devices = append(devices, device)
	}
	return devices
}

func probeEnvelopes() []string {
	messageID := "uuid:" + uuid.NewString()
	return []string{
		probeEnvelope(messageID, "dn:NetworkVideoTransmitter"),
		probeEnvelope("uuid:"+uuid.NewString(), ""),
	}
}

func probeEnvelope(messageID string, types string) string {
	typeBlock := ""
	if strings.TrimSpace(types) != "" {
		typeBlock = fmt.Sprintf(`
      <d:Types>%s</d:Types>`, types)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<e:Envelope xmlns:e="http://www.w3.org/2003/05/soap-envelope" xmlns:w="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">
  <e:Header>
    <w:MessageID>%s</w:MessageID>
    <w:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</w:To>
    <w:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</w:Action>
    <w:ReplyTo>
      <w:Address>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</w:Address>
    </w:ReplyTo>
  </e:Header>
  <e:Body>
    <d:Probe>%s
    </d:Probe>
  </e:Body>
</e:Envelope>`, messageID, typeBlock)
}

func deviceInformationEnvelope() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
    <tds:GetDeviceInformation/>
  </s:Body>
</s:Envelope>`
}

func soapEnvelope(body string, credentials Credentials) (string, error) {
	header, err := securityHeader(credentials)
	if err != nil {
		return "", err
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">` + header + `
  <s:Body>` + body + `
  </s:Body>
</s:Envelope>`, nil
}

func securityHeader(credentials Credentials) (string, error) {
	username := strings.TrimSpace(credentials.Username)
	if username == "" && credentials.Password == "" {
		return "", nil
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate ONVIF security nonce failed: %w", err)
	}
	created := time.Now().UTC().Format(time.RFC3339)
	sum := sha1.Sum(append(append(nonce, []byte(created)...), []byte(credentials.Password)...))
	return fmt.Sprintf(`
  <s:Header>
    <wsse:Security s:mustUnderstand="1" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">
      <wsse:UsernameToken>
        <wsse:Username>%s</wsse:Username>
        <wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password>
        <wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</wsse:Nonce>
        <wsu:Created>%s</wsu:Created>
      </wsse:UsernameToken>
    </wsse:Security>
  </s:Header>`, xmlEscape(username), base64.StdEncoding.EncodeToString(sum[:]), base64.StdEncoding.EncodeToString(nonce), xmlEscape(created)), nil
}

func getCapabilitiesBody(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		category = "Media"
	}
	return fmt.Sprintf(`
    <tds:GetCapabilities>
      <tds:Category>%s</tds:Category>
    </tds:GetCapabilities>`, xmlEscape(category))
}

func setUserBody(username string, password string, userLevel string) string {
	return fmt.Sprintf(`
    <tds:SetUser>
      <tds:User>
        <tt:Username>%s</tt:Username>
        <tt:Password>%s</tt:Password>
        <tt:UserLevel>%s</tt:UserLevel>
      </tds:User>
    </tds:SetUser>`, xmlEscape(username), xmlEscape(password), xmlEscape(userLevel))
}

func getProfilesBody() string {
	return `
    <trt:GetProfiles/>`
}

func getStreamURIBody(profileToken string) string {
	return fmt.Sprintf(`
    <trt:GetStreamUri>
      <trt:StreamSetup>
        <tt:Stream>RTP-Unicast</tt:Stream>
        <tt:Transport>
          <tt:Protocol>RTSP</tt:Protocol>
        </tt:Transport>
      </trt:StreamSetup>
      <trt:ProfileToken>%s</trt:ProfileToken>
    </trt:GetStreamUri>`, xmlEscape(profileToken))
}

func getSnapshotURIBody(profileToken string) string {
	return fmt.Sprintf(`
    <trt:GetSnapshotUri>
      <trt:ProfileToken>%s</trt:ProfileToken>
    </trt:GetSnapshotUri>`, xmlEscape(profileToken))
}

func continuousMoveBody(profileToken string, pan float64, tilt float64, zoom float64) string {
	velocity := ""
	if pan != 0 || tilt != 0 {
		velocity += fmt.Sprintf(`
        <tt:PanTilt x="%s" y="%s"/>`, ptzFloat(pan), ptzFloat(tilt))
	}
	if zoom != 0 {
		velocity += fmt.Sprintf(`
        <tt:Zoom x="%s"/>`, ptzFloat(zoom))
	}
	if velocity == "" {
		velocity = `
        <tt:PanTilt x="0" y="0"/>`
	}
	return fmt.Sprintf(`
    <tptz:ContinuousMove>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:Velocity>%s
      </tptz:Velocity>
    </tptz:ContinuousMove>`, xmlEscape(profileToken), velocity)
}

func stopPTZBody(profileToken string) string {
	return fmt.Sprintf(`
    <tptz:Stop>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:PanTilt>true</tptz:PanTilt>
      <tptz:Zoom>true</tptz:Zoom>
    </tptz:Stop>`, xmlEscape(profileToken))
}

func ptzFloat(value float64) string {
	if value > 1 {
		value = 1
	}
	if value < -1 {
		value = -1
	}
	return strconv.FormatFloat(value, 'f', 3, 64)
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}
