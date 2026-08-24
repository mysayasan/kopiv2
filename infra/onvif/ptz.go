package onvif

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// PTZ beyond jogging: presets, home, and absolute position.
//
// client.go stopped at ContinuousMove/Stop, which is the whole of "an operator is holding
// a button down". Everything an unattended appliance does with a PTZ camera needs a NAMED
// PLACE instead: a guard tour visits places, an alarm sends the camera to a place, and
// "put it back" means the home place. That is what this file adds.
//
// THE PLACES LIVE ON THE CAMERA, NOT IN OUR DATABASE. ONVIF presets are stored by the
// device and recalled by a token the device issues. Mirroring them into a local table
// would create a second answer to "where can this camera point", and the two would part
// company the first time somebody used the camera's own web page — which is how at least
// half of the PTZ cameras in the field get set up. So presets are read live, and anything
// of ours that refers to a preset (a tour stop, an alarm recall) stores the device's token
// and copes with it having gone away. See services/ptz.go.

// PTZPreset is one named position stored on the camera.
type PTZPreset struct {
	// Token is the device's identifier for the preset and the only durable handle on it.
	// Names are not unique on many devices and are editable from the camera's own UI.
	Token string `json:"token"`
	Name  string `json:"name"`
	// Position is where the preset points, when the device reports it. Many devices
	// return presets with no position at all, so an empty position is normal and is
	// not an error — it is only used to show an operator where a preset looks.
	Position *PTZPosition `json:"position,omitempty"`
}

// PTZPosition is a normalized ONVIF PTZ vector: pan/tilt in [-1,1], zoom in [0,1].
type PTZPosition struct {
	Pan  float64 `json:"pan"`
	Tilt float64 `json:"tilt"`
	Zoom float64 `json:"zoom"`
}

// PTZStatus is what the camera says about where it is and whether it is still moving.
type PTZStatus struct {
	Position PTZPosition `json:"position"`
	// Moving is true while the device reports either axis in MOVING state. It is the
	// only reliable way to know a GotoPreset has finished: presets take seconds on a
	// real dome and the SOAP call returns as soon as the move is ACCEPTED, not when it
	// arrives.
	Moving bool `json:"moving"`
	// HasPosition distinguishes "the camera is at 0,0,0" from "the camera did not tell
	// us where it is". Devices that support presets but not GetStatus positions are
	// common, and rendering their absent position as dead centre would be a lie.
	HasPosition bool   `json:"hasPosition"`
	UTCTime     string `json:"utcTime,omitempty"`
}

// PTZPresetRequest addresses one camera's PTZ service for a preset operation.
type PTZPresetRequest struct {
	DeviceServiceURL string
	PTZServiceURL    string
	ProfileToken     string
	Credentials      Credentials
	// PresetToken selects an existing preset (goto/remove), or, on SetPreset, asks the
	// device to overwrite that preset rather than create a new one.
	PresetToken string
	// PresetName is the label to store with a new or overwritten preset.
	PresetName string
	// Speed, when non-zero, asks the device to move at this fraction of full speed.
	// Zero means "the device's configured default", which is what an operator who has
	// never opened a speed control expects.
	Speed float64
}

// PTZAbsoluteRequest moves to an exact position rather than a saved one.
type PTZAbsoluteRequest struct {
	DeviceServiceURL string
	PTZServiceURL    string
	ProfileToken     string
	Credentials      Credentials
	Position         PTZPosition
	Speed            float64
	// Relative treats Position as a TRANSLATION from wherever the camera is now
	// (ONVIF RelativeMove) instead of an absolute destination.
	Relative bool
}

// GetPresets lists the positions this camera has stored.
func (c *Client) GetPresets(ctx context.Context, req PTZPresetRequest) ([]PTZPreset, error) {
	ptzURL, err := c.ptzEndpoint(ctx, req.DeviceServiceURL, req.PTZServiceURL, req.ProfileToken, req.Credentials)
	if err != nil {
		return nil, err
	}
	body, _, err := c.postSOAP(ctx, ptzURL, getPresetsBody(req.ProfileToken), req.Credentials)
	if err != nil {
		return nil, ptzError("list PTZ presets", body, err)
	}
	return ParsePTZPresets(body)
}

// SetPreset stores the camera's CURRENT position under a name and returns the device's
// token for it.
//
// Store-where-it-is-now, not store-these-coordinates, because that is the only gesture an
// operator can perform accurately: they drive the camera until the picture is right and
// then say "here". Absolute coordinates are for a machine.
func (c *Client) SetPreset(ctx context.Context, req PTZPresetRequest) (string, error) {
	name := strings.TrimSpace(req.PresetName)
	if name == "" {
		return "", errors.New("preset name is required")
	}
	ptzURL, err := c.ptzEndpoint(ctx, req.DeviceServiceURL, req.PTZServiceURL, req.ProfileToken, req.Credentials)
	if err != nil {
		return "", err
	}
	body, _, err := c.postSOAP(ctx, ptzURL, setPresetBody(req.ProfileToken, name, req.PresetToken), req.Credentials)
	if err != nil {
		return "", ptzError("save PTZ preset", body, err)
	}
	token, perr := ParseSetPresetToken(body)
	if perr != nil {
		return "", perr
	}
	// Overwriting an existing preset is allowed to answer with no token: the caller
	// already knows which one it replaced, and several devices return an empty
	// SetPresetResponse in that case. Only a CREATE with no token is a failure — we
	// would have nothing to refer to the new preset by.
	if strings.TrimSpace(token) == "" {
		if strings.TrimSpace(req.PresetToken) != "" {
			return strings.TrimSpace(req.PresetToken), nil
		}
		return "", errors.New("camera stored the preset but returned no preset token")
	}
	return token, nil
}

// GotoPreset sends the camera to a stored position.
func (c *Client) GotoPreset(ctx context.Context, req PTZPresetRequest) error {
	if strings.TrimSpace(req.PresetToken) == "" {
		return errors.New("presetToken is required")
	}
	ptzURL, err := c.ptzEndpoint(ctx, req.DeviceServiceURL, req.PTZServiceURL, req.ProfileToken, req.Credentials)
	if err != nil {
		return err
	}
	body, _, err := c.postSOAP(ctx, ptzURL, gotoPresetBody(req.ProfileToken, req.PresetToken, req.Speed), req.Credentials)
	if err != nil {
		return ptzError("recall PTZ preset", body, err)
	}
	return nil
}

// RemovePreset deletes a stored position from the camera.
func (c *Client) RemovePreset(ctx context.Context, req PTZPresetRequest) error {
	if strings.TrimSpace(req.PresetToken) == "" {
		return errors.New("presetToken is required")
	}
	ptzURL, err := c.ptzEndpoint(ctx, req.DeviceServiceURL, req.PTZServiceURL, req.ProfileToken, req.Credentials)
	if err != nil {
		return err
	}
	body, _, err := c.postSOAP(ctx, ptzURL, removePresetBody(req.ProfileToken, req.PresetToken), req.Credentials)
	if err != nil {
		return ptzError("delete PTZ preset", body, err)
	}
	return nil
}

// GotoHome sends the camera to its home position.
func (c *Client) GotoHome(ctx context.Context, req PTZPresetRequest) error {
	ptzURL, err := c.ptzEndpoint(ctx, req.DeviceServiceURL, req.PTZServiceURL, req.ProfileToken, req.Credentials)
	if err != nil {
		return err
	}
	body, _, err := c.postSOAP(ctx, ptzURL, gotoHomeBody(req.ProfileToken, req.Speed), req.Credentials)
	if err != nil {
		return ptzError("send camera home", body, err)
	}
	return nil
}

// SetHome makes the camera's current position its home position.
func (c *Client) SetHome(ctx context.Context, req PTZPresetRequest) error {
	ptzURL, err := c.ptzEndpoint(ctx, req.DeviceServiceURL, req.PTZServiceURL, req.ProfileToken, req.Credentials)
	if err != nil {
		return err
	}
	body, _, err := c.postSOAP(ctx, ptzURL, setHomeBody(req.ProfileToken), req.Credentials)
	if err != nil {
		return ptzError("set camera home", body, err)
	}
	return nil
}

// PTZGetStatus reads the camera's current position and whether it is still moving.
func (c *Client) PTZGetStatus(ctx context.Context, req PTZPresetRequest) (*PTZStatus, error) {
	ptzURL, err := c.ptzEndpoint(ctx, req.DeviceServiceURL, req.PTZServiceURL, req.ProfileToken, req.Credentials)
	if err != nil {
		return nil, err
	}
	body, _, err := c.postSOAP(ctx, ptzURL, getPTZStatusBody(req.ProfileToken), req.Credentials)
	if err != nil {
		return nil, ptzError("read PTZ status", body, err)
	}
	return ParsePTZStatus(body)
}

// PTZGoto moves to an absolute position, or by a relative translation.
func (c *Client) PTZGoto(ctx context.Context, req PTZAbsoluteRequest) error {
	ptzURL, err := c.ptzEndpoint(ctx, req.DeviceServiceURL, req.PTZServiceURL, req.ProfileToken, req.Credentials)
	if err != nil {
		return err
	}
	verb := "AbsoluteMove"
	action := "move camera to position"
	if req.Relative {
		verb = "RelativeMove"
		action = "nudge camera"
	}
	body, _, err := c.postSOAP(ctx, ptzURL, absoluteMoveBody(verb, req.ProfileToken, req.Position, req.Speed), req.Credentials)
	if err != nil {
		return ptzError(action, body, err)
	}
	return nil
}

// ptzEndpoint validates the profile token and resolves the PTZ service URL. Every call in
// this file needs both, and getting the token check wrong produces a device fault that
// reads like a network problem.
func (c *Client) ptzEndpoint(ctx context.Context, deviceURL, ptzURL, profileToken string, credentials Credentials) (string, error) {
	if strings.TrimSpace(profileToken) == "" {
		return "", errors.New("profileToken is required")
	}
	return c.resolvePTZServiceURL(ctx, deviceURL, ptzURL, credentials)
}

// ptzError keeps the camera's own explanation instead of "status 500".
//
// A SOAP fault is how a device says "that preset does not exist", "the preset store is
// full", or "this profile has no PTZ configuration" — all of which are ordinary answers
// when a person is managing presets, and all of which arrive as HTTP 500. postSOAP turns
// that into "ONVIF SOAP endpoint returned status 500", which tells an operator nothing
// and sends an installer to check the network. The fault text is the whole message.
func ptzError(action string, body []byte, err error) error {
	if reason := ParseSOAPFault(body); reason != "" {
		return fmt.Errorf("%s failed: %s", action, reason)
	}
	return fmt.Errorf("%s failed: %w", action, err)
}

func getPresetsBody(profileToken string) string {
	return fmt.Sprintf(`
    <tptz:GetPresets>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
    </tptz:GetPresets>`, xmlEscape(profileToken))
}

func setPresetBody(profileToken string, name string, presetToken string) string {
	existing := ""
	if strings.TrimSpace(presetToken) != "" {
		existing = fmt.Sprintf(`
      <tptz:PresetToken>%s</tptz:PresetToken>`, xmlEscape(presetToken))
	}
	return fmt.Sprintf(`
    <tptz:SetPreset>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:PresetName>%s</tptz:PresetName>%s
    </tptz:SetPreset>`, xmlEscape(profileToken), xmlEscape(name), existing)
}

func gotoPresetBody(profileToken string, presetToken string, speed float64) string {
	return fmt.Sprintf(`
    <tptz:GotoPreset>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:PresetToken>%s</tptz:PresetToken>%s
    </tptz:GotoPreset>`, xmlEscape(profileToken), xmlEscape(presetToken), ptzSpeedElement("tptz:Speed", speed))
}

func removePresetBody(profileToken string, presetToken string) string {
	return fmt.Sprintf(`
    <tptz:RemovePreset>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:PresetToken>%s</tptz:PresetToken>
    </tptz:RemovePreset>`, xmlEscape(profileToken), xmlEscape(presetToken))
}

func gotoHomeBody(profileToken string, speed float64) string {
	return fmt.Sprintf(`
    <tptz:GotoHomePosition>
      <tptz:ProfileToken>%s</tptz:ProfileToken>%s
    </tptz:GotoHomePosition>`, xmlEscape(profileToken), ptzSpeedElement("tptz:Speed", speed))
}

func setHomeBody(profileToken string) string {
	return fmt.Sprintf(`
    <tptz:SetHomePosition>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
    </tptz:SetHomePosition>`, xmlEscape(profileToken))
}

func getPTZStatusBody(profileToken string) string {
	return fmt.Sprintf(`
    <tptz:GetStatus>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
    </tptz:GetStatus>`, xmlEscape(profileToken))
}

func absoluteMoveBody(verb string, profileToken string, pos PTZPosition, speed float64) string {
	vector := fmt.Sprintf(`
        <tt:PanTilt x="%s" y="%s"/>
        <tt:Zoom x="%s"/>`, ptzFloat(pos.Pan), ptzFloat(pos.Tilt), ptzFloat(pos.Zoom))
	// AbsoluteMove names the destination "Position"; RelativeMove names the offset
	// "Translation". Sending the wrong element name is accepted by lenient devices and
	// silently ignored by strict ones, which looks exactly like a camera that cannot move.
	field := "Position"
	if verb == "RelativeMove" {
		field = "Translation"
	}
	return fmt.Sprintf(`
    <tptz:%s>
      <tptz:ProfileToken>%s</tptz:ProfileToken>
      <tptz:%s>%s
      </tptz:%s>%s
    </tptz:%s>`, verb, xmlEscape(profileToken), field, vector, field, ptzSpeedElement("tptz:Speed", speed), verb)
}

// ptzSpeedElement renders an optional speed vector.
//
// Omitted entirely when zero rather than sent as 0. A speed of zero is a valid ONVIF
// vector meaning "do not move", so defaulting it to 0 would produce a preset recall that
// is accepted and never arrives — the hardest kind of failure to diagnose, because every
// layer reports success.
func ptzSpeedElement(element string, speed float64) string {
	if speed <= 0 {
		return ""
	}
	if speed > 1 {
		speed = 1
	}
	value := strconv.FormatFloat(speed, 'f', 3, 64)
	return fmt.Sprintf(`
      <%s>
        <tt:PanTilt x="%s" y="%s"/>
        <tt:Zoom x="%s"/>
      </%s>`, element, value, value, value, element)
}
