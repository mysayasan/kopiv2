package onvif

import (
	"strings"
	"testing"
)

const profilesWithEncoderXML = `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
      <trt:Profiles token="MainStream">
        <tt:Name>Main</tt:Name>
        <tt:VideoEncoderConfiguration token="VEC_0">
          <tt:Name>VEnc0</tt:Name>
          <tt:Encoding>H264</tt:Encoding>
          <tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>
          <tt:Quality>4</tt:Quality>
          <tt:RateControl>
            <tt:FrameRateLimit>25</tt:FrameRateLimit>
            <tt:EncodingInterval>1</tt:EncodingInterval>
            <tt:BitrateLimit>4096</tt:BitrateLimit>
          </tt:RateControl>
          <tt:H264><tt:GovLength>50</tt:GovLength><tt:H264Profile>High</tt:H264Profile></tt:H264>
          <tt:SessionTimeout>PT30S</tt:SessionTimeout>
        </tt:VideoEncoderConfiguration>
      </trt:Profiles>
      <trt:Profiles token="SubStream">
        <tt:Name>Sub</tt:Name>
        <tt:VideoEncoderConfiguration token="VEC_1">
          <tt:Encoding>H265</tt:Encoding>
          <tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution>
          <tt:RateControl><tt:BitrateLimit>512</tt:BitrateLimit></tt:RateControl>
        </tt:VideoEncoderConfiguration>
      </trt:Profiles>
    </trt:GetProfilesResponse>
  </s:Body>
</s:Envelope>`

func TestParseVideoEncoderConfigByToken(t *testing.T) {
	cfg, err := ParseVideoEncoderConfig([]byte(profilesWithEncoderXML), "MainStream")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Token != "VEC_0" || cfg.Encoding != "H264" {
		t.Fatalf("unexpected config token/encoding: %+v", cfg)
	}
	if cfg.Width != 1920 || cfg.Height != 1080 || cfg.BitrateLimit != 4096 || cfg.FrameRateLimit != 25 {
		t.Fatalf("unexpected media fields: %+v", cfg)
	}
	if cfg.GovLength != 50 || cfg.H264Profile != "High" {
		t.Fatalf("unexpected H264 fields: %+v", cfg)
	}
}

func TestParseVideoEncoderConfigMissingProfile(t *testing.T) {
	if _, err := ParseVideoEncoderConfig([]byte(profilesWithEncoderXML), "Nope"); err == nil {
		t.Fatal("expected error for unknown profile token")
	}
}

// The Set body must reflect the new encoding and omit the H264 block when switching
// to H265 (cameras reject an H264 element on an H265 configuration).
func TestSetVideoEncoderConfigurationBodyH265(t *testing.T) {
	body := setVideoEncoderConfigurationBody(VideoEncoderConfig{
		Token: "VEC_0", Name: "VEnc0", Encoding: "H265",
		Width: 1920, Height: 1080, BitrateLimit: 2048, FrameRateLimit: 25,
	})
	if !strings.Contains(body, `token="VEC_0"`) || !strings.Contains(body, "<tt:Encoding>H265</tt:Encoding>") {
		t.Fatalf("missing token/encoding: %s", body)
	}
	if strings.Contains(body, "<tt:H264>") {
		t.Fatalf("H265 config must not emit an H264 block: %s", body)
	}
	if !strings.Contains(body, "<tt:BitrateLimit>2048</tt:BitrateLimit>") {
		t.Fatalf("missing bitrate: %s", body)
	}
	if !strings.Contains(body, "<trt:ForcePersistence>true</trt:ForcePersistence>") {
		t.Fatalf("change must be persisted across reboot: %s", body)
	}
}

func TestNormalizeEncoding(t *testing.T) {
	cases := map[string]string{"hevc": "H265", "h265": "H265", "h.264": "H264", "avc": "H264", "vp9": ""}
	for in, want := range cases {
		if got := normalizeEncoding(in); got != want {
			t.Fatalf("normalizeEncoding(%q)=%q want %q", in, got, want)
		}
	}
}

const getServicesXML = `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tds:GetServicesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
      <tds:Service>
        <tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace>
        <tds:XAddr>http://cam/onvif/media_service</tds:XAddr>
      </tds:Service>
      <tds:Service>
        <tds:Namespace>http://www.onvif.org/ver20/media/wsdl</tds:Namespace>
        <tds:XAddr>http://cam/onvif/media2_service</tds:XAddr>
      </tds:Service>
    </tds:GetServicesResponse>
  </s:Body>
</s:Envelope>`

func TestParseMedia2ServiceXAddr(t *testing.T) {
	if got := ParseMedia2ServiceXAddr([]byte(getServicesXML)); got != "http://cam/onvif/media2_service" {
		t.Fatalf("media2 xaddr = %q", got)
	}
	// No ver20 media advertised → empty.
	none := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>
	  <tds:GetServicesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
	    <tds:Service><tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace><tds:XAddr>http://cam/m1</tds:XAddr></tds:Service>
	  </tds:GetServicesResponse></s:Body></s:Envelope>`
	if got := ParseMedia2ServiceXAddr([]byte(none)); got != "" {
		t.Fatalf("expected empty when no ver20 media, got %q", got)
	}
}

// The Media2 set body must place GovLength/Profile as attributes and the bitrate in
// the RateControl attribute, per the ver20 schema (where H.265 is valid).
func TestSetVideoEncoder2ConfigurationBody(t *testing.T) {
	body := setVideoEncoder2ConfigurationBody(VideoEncoderConfig{
		Token: "vec0", Encoding: "H265", Width: 1920, Height: 1080, BitrateLimit: 2048, FrameRateLimit: 25,
	})
	for _, want := range []string{`<tr2:Configuration token="vec0"`, `GovLength=`, `Profile=`, "<tt:Encoding>H265</tt:Encoding>", `BitrateLimit="2048"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("media2 body missing %q: %s", want, body)
		}
	}
}

func TestCanonicalEncoding(t *testing.T) {
	if canonicalEncoding("HEVC") != "h265" || canonicalEncoding("H264") != "h264" {
		t.Fatal("canonicalEncoding should fold HEVC->h265 and H264->h264")
	}
}
