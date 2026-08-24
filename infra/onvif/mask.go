package onvif

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ONVIF privacy masks: regions the CAMERA refuses to show anybody (W3-6).
//
// WHY THIS HAS TO BE THE CAMERA'S JOB, and not ours. A privacy mask exists to make certain
// pixels not exist — the neighbour's window, the pavement outside the gate, the keypad
// somebody types a PIN into. There are three places a mask could be applied, and only one
// of them is a privacy mask:
//
//   - IN THE CAMERA. The pixels never leave the sensor. The recording on disk does not
//     contain them, an export cannot leak them, and somebody who walks off with the drive
//     has nothing. This is what this file does.
//   - IN OUR RECORDING PIPELINE. It would work, and it would cost the product its
//     architecture: recording is `-c copy` today (infra/recording/encode.go), and masking
//     mid-stream means decoding, filtering and re-encoding every camera, all the time. The
//     capacity story changes by an order of magnitude for a feature most cameras will do
//     for free.
//   - IN THE VIEWER. This is the trap. An overlay drawn over a video element looks
//     identical to the operator and protects nothing: the pixels are on disk, in the
//     export, and in every copy of the file. A mask that is not burned in is a courtesy,
//     not a privacy control, and the difference has to be stated rather than implied.
//
// So masks go to the device, over ONVIF Media2 — and where a camera will not take them,
// the product says so instead of pretending. See apps/mymatasan/services/privacy.go.
//
// A CAMERA CAN ACCEPT A WRITE AND NOT APPLY IT. encoder.go already carries this scar for
// H.265: "many cameras accept a Media1 set with HTTP 200 but silently keep H.264". A
// privacy mask you believe is applied is worse than one you know is missing, so every
// write here is READ BACK and compared — see VerifyMasks.

// Mask types, as ONVIF spells them.
const (
	MaskTypeColor     = "Color"
	MaskTypePixelated = "Pixelated"
	MaskTypeBlurred   = "Blurred"
)

// Mask is one privacy region on a camera.
type Mask struct {
	// Token is the device's identifier, empty for a mask that has not been created yet.
	Token string `json:"token"`
	// ConfigurationToken is the video source configuration the mask belongs to.
	ConfigurationToken string `json:"configurationToken"`
	// Polygon is the masked region, in ONVIF's normalized coordinate space (see
	// maskPointFromUnit).
	Polygon []MaskPoint `json:"polygon"`
	Type    string      `json:"type"`
	Enabled bool        `json:"enabled"`
}

// MaskPoint is one polygon vertex in ONVIF's coordinate space.
type MaskPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// MaskOptions is what the camera says it can do with masks. Read before writing, because
// a camera that supports four masks and rectangles only will accept a fifth polygon and
// quietly do nothing with it.
type MaskOptions struct {
	// MaxMasks is how many the camera can hold; 0 means it did not say.
	MaxMasks int `json:"maxMasks"`
	// MaxPoints bounds a polygon; 0 means it did not say.
	MaxPoints int `json:"maxPoints"`
	// RectangleOnly is true when the camera will only accept axis-aligned rectangles,
	// whatever polygon it is sent.
	RectangleOnly bool `json:"rectangleOnly"`
	// Types are the mask styles the camera supports (Color, Pixelated, Blurred).
	Types []string `json:"types"`
	// Supported is false when the camera has no Media2 service or no mask support at all,
	// which is a fact the product has to surface rather than retry.
	Supported bool `json:"supported"`
}

// MaskRequest addresses a camera's mask operations.
type MaskRequest struct {
	DeviceServiceURL string
	// Media2ServiceURL, when known, skips service discovery.
	Media2ServiceURL   string
	Credentials        Credentials
	ConfigurationToken string
	Token              string
	Mask               Mask
}

// GetMaskOptions asks the camera what it can do with masks.
func (c *Client) GetMaskOptions(ctx context.Context, req MaskRequest) (*MaskOptions, error) {
	endpoint, err := c.media2Endpoint(ctx, req)
	if err != nil {
		// Not an error the caller should retry: a camera without Media2 has no ONVIF mask
		// support, full stop, and the product's job is to say so.
		return &MaskOptions{Supported: false}, nil
	}
	body, _, err := c.postSOAP(ctx, endpoint, fmt.Sprintf(`
    <tr2:GetMaskOptions>
      <tr2:ConfigurationToken>%s</tr2:ConfigurationToken>
    </tr2:GetMaskOptions>`, xmlEscape(req.ConfigurationToken)), req.Credentials)
	if err != nil {
		// A fault here means the service exists and masks do not. Same answer, said once.
		return &MaskOptions{Supported: false}, nil
	}
	return ParseMaskOptions(body)
}

// GetMasks lists the masks currently on the camera.
func (c *Client) GetMasks(ctx context.Context, req MaskRequest) ([]Mask, error) {
	endpoint, err := c.media2Endpoint(ctx, req)
	if err != nil {
		return nil, err
	}
	filter := ""
	if strings.TrimSpace(req.ConfigurationToken) != "" {
		filter = fmt.Sprintf(`
      <tr2:ConfigurationToken>%s</tr2:ConfigurationToken>`, xmlEscape(req.ConfigurationToken))
	}
	body, _, err := c.postSOAP(ctx, endpoint, "\n    <tr2:GetMasks>"+filter+"\n    </tr2:GetMasks>", req.Credentials)
	if err != nil {
		return nil, maskError("list the camera's privacy masks", body, err)
	}
	return ParseMasks(body)
}

// CreateMask adds a mask and returns the token the camera issued.
func (c *Client) CreateMask(ctx context.Context, req MaskRequest) (string, error) {
	endpoint, err := c.media2Endpoint(ctx, req)
	if err != nil {
		return "", err
	}
	body, _, err := c.postSOAP(ctx, endpoint, "\n    <tr2:CreateMask>"+maskXML(req.Mask, false)+"\n    </tr2:CreateMask>", req.Credentials)
	if err != nil {
		return "", maskError("add a privacy mask", body, err)
	}
	token, perr := ParseCreateMaskToken(body)
	if perr != nil {
		return "", perr
	}
	if strings.TrimSpace(token) == "" {
		// Without the token the mask may exist on the camera and cannot be edited or
		// removed from here. Saying so beats reporting success.
		return "", errors.New("the camera stored the mask but returned no token for it")
	}
	return token, nil
}

// SetMask updates an existing mask.
func (c *Client) SetMask(ctx context.Context, req MaskRequest) error {
	if strings.TrimSpace(req.Mask.Token) == "" {
		return errors.New("a mask token is required")
	}
	endpoint, err := c.media2Endpoint(ctx, req)
	if err != nil {
		return err
	}
	body, _, err := c.postSOAP(ctx, endpoint, "\n    <tr2:SetMask>"+maskXML(req.Mask, true)+"\n    </tr2:SetMask>", req.Credentials)
	if err != nil {
		return maskError("change a privacy mask", body, err)
	}
	return nil
}

// DeleteMask removes a mask from the camera.
func (c *Client) DeleteMask(ctx context.Context, req MaskRequest) error {
	if strings.TrimSpace(req.Token) == "" {
		return errors.New("a mask token is required")
	}
	endpoint, err := c.media2Endpoint(ctx, req)
	if err != nil {
		return err
	}
	body, _, err := c.postSOAP(ctx, endpoint, fmt.Sprintf(`
    <tr2:DeleteMask>
      <tr2:Token>%s</tr2:Token>
    </tr2:DeleteMask>`, xmlEscape(req.Token)), req.Credentials)
	if err != nil {
		return maskError("remove a privacy mask", body, err)
	}
	return nil
}

func (c *Client) media2Endpoint(ctx context.Context, req MaskRequest) (string, error) {
	if url := strings.TrimSpace(req.Media2ServiceURL); url != "" {
		return url, nil
	}
	url, err := c.getMedia2ServiceURL(ctx, req.DeviceServiceURL, req.Credentials)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(url) == "" {
		return "", errors.New("this camera does not offer the ONVIF Media2 service, which is where privacy masks live")
	}
	return url, nil
}

func maskError(action string, body []byte, err error) error {
	if reason := ParseSOAPFault(body); reason != "" {
		return fmt.Errorf("%s failed: %s", action, reason)
	}
	return fmt.Errorf("%s failed: %w", action, err)
}

func maskXML(mask Mask, withToken bool) string {
	var sb strings.Builder
	sb.WriteString("\n      <tr2:Mask")
	if withToken && strings.TrimSpace(mask.Token) != "" {
		sb.WriteString(fmt.Sprintf(` token="%s"`, xmlEscape(mask.Token)))
	}
	sb.WriteString(">")
	sb.WriteString(fmt.Sprintf(`
        <tr2:ConfigurationToken>%s</tr2:ConfigurationToken>`, xmlEscape(mask.ConfigurationToken)))
	sb.WriteString("\n        <tr2:Polygon>")
	for _, p := range mask.Polygon {
		sb.WriteString(fmt.Sprintf(`
          <tt:Point x="%s" y="%s"/>`, maskFloat(p.X), maskFloat(p.Y)))
	}
	sb.WriteString("\n        </tr2:Polygon>")
	maskType := strings.TrimSpace(mask.Type)
	if maskType == "" {
		maskType = MaskTypeColor
	}
	sb.WriteString(fmt.Sprintf(`
        <tr2:Type>%s</tr2:Type>
        <tr2:Enabled>%t</tr2:Enabled>`, xmlEscape(maskType), mask.Enabled))
	sb.WriteString("\n      </tr2:Mask>")
	return sb.String()
}

func maskFloat(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }

// MaskPointFromUnit converts a point in OUR coordinate space (0..1, origin top-left, the
// same space detection zones use) into ONVIF's (-1..1, origin centre, y up).
//
// The two conventions differ in origin, scale AND the direction of y, which is three
// chances to be wrong in a way that still produces a plausible-looking rectangle. It is one
// function, used in both directions, and the round trip is what VerifyMasks compares.
func MaskPointFromUnit(x, y float64) MaskPoint {
	return MaskPoint{X: x*2 - 1, Y: 1 - y*2}
}

// MaskPointToUnit is MaskPointFromUnit's inverse.
func MaskPointToUnit(p MaskPoint) (float64, float64) {
	return (p.X + 1) / 2, (1 - p.Y) / 2
}

// MasksMatch reports whether a mask read back from a camera describes the same region that
// was written, within tolerance.
//
// THIS IS THE POINT OF THE WHOLE FILE. A camera can accept a mask with HTTP 200 and store
// something else — a different coordinate space, a bounding rectangle instead of the
// polygon, or nothing at all. A privacy mask that is believed to be applied and is not is
// worse than no mask, because somebody relies on it. Anything that does not round-trip is
// reported as unconfirmed rather than as protected.
//
// The tolerance is generous on purpose: cameras quantise mask coordinates to their own
// grid, and a few pixels of difference is the device being a device. A different coordinate
// space is out by a factor, not by a rounding.
func MasksMatch(want, got []MaskPoint, tolerance float64) bool {
	if len(want) == 0 || len(want) != len(got) {
		return false
	}
	if tolerance <= 0 {
		tolerance = 0.05
	}
	for i := range want {
		if math.Abs(want[i].X-got[i].X) > tolerance || math.Abs(want[i].Y-got[i].Y) > tolerance {
			return false
		}
	}
	return true
}

// --- parsing -------------------------------------------------------------------------

type maskOptionsEnvelopeXML struct {
	Body maskOptionsBodyXML `xml:"Body"`
}

type maskOptionsBodyXML struct {
	Response maskOptionsResponseXML `xml:"GetMaskOptionsResponse"`
}

type maskOptionsResponseXML struct {
	Options maskOptionsXML `xml:"Options"`
}

type maskOptionsXML struct {
	MaxMasks      int      `xml:"MaxMasks"`
	MaxPoints     int      `xml:"MaxPoints"`
	RectangleOnly bool     `xml:"RectangleOnly"`
	Types         []string `xml:"Types"`
}

type masksEnvelopeXML struct {
	Body masksBodyXML `xml:"Body"`
}

type masksBodyXML struct {
	Response masksResponseXML `xml:"GetMasksResponse"`
}

type masksResponseXML struct {
	Masks []maskXMLShape `xml:"Masks"`
}

type maskXMLShape struct {
	Token              string         `xml:"token,attr"`
	ConfigurationToken string         `xml:"ConfigurationToken"`
	Polygon            maskPolygonXML `xml:"Polygon"`
	Type               string         `xml:"Type"`
	Enabled            bool           `xml:"Enabled"`
}

type maskPolygonXML struct {
	Points []maskPointXML `xml:"Point"`
}

type maskPointXML struct {
	X string `xml:"x,attr"`
	Y string `xml:"y,attr"`
}

type createMaskEnvelopeXML struct {
	Body createMaskBodyXML `xml:"Body"`
}

type createMaskBodyXML struct {
	Response createMaskResponseXML `xml:"CreateMaskResponse"`
}

type createMaskResponseXML struct {
	Token string `xml:"Token"`
}

// ParseMaskOptions parses GetMaskOptionsResponse.
func ParseMaskOptions(data []byte) (*MaskOptions, error) {
	var envelope maskOptionsEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	opts := envelope.Body.Response.Options
	types := make([]string, 0, len(opts.Types))
	for _, t := range opts.Types {
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			types = append(types, trimmed)
		}
	}
	return &MaskOptions{
		MaxMasks:      opts.MaxMasks,
		MaxPoints:     opts.MaxPoints,
		RectangleOnly: opts.RectangleOnly,
		Types:         types,
		// A camera that answered GetMaskOptions at all supports masks; MaxMasks of 0 means
		// it did not say how many, not that it can hold none.
		Supported: true,
	}, nil
}

// ParseMasks parses GetMasksResponse.
func ParseMasks(data []byte) ([]Mask, error) {
	var envelope masksEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	masks := make([]Mask, 0, len(envelope.Body.Response.Masks))
	for _, m := range envelope.Body.Response.Masks {
		token := strings.TrimSpace(m.Token)
		if token == "" {
			// The token is the only handle: a tokenless mask is one nothing can edit or
			// remove, and listing it would offer the operator a control that cannot work.
			continue
		}
		mask := Mask{
			Token:              token,
			ConfigurationToken: strings.TrimSpace(m.ConfigurationToken),
			Type:               strings.TrimSpace(m.Type),
			Enabled:            m.Enabled,
		}
		for _, p := range m.Polygon.Points {
			x, xok := ptzCoord(p.X)
			y, yok := ptzCoord(p.Y)
			if !xok || !yok {
				continue
			}
			mask.Polygon = append(mask.Polygon, MaskPoint{X: x, Y: y})
		}
		masks = append(masks, mask)
	}
	return masks, nil
}

// ParseCreateMaskToken parses CreateMaskResponse.
func ParseCreateMaskToken(data []byte) (string, error) {
	var envelope createMaskEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return "", err
	}
	return strings.TrimSpace(envelope.Body.Response.Token), nil
}

// GetProfileDetails lists the camera's media profiles, including the video source
// configuration each one draws from — which is what a privacy mask attaches to.
func (c *Client) GetProfileDetails(ctx context.Context, req StreamURIRequest) ([]MediaProfile, error) {
	mediaURL := strings.TrimSpace(req.MediaServiceURL)
	if mediaURL == "" {
		resolved, err := c.getMediaServiceURL(ctx, req.DeviceServiceURL, req.Credentials)
		if err != nil {
			return nil, err
		}
		mediaURL = resolved
	}
	body, _, err := c.postSOAP(ctx, mediaURL, getProfilesBody(), req.Credentials)
	if err != nil {
		return nil, maskError("read the camera's media profiles", body, err)
	}
	return ParseProfiles(body)
}
