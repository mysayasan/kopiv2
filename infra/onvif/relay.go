package onvif

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ONVIF relay outputs: the dry contacts on a camera's terminal block (W3-5b).
//
// This is the first thing in the product that acts on the world beyond a camera's own
// movement. A relay output drives a siren, a strobe, a gate, a door strike, a light. It is
// the difference between recording an intrusion and responding to one.
//
// TWO PROPERTIES OF A RELAY DECIDE HOW IT MUST BE DRIVEN, and getting them wrong is not a
// cosmetic bug:
//
//   - MODE. A MONOSTABLE relay returns to idle on its own after DelayTime; a BISTABLE one
//     stays where it was put, forever, until something puts it back. A siren driven through
//     a bistable relay by code that only ever sends "active" is a siren nobody can silence.
//   - IDLE STATE. "Closed" and "open" describe the CIRCUIT, not the intent, and an installer
//     may have wired either sense. `active` therefore means "the non-idle state", which is
//     a fact about the building. It is settled where somebody can answer it — on the screen
//     — not guessed here.
//
// The operations live on the Device Management service in ONVIF Core, and are mirrored on
// the DeviceIO service on cameras that have one. `relayEndpoint` prefers DeviceIO when the
// device advertises it and falls back to the device service, because firmware differs on
// which one actually implements them.

// RelayMode values.
const (
	RelayModeMonostable = "Monostable"
	RelayModeBistable   = "Bistable"
)

// RelayIdleState values.
const (
	RelayIdleClosed = "closed"
	RelayIdleOpen   = "open"
)

// RelayOutput is one physical output on a device.
type RelayOutput struct {
	Token string `json:"token"`
	// Mode is Monostable (returns to idle by itself) or Bistable (stays put).
	Mode string `json:"mode"`
	// DelayTime is how long a monostable relay holds before returning to idle, as the
	// device reports it (an xs:duration such as "PT5S"), plus the seconds we read out of it.
	DelayTime    string `json:"delayTime"`
	DelaySeconds int    `json:"delaySeconds"`
	IdleState    string `json:"idleState"`
	// Bistable is a convenience for callers that only care about the one question that
	// changes their behaviour: does this relay put itself back?
	Bistable bool `json:"bistable"`
}

// RelayRequest addresses a device's relay operations.
type RelayRequest struct {
	DeviceServiceURL string
	// RelayServiceURL, when known, skips service discovery.
	RelayServiceURL string
	Credentials     Credentials
	Token           string
	// Active drives the relay to its non-idle state; false returns it to idle.
	Active bool
	// Mode and DelaySeconds are used by SetRelayOutputSettings.
	Mode         string
	DelaySeconds int
	IdleState    string
}

// GetRelayOutputs lists the device's outputs.
func (c *Client) GetRelayOutputs(ctx context.Context, req RelayRequest) ([]RelayOutput, error) {
	endpoint, err := c.relayEndpoint(ctx, req)
	if err != nil {
		return nil, err
	}
	body, _, err := c.postSOAP(ctx, endpoint, "\n    <tds:GetRelayOutputs/>", req.Credentials)
	if err != nil {
		return nil, relayError("list the camera's relay outputs", body, err)
	}
	return ParseRelayOutputs(body)
}

// SetRelayOutputState drives one output.
func (c *Client) SetRelayOutputState(ctx context.Context, req RelayRequest) error {
	if strings.TrimSpace(req.Token) == "" {
		return errors.New("a relay token is required")
	}
	endpoint, err := c.relayEndpoint(ctx, req)
	if err != nil {
		return err
	}
	state := "inactive"
	if req.Active {
		state = "active"
	}
	body, _, err := c.postSOAP(ctx, endpoint, fmt.Sprintf(`
    <tds:SetRelayOutputState>
      <tds:RelayOutputToken>%s</tds:RelayOutputToken>
      <tds:LogicalState>%s</tds:LogicalState>
    </tds:SetRelayOutputState>`, xmlEscape(req.Token), state), req.Credentials)
	if err != nil {
		return relayError("switch the camera's relay output", body, err)
	}
	return nil
}

// SetRelayOutputSettings changes an output's mode, hold time and idle sense.
func (c *Client) SetRelayOutputSettings(ctx context.Context, req RelayRequest) error {
	if strings.TrimSpace(req.Token) == "" {
		return errors.New("a relay token is required")
	}
	endpoint, err := c.relayEndpoint(ctx, req)
	if err != nil {
		return err
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = RelayModeMonostable
	}
	idle := strings.TrimSpace(req.IdleState)
	if idle == "" {
		idle = RelayIdleClosed
	}
	delay := req.DelaySeconds
	if delay <= 0 {
		delay = 1
	}
	body, _, err := c.postSOAP(ctx, endpoint, fmt.Sprintf(`
    <tds:SetRelayOutputSettings>
      <tds:RelayOutputToken>%s</tds:RelayOutputToken>
      <tds:Properties>
        <tt:Mode>%s</tt:Mode>
        <tt:DelayTime>PT%dS</tt:DelayTime>
        <tt:IdleState>%s</tt:IdleState>
      </tds:Properties>
    </tds:SetRelayOutputSettings>`, xmlEscape(req.Token), xmlEscape(mode), delay, xmlEscape(idle)), req.Credentials)
	if err != nil {
		return relayError("change the relay output settings", body, err)
	}
	return nil
}

// relayEndpoint resolves where the relay operations live, preferring DeviceIO.
func (c *Client) relayEndpoint(ctx context.Context, req RelayRequest) (string, error) {
	if url := strings.TrimSpace(req.RelayServiceURL); url != "" {
		return url, nil
	}
	deviceURL, err := NormalizeDeviceServiceURL(req.DeviceServiceURL)
	if err != nil {
		return "", err
	}
	if services, svcErr := c.GetServices(ctx, DeviceRequest{DeviceServiceURL: deviceURL, Credentials: req.Credentials}); svcErr == nil {
		for _, svc := range services {
			if strings.Contains(strings.ToLower(svc.Namespace), "deviceio") && strings.TrimSpace(svc.XAddr) != "" {
				return strings.TrimSpace(svc.XAddr), nil
			}
		}
	}
	// The device service is mandatory and carries these operations in ONVIF Core, so it is
	// the fallback rather than an error: a camera with relays and no DeviceIO service is
	// common, and refusing it would hide working hardware.
	return deviceURL, nil
}

func relayError(action string, body []byte, err error) error {
	if reason := ParseSOAPFault(body); reason != "" {
		return fmt.Errorf("%s failed: %s", action, reason)
	}
	return fmt.Errorf("%s failed: %w", action, err)
}

type relayOutputsEnvelopeXML struct {
	Body relayOutputsBodyXML `xml:"Body"`
}

type relayOutputsBodyXML struct {
	Response relayOutputsResponseXML `xml:"GetRelayOutputsResponse"`
}

type relayOutputsResponseXML struct {
	RelayOutputs []relayOutputXML `xml:"RelayOutputs"`
}

type relayOutputXML struct {
	Token      string             `xml:"token,attr"`
	Properties relayPropertiesXML `xml:"Properties"`
}

type relayPropertiesXML struct {
	Mode      string `xml:"Mode"`
	DelayTime string `xml:"DelayTime"`
	IdleState string `xml:"IdleState"`
}

// ParseRelayOutputs parses GetRelayOutputsResponse.
func ParseRelayOutputs(data []byte) ([]RelayOutput, error) {
	var envelope relayOutputsEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	outputs := make([]RelayOutput, 0, len(envelope.Body.Response.RelayOutputs))
	for _, out := range envelope.Body.Response.RelayOutputs {
		token := strings.TrimSpace(out.Token)
		if token == "" {
			// Same rule as a PTZ preset: the token is the only handle, so a tokenless
			// output is one nothing can switch.
			continue
		}
		mode := strings.TrimSpace(out.Properties.Mode)
		outputs = append(outputs, RelayOutput{
			Token:        token,
			Mode:         mode,
			DelayTime:    strings.TrimSpace(out.Properties.DelayTime),
			DelaySeconds: parseXSDurationSeconds(out.Properties.DelayTime),
			IdleState:    strings.ToLower(strings.TrimSpace(out.Properties.IdleState)),
			// UNKNOWN COUNTS AS BISTABLE. A device that does not say whether its relay
			// returns to idle must be driven as though it does not — the safe assumption
			// is the one where we are responsible for switching it back off, because the
			// other way round leaves a siren running.
			Bistable: !strings.EqualFold(mode, RelayModeMonostable),
		})
	}
	return outputs, nil
}

// parseXSDurationSeconds reads the seconds out of the small subset of xs:duration that
// relay delay times use ("PT5S", "PT1M30S"). Returns 0 on anything else, which callers
// read as "the device did not say".
func parseXSDurationSeconds(value string) int {
	v := strings.ToUpper(strings.TrimSpace(value))
	if !strings.HasPrefix(v, "PT") {
		return 0
	}
	v = v[2:]
	total, digits := 0, ""
	for _, ch := range v {
		switch {
		case ch >= '0' && ch <= '9':
			digits += string(ch)
		case ch == 'H', ch == 'M', ch == 'S':
			n, err := strconv.Atoi(digits)
			digits = ""
			if err != nil {
				return 0
			}
			switch ch {
			case 'H':
				total += n * 3600
			case 'M':
				total += n * 60
			case 'S':
				total += n
			}
		default:
			return 0
		}
	}
	return total
}
