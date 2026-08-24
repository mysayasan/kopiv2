package onvif

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const masksResponse = `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tr2:GetMasksResponse xmlns:tr2="http://www.onvif.org/ver20/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
      <tr2:Masks token="MASK_1">
        <tr2:ConfigurationToken>VSC_1</tr2:ConfigurationToken>
        <tr2:Polygon>
          <tt:Point x="-0.5000" y="0.5000"/>
          <tt:Point x="0.0000" y="0.5000"/>
          <tt:Point x="0.0000" y="0.0000"/>
          <tt:Point x="-0.5000" y="0.0000"/>
        </tr2:Polygon>
        <tr2:Type>Color</tr2:Type>
        <tr2:Enabled>true</tr2:Enabled>
      </tr2:Masks>
      <tr2:Masks>
        <tr2:ConfigurationToken>VSC_1</tr2:ConfigurationToken>
        <tr2:Type>Blurred</tr2:Type>
        <tr2:Enabled>true</tr2:Enabled>
      </tr2:Masks>
    </tr2:GetMasksResponse>
  </s:Body>
</s:Envelope>`

func TestParseMasks(t *testing.T) {
	masks, err := ParseMasks([]byte(masksResponse))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The tokenless mask is dropped: the token is the only handle, so listing it would
	// offer an operator a control that cannot edit or remove anything.
	if len(masks) != 1 {
		t.Fatalf("want 1 mask, got %d: %+v", len(masks), masks)
	}
	m := masks[0]
	if m.Token != "MASK_1" || m.ConfigurationToken != "VSC_1" || !m.Enabled {
		t.Fatalf("mask wrong: %+v", m)
	}
	if len(m.Polygon) != 4 {
		t.Fatalf("polygon lost points: %+v", m.Polygon)
	}
	if m.Polygon[0].X != -0.5 || m.Polygon[0].Y != 0.5 {
		t.Fatalf("first point = %+v", m.Polygon[0])
	}
}

// The two coordinate conventions differ in ORIGIN, SCALE and the DIRECTION OF Y — three
// chances to be wrong in a way that still draws a plausible-looking rectangle somewhere on
// the picture. One function, used in both directions, and the round trip is what the
// camera read-back is compared against.
func TestMaskCoordinateRoundTrip(t *testing.T) {
	cases := [][2]float64{
		{0, 0},       // our top-left
		{1, 1},       // our bottom-right
		{0.5, 0.5},   // centre
		{0.25, 0.75}, // an asymmetric point, so a sign error cannot pass
	}
	for _, c := range cases {
		p := MaskPointFromUnit(c[0], c[1])
		x, y := MaskPointToUnit(p)
		if math.Abs(x-c[0]) > 1e-9 || math.Abs(y-c[1]) > 1e-9 {
			t.Fatalf("round trip of %v gave %v,%v (via %+v)", c, x, y, p)
		}
	}
	// The conventions really are different, so the naive identity must NOT hold — a test
	// that passes with a no-op conversion is testing nothing.
	if p := MaskPointFromUnit(0, 0); p.X != -1 || p.Y != 1 {
		t.Fatalf("our top-left should be ONVIF (-1, 1), got %+v", p)
	}
	if p := MaskPointFromUnit(1, 1); p.X != 1 || p.Y != -1 {
		t.Fatalf("our bottom-right should be ONVIF (1, -1), got %+v", p)
	}
}

// A camera can accept a mask with HTTP 200 and store something else. A mask believed to be
// applied and not applied is worse than no mask, because somebody relies on it.
func TestMasksMatch(t *testing.T) {
	want := []MaskPoint{{X: -0.5, Y: 0.5}, {X: 0, Y: 0.5}, {X: 0, Y: 0}, {X: -0.5, Y: 0}}

	if !MasksMatch(want, want, 0.05) {
		t.Fatal("a mask must match itself")
	}
	// Device quantisation is the device being a device.
	nudged := []MaskPoint{{X: -0.51, Y: 0.51}, {X: 0.01, Y: 0.49}, {X: 0.0, Y: 0.01}, {X: -0.49, Y: 0}}
	if !MasksMatch(want, nudged, 0.05) {
		t.Fatal("a few pixels of rounding must not read as a failure")
	}
	// A DIFFERENT COORDINATE SPACE is out by a factor, not a rounding — the case this
	// exists to catch.
	wrongSpace := []MaskPoint{{X: 0.25, Y: 0.25}, {X: 0.5, Y: 0.25}, {X: 0.5, Y: 0.5}, {X: 0.25, Y: 0.5}}
	if MasksMatch(want, wrongSpace, 0.05) {
		t.Fatal("a mask stored in another coordinate space must not read as applied")
	}
	// A camera that reduced the polygon to a bounding box has fewer points, and a shape
	// that is not the shape asked for.
	if MasksMatch(want, want[:2], 0.05) {
		t.Fatal("a mask with points missing must not read as applied")
	}
	// Nothing stored at all is the loudest failure and must never match.
	if MasksMatch(want, nil, 0.5) {
		t.Fatal("no mask must never match a mask")
	}
	if MasksMatch(nil, nil, 0.05) {
		t.Fatal("an empty region is not a confirmed mask")
	}
}

func TestMaskOptionsAndUnsupportedCameras(t *testing.T) {
	// A camera that answers GetMaskOptions supports masks; MaxMasks of 0 means it did not
	// say how many, not that it can hold none.
	opts, err := ParseMaskOptions([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>
		<tr2:GetMaskOptionsResponse xmlns:tr2="http://www.onvif.org/ver20/media/wsdl">
			<tr2:Options><tr2:MaxMasks>4</tr2:MaxMasks><tr2:MaxPoints>4</tr2:MaxPoints>
			<tr2:RectangleOnly>true</tr2:RectangleOnly>
			<tr2:Types>Color</tr2:Types><tr2:Types>Blurred</tr2:Types></tr2:Options>
		</tr2:GetMaskOptionsResponse></s:Body></s:Envelope>`))
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	if !opts.Supported || opts.MaxMasks != 4 || !opts.RectangleOnly {
		t.Fatalf("options wrong: %+v", opts)
	}
	if strings.Join(opts.Types, ",") != "Color,Blurred" {
		t.Fatalf("types = %v", opts.Types)
	}

	// A camera with no Media2 service has no ONVIF mask support at all. That is an answer,
	// not an error to retry: the product's job is to say the camera cannot mask.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>
			<tds:GetServicesResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>
		</s:Body></s:Envelope>`))
	}))
	defer server.Close()
	got, err := NewClient().GetMaskOptions(context.Background(), MaskRequest{DeviceServiceURL: server.URL})
	if err != nil {
		t.Fatalf("a camera without Media2 must not be an error: %v", err)
	}
	if got.Supported {
		t.Fatal("a camera without Media2 cannot mask, and must say so")
	}
}

func TestMaskBodies(t *testing.T) {
	mask := Mask{
		Token: "MASK_1", ConfigurationToken: "VSC_1", Type: MaskTypeColor, Enabled: true,
		Polygon: []MaskPoint{{X: -0.5, Y: 0.5}, {X: 0, Y: 0}},
	}
	// A create must NOT carry a token — some devices treat that as an update of a mask
	// that does not exist and refuse the whole call.
	create := maskXML(mask, false)
	if strings.Contains(create, `token="MASK_1"`) {
		t.Fatalf("create carried a token: %s", create)
	}
	update := maskXML(mask, true)
	if !strings.Contains(update, `token="MASK_1"`) {
		t.Fatalf("update did not name the mask: %s", update)
	}
	if !strings.Contains(update, "<tr2:Enabled>true</tr2:Enabled>") {
		t.Fatalf("enabled not sent: %s", update)
	}
	// A mask with no type still has to be a valid request; Color is the one every camera
	// that supports masks supports.
	if !strings.Contains(maskXML(Mask{ConfigurationToken: "VSC_1"}, false), "<tr2:Type>Color</tr2:Type>") {
		t.Fatal("a mask with no type must default to Color")
	}
}

func TestCreateMaskWithoutATokenIsAnError(t *testing.T) {
	// The mask may then exist on the CAMERA and be uneditable and unremovable from here.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>
			<tr2:CreateMaskResponse xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"/>
		</s:Body></s:Envelope>`))
	}))
	defer server.Close()

	if _, err := NewClient().CreateMask(context.Background(), MaskRequest{
		Media2ServiceURL: server.URL, Mask: Mask{ConfigurationToken: "VSC_1"},
	}); err == nil {
		t.Fatal("a mask the camera will not name must fail")
	}
}

func TestMaskSurfacesTheDeviceRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault>
			<s:Reason><s:Text xml:lang="en">The maximum number of masks has been reached</s:Text></s:Reason>
		</s:Fault></s:Body></s:Envelope>`))
	}))
	defer server.Close()

	_, err := NewClient().CreateMask(context.Background(), MaskRequest{
		Media2ServiceURL: server.URL, Mask: Mask{ConfigurationToken: "VSC_1", Polygon: []MaskPoint{{}}},
	})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "maximum number of masks") || strings.Contains(err.Error(), "500") {
		t.Fatalf("the camera's own explanation was lost: %v", err)
	}
}
