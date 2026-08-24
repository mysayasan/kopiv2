package onvif

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const presetsResponse = `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tptz:GetPresetsResponse xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
      <tptz:Preset token="PRESET_1">
        <tt:Name>Front gate</tt:Name>
        <tt:PTZPosition>
          <tt:PanTilt x="-0.500" y="0.250"/>
          <tt:Zoom x="0.100"/>
        </tt:PTZPosition>
      </tptz:Preset>
      <tptz:Preset token="PRESET_2">
        <tt:Name>Loading bay</tt:Name>
      </tptz:Preset>
      <tptz:Preset token="PRESET_3"/>
      <tptz:Preset>
        <tt:Name>Nameless and tokenless</tt:Name>
      </tptz:Preset>
    </tptz:GetPresetsResponse>
  </s:Body>
</s:Envelope>`

func TestParsePTZPresets(t *testing.T) {
	presets, err := ParsePTZPresets([]byte(presetsResponse))
	if err != nil {
		t.Fatalf("parse presets: %v", err)
	}
	// The tokenless preset is dropped: three remain, not four.
	if len(presets) != 3 {
		t.Fatalf("want 3 presets, got %d: %+v", len(presets), presets)
	}
	if presets[0].Token != "PRESET_1" || presets[0].Name != "Front gate" {
		t.Fatalf("first preset wrong: %+v", presets[0])
	}
	if presets[0].Position == nil {
		t.Fatal("first preset should carry a position")
	}
	if presets[0].Position.Pan != -0.5 || presets[0].Position.Tilt != 0.25 || presets[0].Position.Zoom != 0.1 {
		t.Fatalf("position wrong: %+v", *presets[0].Position)
	}
	// A preset the device reports with no position is normal; it must not read as 0,0,0.
	if presets[1].Position != nil {
		t.Fatalf("second preset should have no position, got %+v", *presets[1].Position)
	}
	// A preset with no name falls back to its token rather than rendering as a blank row.
	if presets[2].Name != "PRESET_3" {
		t.Fatalf("unnamed preset should fall back to its token, got %q", presets[2].Name)
	}
}

func TestParsePTZStatus(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantMoving  bool
		wantHasPos  bool
		wantPanTilt [2]float64
	}{
		{
			name: "moving with position",
			body: ptzStatusEnvelope(`<tt:PanTilt x="0.200" y="-0.400"/><tt:Zoom x="0.500"/>`, "MOVING", "IDLE"),
			// Either axis moving means the camera has not arrived yet.
			wantMoving:  true,
			wantHasPos:  true,
			wantPanTilt: [2]float64{0.2, -0.4},
		},
		{
			name:       "idle",
			body:       ptzStatusEnvelope(`<tt:PanTilt x="0.000" y="0.000"/><tt:Zoom x="0.000"/>`, "IDLE", "IDLE"),
			wantMoving: false,
			// Dead centre is a real position and must be reported as one.
			wantHasPos:  true,
			wantPanTilt: [2]float64{0, 0},
		},
		{
			name: "zoom axis moving",
			body: ptzStatusEnvelope(`<tt:PanTilt x="0.100" y="0.100"/><tt:Zoom x="0.300"/>`, "IDLE", "MOVING"),
			// A camera zooming is still in flight; a tour that stepped on now would
			// cut the move short.
			wantMoving:  true,
			wantHasPos:  true,
			wantPanTilt: [2]float64{0.1, 0.1},
		},
		{
			name: "unknown move status is not moving",
			body: ptzStatusEnvelope(`<tt:PanTilt x="0.100" y="0.100"/><tt:Zoom x="0.000"/>`, "UNKNOWN", "UNKNOWN"),
			// The load-bearing case: a device that never says MOVING must not leave
			// every recall looking permanently in flight, or a tour never steps again.
			wantMoving:  false,
			wantHasPos:  true,
			wantPanTilt: [2]float64{0.1, 0.1},
		},
		{
			name:       "no position reported",
			body:       ptzStatusEnvelope("", "IDLE", "IDLE"),
			wantMoving: false,
			wantHasPos: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, err := ParsePTZStatus([]byte(tc.body))
			if err != nil {
				t.Fatalf("parse status: %v", err)
			}
			if status.Moving != tc.wantMoving {
				t.Fatalf("moving = %v, want %v", status.Moving, tc.wantMoving)
			}
			if status.HasPosition != tc.wantHasPos {
				t.Fatalf("hasPosition = %v, want %v", status.HasPosition, tc.wantHasPos)
			}
			if tc.wantHasPos {
				if status.Position.Pan != tc.wantPanTilt[0] || status.Position.Tilt != tc.wantPanTilt[1] {
					t.Fatalf("position = %+v, want pan/tilt %v", status.Position, tc.wantPanTilt)
				}
			}
		})
	}
}

func TestParseSOAPFault(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "soap 1.2 reason text",
			body: `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault>
				<s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:InvalidArgVal</s:Value>
				<s:Subcode><s:Value>ter:NoPreset</s:Value></s:Subcode></s:Subcode></s:Code>
				<s:Reason><s:Text xml:lang="en">The requested preset token does not exist</s:Text></s:Reason>
			</s:Fault></s:Body></s:Envelope>`,
			want: "The requested preset token does not exist",
		},
		{
			name: "soap 1.1 faultstring",
			body: `<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://schemas.xmlsoap.org/soap/envelope/"><SOAP-ENV:Body>
				<SOAP-ENV:Fault><faultcode>SOAP-ENV:Client</faultcode>
				<faultstring>Maximum number of presets reached</faultstring></SOAP-ENV:Fault>
			</SOAP-ENV:Body></SOAP-ENV:Envelope>`,
			want: "Maximum number of presets reached",
		},
		{
			name: "innermost subcode when there is no text",
			body: `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault>
				<s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:InvalidArgVal</s:Value>
				<s:Subcode><s:Value>ter:TooManyPresets</s:Value></s:Subcode></s:Subcode></s:Code>
			</s:Fault></s:Body></s:Envelope>`,
			// The namespace prefix means nothing to a person, so it is stripped.
			want: "TooManyPresets",
		},
		{
			name: "a successful response is not a fault",
			body: presetsResponse,
			want: "",
		},
		{
			name: "html error page is not a fault",
			body: "<html><body>404 not found</body></html>",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseSOAPFault([]byte(tc.body)); got != tc.want {
				t.Fatalf("ParseSOAPFault = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPTZBodies(t *testing.T) {
	// Speed is OMITTED when zero, never sent as zero: an ONVIF speed of 0 means "do not
	// move", so a defaulted speed produces a recall that is accepted and never arrives.
	if strings.Contains(gotoPresetBody("prof", "p1", 0), "Speed") {
		t.Fatal("a zero speed must not be sent as a speed vector")
	}
	withSpeed := gotoPresetBody("prof", "p1", 0.5)
	if !strings.Contains(withSpeed, `<tptz:Speed>`) || !strings.Contains(withSpeed, `x="0.500"`) {
		t.Fatalf("speed vector missing: %s", withSpeed)
	}

	// SetPreset without a token CREATES; with one it OVERWRITES. Sending an empty
	// PresetToken element makes some devices create a preset the caller cannot address.
	if strings.Contains(setPresetBody("prof", "Gate", ""), "PresetToken") {
		t.Fatal("create must not send an empty PresetToken")
	}
	if !strings.Contains(setPresetBody("prof", "Gate", "p7"), "<tptz:PresetToken>p7</tptz:PresetToken>") {
		t.Fatal("overwrite must name the preset it replaces")
	}

	// AbsoluteMove says Position; RelativeMove says Translation. Strict devices ignore
	// the wrong element and look like a camera that cannot move.
	abs := absoluteMoveBody("AbsoluteMove", "prof", PTZPosition{Pan: 0.5}, 0)
	if !strings.Contains(abs, "<tptz:Position>") || strings.Contains(abs, "Translation") {
		t.Fatalf("absolute move body wrong: %s", abs)
	}
	rel := absoluteMoveBody("RelativeMove", "prof", PTZPosition{Pan: 0.5}, 0)
	if !strings.Contains(rel, "<tptz:Translation>") || strings.Contains(rel, "<tptz:Position>") {
		t.Fatalf("relative move body wrong: %s", rel)
	}

	// Names are attacker-supplied text that ends up inside XML.
	escaped := setPresetBody("prof", `Gate & <Yard>`, "")
	if !strings.Contains(escaped, "Gate &amp; &lt;Yard&gt;") {
		t.Fatalf("preset name not escaped: %s", escaped)
	}
}

func TestGotoPresetSurfacesDeviceFault(t *testing.T) {
	// A camera refusing a preset answers HTTP 500 with a SOAP fault. Without the fault
	// text the operator is told "status 500", which sends an installer to check the
	// network for something the camera already explained.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault>
			<s:Reason><s:Text xml:lang="en">The requested preset token does not exist</s:Text></s:Reason>
		</s:Fault></s:Body></s:Envelope>`))
	}))
	defer server.Close()

	client := NewClient()
	err := client.GotoPreset(context.Background(), PTZPresetRequest{
		PTZServiceURL: server.URL,
		ProfileToken:  "prof",
		PresetToken:   "missing",
	})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "preset token does not exist") {
		t.Fatalf("device explanation lost: %v", err)
	}
	if strings.Contains(err.Error(), "status 500") {
		t.Fatalf("HTTP status leaked instead of the device's reason: %v", err)
	}
}

func TestSetPresetRequiresATokenWhenCreating(t *testing.T) {
	// A device that stores a NEW preset and returns no token leaves us with a position
	// nothing can recall or delete. Saying so beats reporting success.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>
			<tptz:SetPresetResponse xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl"/>
		</s:Body></s:Envelope>`))
	}))
	defer server.Close()

	client := NewClient()
	if _, err := client.SetPreset(context.Background(), PTZPresetRequest{
		PTZServiceURL: server.URL, ProfileToken: "prof", PresetName: "Gate",
	}); err == nil {
		t.Fatal("creating a preset with no returned token must fail")
	}

	// Overwriting is different: the caller already knows the token, and several devices
	// answer an overwrite with an empty response.
	token, err := client.SetPreset(context.Background(), PTZPresetRequest{
		PTZServiceURL: server.URL, ProfileToken: "prof", PresetName: "Gate", PresetToken: "p3",
	})
	if err != nil {
		t.Fatalf("overwrite should succeed: %v", err)
	}
	if token != "p3" {
		t.Fatalf("overwrite should keep its token, got %q", token)
	}
}

func ptzStatusEnvelope(position string, panTiltStatus string, zoomStatus string) string {
	positionBlock := ""
	if position != "" {
		positionBlock = "<tt:Position>" + position + "</tt:Position>"
	}
	return `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tptz:GetStatusResponse xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
      <tptz:PTZStatus>
        ` + positionBlock + `
        <tt:MoveStatus>
          <tt:PanTilt>` + panTiltStatus + `</tt:PanTilt>
          <tt:Zoom>` + zoomStatus + `</tt:Zoom>
        </tt:MoveStatus>
        <tt:UtcTime>2026-08-24T10:00:00Z</tt:UtcTime>
      </tptz:PTZStatus>
    </tptz:GetStatusResponse>
  </s:Body>
</s:Envelope>`
}
