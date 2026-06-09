package onvif

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestParseProbeMatches(t *testing.T) {
	data := []byte(`<?xml version="1.0"?>
<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
  <SOAP-ENV:Body>
    <d:ProbeMatches>
      <d:ProbeMatch>
        <d:Types>dn:NetworkVideoTransmitter</d:Types>
        <d:Scopes>onvif://www.onvif.org/name/Front%20Gate onvif://www.onvif.org/hardware/HW123</d:Scopes>
        <d:XAddrs>http://192.168.1.40:8899/onvif/device_service</d:XAddrs>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </SOAP-ENV:Body>
</SOAP-ENV:Envelope>`)

	devices, err := ParseProbeMatches(data)
	if err != nil {
		t.Fatalf("ParseProbeMatches() error = %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("device count = %d", len(devices))
	}
	device := devices[0]
	if device.XAddr != "http://192.168.1.40:8899/onvif/device_service" {
		t.Fatalf("XAddr = %q", device.XAddr)
	}
	if device.Host != "192.168.1.40" || device.Port != 8899 {
		t.Fatalf("host/port = %s/%d", device.Host, device.Port)
	}
	if device.Name != "Front Gate" {
		t.Fatalf("Name = %q", device.Name)
	}
	if device.HardwareID != "HW123" {
		t.Fatalf("HardwareID = %q", device.HardwareID)
	}
}

func TestParseDeviceInformation(t *testing.T) {
	data := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
      <tds:Manufacturer>Acme</tds:Manufacturer>
      <tds:Model>Cam One</tds:Model>
      <tds:FirmwareVersion>1.2.3</tds:FirmwareVersion>
      <tds:SerialNumber>SN123</tds:SerialNumber>
      <tds:HardwareId>HW123</tds:HardwareId>
    </tds:GetDeviceInformationResponse>
  </s:Body>
</s:Envelope>`)

	info, err := ParseDeviceInformation(data)
	if err != nil {
		t.Fatalf("ParseDeviceInformation() error = %v", err)
	}
	if info.Manufacturer != "Acme" || info.Model != "Cam One" || info.HardwareID != "HW123" {
		t.Fatalf("info = %+v", info)
	}
}

func TestNormalizeDeviceServiceURL(t *testing.T) {
	got, err := NormalizeDeviceServiceURL("192.168.1.10")
	if err != nil {
		t.Fatalf("NormalizeDeviceServiceURL() error = %v", err)
	}
	want := "http://192.168.1.10/onvif/device_service"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestParseMediaStreamResponses(t *testing.T) {
	capabilities := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tds:GetCapabilitiesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
      <tds:Capabilities>
        <tt:Media xmlns:tt="http://www.onvif.org/ver10/schema">
          <tt:XAddr>http://192.168.1.40/onvif/media_service</tt:XAddr>
        </tt:Media>
        <tt:PTZ xmlns:tt="http://www.onvif.org/ver10/schema">
          <tt:XAddr>http://192.168.1.40/onvif/ptz_service</tt:XAddr>
        </tt:PTZ>
      </tds:Capabilities>
    </tds:GetCapabilitiesResponse>
  </s:Body>
</s:Envelope>`)
	mediaURL, err := ParseMediaXAddr(capabilities)
	if err != nil {
		t.Fatalf("ParseMediaXAddr() error = %v", err)
	}
	if mediaURL != "http://192.168.1.40/onvif/media_service" {
		t.Fatalf("mediaURL = %q", mediaURL)
	}
	services, err := ParseServiceXAddrs(capabilities)
	if err != nil {
		t.Fatalf("ParseServiceXAddrs() error = %v", err)
	}
	if services.PTZXAddr != "http://192.168.1.40/onvif/ptz_service" {
		t.Fatalf("ptzURL = %q", services.PTZXAddr)
	}

	profiles := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
      <trt:Profiles token="profile_1"/>
    </trt:GetProfilesResponse>
  </s:Body>
</s:Envelope>`)
	token, err := ParseFirstProfileToken(profiles)
	if err != nil {
		t.Fatalf("ParseFirstProfileToken() error = %v", err)
	}
	if token != "profile_1" {
		t.Fatalf("token = %q", token)
	}

	streamURI := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetStreamUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
      <trt:MediaUri>
        <tt:Uri xmlns:tt="http://www.onvif.org/ver10/schema">rtsp://192.168.1.40/Streaming/Channels/101</tt:Uri>
      </trt:MediaUri>
    </trt:GetStreamUriResponse>
  </s:Body>
</s:Envelope>`)
	uri, err := ParseStreamURI(streamURI)
	if err != nil {
		t.Fatalf("ParseStreamURI() error = %v", err)
	}
	if uri != "rtsp://192.168.1.40/Streaming/Channels/101" {
		t.Fatalf("uri = %q", uri)
	}

	snapshotURI := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetSnapshotUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
      <trt:MediaUri>
        <tt:Uri xmlns:tt="http://www.onvif.org/ver10/schema">http://192.168.1.40/onvif/snapshot.jpg</tt:Uri>
      </trt:MediaUri>
    </trt:GetSnapshotUriResponse>
  </s:Body>
</s:Envelope>`)
	snapshot, err := ParseSnapshotURI(snapshotURI)
	if err != nil {
		t.Fatalf("ParseSnapshotURI() error = %v", err)
	}
	if snapshot != "http://192.168.1.40/onvif/snapshot.jpg" {
		t.Fatalf("snapshot = %q", snapshot)
	}
}

func TestSetUserBodyEscapesPayload(t *testing.T) {
	body := setUserBody(`admin&root`, `p<ass>"'`, "Administrator")
	if !strings.Contains(body, "admin&amp;root") {
		t.Fatalf("username not escaped in body: %s", body)
	}
	if !strings.Contains(body, "p&lt;ass&gt;&quot;&apos;") {
		t.Fatalf("password not escaped in body: %s", body)
	}
}

func TestGetCapabilitiesFallsBackWhenAllCategoryIsRejected(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		switch {
		case strings.Contains(string(body), "<tds:Category>All</tds:Category>"):
			http.Error(w, "unsupported category", http.StatusBadRequest)
		case strings.Contains(string(body), "<tds:Category>Media</tds:Category>"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tds:GetCapabilitiesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
      <tds:Capabilities>
        <tt:Media xmlns:tt="http://www.onvif.org/ver10/schema">
          <tt:XAddr>` + serverURL + `/media</tt:XAddr>
        </tt:Media>
      </tds:Capabilities>
    </tds:GetCapabilitiesResponse>
  </s:Body>
</s:Envelope>`))
		case strings.Contains(string(body), "<tds:Category>PTZ</tds:Category>"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tds:GetCapabilitiesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
      <tds:Capabilities>
        <tt:PTZ xmlns:tt="http://www.onvif.org/ver10/schema">
          <tt:XAddr>` + serverURL + `/ptz</tt:XAddr>
        </tt:PTZ>
      </tds:Capabilities>
    </tds:GetCapabilitiesResponse>
  </s:Body>
</s:Envelope>`))
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client := &Client{HTTPClient: server.Client()}
	capabilities, err := client.GetCapabilities(context.Background(), server.URL, Credentials{})
	if err != nil {
		t.Fatalf("GetCapabilities() error = %v", err)
	}
	if capabilities.MediaXAddr != server.URL+"/media" {
		t.Fatalf("MediaXAddr = %q", capabilities.MediaXAddr)
	}
	if capabilities.PTZXAddr != server.URL+"/ptz" || !capabilities.PTZSupported {
		t.Fatalf("PTZ capability = %+v", capabilities)
	}
}

func TestEnrichDiscoveredDevicesAddsMetadataAndStreamHints(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		payload := string(body)
		switch {
		case strings.Contains(payload, "GetDeviceInformation"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
      <tds:Manufacturer>Acme</tds:Manufacturer>
      <tds:Model>Cam One</tds:Model>
      <tds:FirmwareVersion>1.2.3</tds:FirmwareVersion>
      <tds:SerialNumber>SN123</tds:SerialNumber>
      <tds:HardwareId>HW123</tds:HardwareId>
    </tds:GetDeviceInformationResponse>
  </s:Body>
</s:Envelope>`))
		case strings.Contains(payload, "GetCapabilities"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tds:GetCapabilitiesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
      <tds:Capabilities>
        <tt:Media xmlns:tt="http://www.onvif.org/ver10/schema">
          <tt:XAddr>` + serverURL + `/media</tt:XAddr>
        </tt:Media>
        <tt:PTZ xmlns:tt="http://www.onvif.org/ver10/schema">
          <tt:XAddr>` + serverURL + `/ptz</tt:XAddr>
        </tt:PTZ>
      </tds:Capabilities>
    </tds:GetCapabilitiesResponse>
  </s:Body>
</s:Envelope>`))
		case strings.Contains(payload, "GetProfiles"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
      <trt:Profiles token="sub">
        <tt:VideoEncoderConfiguration>
          <tt:Encoding>H264</tt:Encoding>
          <tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution>
        </tt:VideoEncoderConfiguration>
      </trt:Profiles>
    </trt:GetProfilesResponse>
  </s:Body>
</s:Envelope>`))
		case strings.Contains(payload, "GetStreamUri"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetStreamUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
      <trt:MediaUri><tt:Uri xmlns:tt="http://www.onvif.org/ver10/schema">rtsp://192.168.1.40/live</tt:Uri></trt:MediaUri>
    </trt:GetStreamUriResponse>
  </s:Body>
</s:Envelope>`))
		case strings.Contains(payload, "GetSnapshotUri"):
			_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetSnapshotUriResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
      <trt:MediaUri><tt:Uri xmlns:tt="http://www.onvif.org/ver10/schema">` + serverURL + `/snapshot.jpg</tt:Uri></trt:MediaUri>
    </trt:GetSnapshotUriResponse>
  </s:Body>
</s:Envelope>`))
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	device := DeviceFromXAddr(server.URL)
	device.Name = "Scope Name"
	client := &Client{HTTPClient: server.Client()}
	devices := client.enrichDiscoveredDevices(context.Background(), []Device{device})
	if len(devices) != 1 {
		t.Fatalf("device count = %d", len(devices))
	}
	got := devices[0]
	if got.Model != "Cam One" || got.Manufacturer != "Acme" || got.Name != "Cam One" {
		t.Fatalf("metadata = %+v", got)
	}
	if got.MediaXAddr != server.URL+"/media" || got.PTZXAddr != server.URL+"/ptz" || !got.PTZSupported {
		t.Fatalf("capabilities = %+v", got)
	}
	if got.ProfileToken != "sub" || got.RTSPURL != "rtsp://192.168.1.40/live" || got.SnapshotURI != server.URL+"/snapshot.jpg" {
		t.Fatalf("stream fields = %+v", got)
	}
}

func TestParsePreferredProfileTokenChoosesLowerResolutionProfile(t *testing.T) {
	profiles := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
      <trt:Profiles token="main">
        <tt:Name>MainStream</tt:Name>
        <tt:VideoEncoderConfiguration>
          <tt:Encoding>H265</tt:Encoding>
          <tt:Resolution>
            <tt:Width>3840</tt:Width>
            <tt:Height>2160</tt:Height>
          </tt:Resolution>
        </tt:VideoEncoderConfiguration>
      </trt:Profiles>
      <trt:Profiles token="sub">
        <tt:Name>SubStream</tt:Name>
        <tt:VideoEncoderConfiguration>
          <tt:Encoding>H264</tt:Encoding>
          <tt:Resolution>
            <tt:Width>640</tt:Width>
            <tt:Height>360</tt:Height>
          </tt:Resolution>
        </tt:VideoEncoderConfiguration>
      </trt:Profiles>
    </trt:GetProfilesResponse>
  </s:Body>
</s:Envelope>`)

	token, err := ParsePreferredProfileToken(profiles)
	if err != nil {
		t.Fatalf("ParsePreferredProfileToken() error = %v", err)
	}
	if token != "sub" {
		t.Fatalf("token = %q, want sub", token)
	}
}

func TestParsePreferredProfileTokenPrefersH264OverMJPEG(t *testing.T) {
	profiles := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
      <trt:Profiles token="mjpeg">
        <tt:Name>MobileMJPEG</tt:Name>
        <tt:VideoEncoderConfiguration>
          <tt:Encoding>M-JPEG</tt:Encoding>
          <tt:Resolution>
            <tt:Width>352</tt:Width>
            <tt:Height>288</tt:Height>
          </tt:Resolution>
        </tt:VideoEncoderConfiguration>
      </trt:Profiles>
      <trt:Profiles token="h264">
        <tt:Name>SubStream</tt:Name>
        <tt:VideoEncoderConfiguration>
          <tt:Encoding>H264</tt:Encoding>
          <tt:Resolution>
            <tt:Width>640</tt:Width>
            <tt:Height>360</tt:Height>
          </tt:Resolution>
        </tt:VideoEncoderConfiguration>
      </trt:Profiles>
    </trt:GetProfilesResponse>
  </s:Body>
</s:Envelope>`)

	token, err := ParsePreferredProfileToken(profiles)
	if err != nil {
		t.Fatalf("ParsePreferredProfileToken() error = %v", err)
	}
	if token != "h264" {
		t.Fatalf("token = %q, want h264", token)
	}
}

func TestProbeDeviceServiceURLsAddsCommonPortsForBareHost(t *testing.T) {
	urls, err := ProbeDeviceServiceURLs("192.168.0.85")
	if err != nil {
		t.Fatalf("ProbeDeviceServiceURLs() error = %v", err)
	}
	want := []string{
		"http://192.168.0.85/onvif/device_service",
		"http://192.168.0.85:8899/onvif/device_service",
		"http://192.168.0.85:8080/onvif/device_service",
		"http://192.168.0.85:8000/onvif/device_service",
		"http://192.168.0.85:5000/onvif/device_service",
		"http://192.168.0.85:2020/onvif/device_service",
	}
	if len(urls) != len(want) {
		t.Fatalf("url count = %d, want %d: %#v", len(urls), len(want), urls)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("urls[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}

func TestProbeDoesNotAddFallbacksForExplicitHostPort(t *testing.T) {
	urls, err := ProbeDeviceServiceURLs("192.168.0.85:8899")
	if err != nil {
		t.Fatalf("ProbeDeviceServiceURLs() error = %v", err)
	}
	if len(urls) != 1 || urls[0] != "http://192.168.0.85:8899/onvif/device_service" {
		t.Fatalf("urls = %#v", urls)
	}
}

func TestProbeTriesFallbackPortForBareHost(t *testing.T) {
	client := &Client{
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "192.168.0.85:8899" {
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("not here")),
						Request:    req,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
      <tds:Manufacturer>Acme</tds:Manufacturer>
      <tds:Model>Fallback Cam</tds:Model>
      <tds:FirmwareVersion>1.2.3</tds:FirmwareVersion>
      <tds:SerialNumber>SN85</tds:SerialNumber>
      <tds:HardwareId>HW85</tds:HardwareId>
    </tds:GetDeviceInformationResponse>
  </s:Body>
</s:Envelope>`)),
					Request: req,
				}, nil
			}),
		},
	}

	device, err := client.Probe(context.Background(), "192.168.0.85")
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if device.XAddr != "http://192.168.0.85:8899/onvif/device_service" {
		t.Fatalf("XAddr = %q", device.XAddr)
	}
	if device.Model != "Fallback Cam" || device.SerialNumber != "SN85" {
		t.Fatalf("device metadata = %+v", device)
	}
}
