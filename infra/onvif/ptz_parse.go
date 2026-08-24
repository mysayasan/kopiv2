package onvif

import (
	"encoding/xml"
	"strconv"
	"strings"
)

// Response parsing for the PTZ calls in ptz.go, and for SOAP faults.

type ptzPresetsEnvelopeXML struct {
	Body ptzPresetsBodyXML `xml:"Body"`
}

type ptzPresetsBodyXML struct {
	Response ptzPresetsResponseXML `xml:"GetPresetsResponse"`
}

type ptzPresetsResponseXML struct {
	Presets []ptzPresetXML `xml:"Preset"`
}

type ptzPresetXML struct {
	Token    string       `xml:"token,attr"`
	Name     string       `xml:"Name"`
	Position ptzVectorXML `xml:"PTZPosition"`
}

type ptzVectorXML struct {
	PanTilt ptzPanTiltXML `xml:"PanTilt"`
	Zoom    ptzZoomXML    `xml:"Zoom"`
}

type ptzPanTiltXML struct {
	X string `xml:"x,attr"`
	Y string `xml:"y,attr"`
}

type ptzZoomXML struct {
	X string `xml:"x,attr"`
}

type setPresetEnvelopeXML struct {
	Body setPresetBodyXML `xml:"Body"`
}

type setPresetBodyXML struct {
	Response setPresetResponseXML `xml:"SetPresetResponse"`
}

type setPresetResponseXML struct {
	PresetToken string `xml:"PresetToken"`
}

type ptzStatusEnvelopeXML struct {
	Body ptzStatusBodyXML `xml:"Body"`
}

type ptzStatusBodyXML struct {
	Response ptzStatusResponseXML `xml:"GetStatusResponse"`
}

type ptzStatusResponseXML struct {
	Status ptzStatusXML `xml:"PTZStatus"`
}

type ptzStatusXML struct {
	Position   ptzVectorXML     `xml:"Position"`
	MoveStatus ptzMoveStatusXML `xml:"MoveStatus"`
	UTCTime    string           `xml:"UtcTime"`
}

type ptzMoveStatusXML struct {
	PanTilt string `xml:"PanTilt"`
	Zoom    string `xml:"Zoom"`
}

type soapFaultEnvelopeXML struct {
	Body soapFaultBodyXML `xml:"Body"`
}

type soapFaultBodyXML struct {
	Fault soapFaultXML `xml:"Fault"`
}

type soapFaultXML struct {
	// SOAP 1.2 (what ONVIF uses) puts the human text under Reason/Text, and the machine
	// code under Code/Subcode/Value. SOAP 1.1 devices exist in the field and use
	// faultstring instead, so both are read: a fault nobody can read is the same as no
	// fault at all.
	Reason      soapReasonXML `xml:"Reason"`
	Code        soapCodeXML   `xml:"Code"`
	FaultString string        `xml:"faultstring"`
}

type soapReasonXML struct {
	Text []string `xml:"Text"`
}

type soapCodeXML struct {
	Subcode soapSubcodeXML `xml:"Subcode"`
}

type soapSubcodeXML struct {
	Value   string          `xml:"Value"`
	Subcode *soapSubcodeXML `xml:"Subcode"`
}

// ParsePTZPresets parses GetPresetsResponse.
//
// Presets with no token are DROPPED rather than returned with an empty one. The token is
// the only way to recall or delete a preset, so a tokenless entry is a row an operator can
// see and cannot use — and it would make an empty-string tour stop look valid.
func ParsePTZPresets(data []byte) ([]PTZPreset, error) {
	var envelope ptzPresetsEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	presets := make([]PTZPreset, 0, len(envelope.Body.Response.Presets))
	for _, p := range envelope.Body.Response.Presets {
		token := strings.TrimSpace(p.Token)
		if token == "" {
			continue
		}
		preset := PTZPreset{Token: token, Name: strings.TrimSpace(p.Name)}
		// A preset the device did not name is shown by its token rather than as a blank
		// row, because a blank row in a list of places is unusable.
		if preset.Name == "" {
			preset.Name = token
		}
		if pos, ok := ptzVector(p.Position); ok {
			preset.Position = &pos
		}
		presets = append(presets, preset)
	}
	return presets, nil
}

// ParseSetPresetToken parses SetPresetResponse. An empty token is not an error here; see
// Client.SetPreset for why an overwrite is allowed to answer with nothing.
func ParseSetPresetToken(data []byte) (string, error) {
	var envelope setPresetEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return "", err
	}
	return strings.TrimSpace(envelope.Body.Response.PresetToken), nil
}

// ParsePTZStatus parses GetStatusResponse.
func ParsePTZStatus(data []byte) (*PTZStatus, error) {
	var envelope ptzStatusEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	st := envelope.Body.Response.Status
	out := &PTZStatus{UTCTime: strings.TrimSpace(st.UTCTime)}
	if pos, ok := ptzVector(st.Position); ok {
		out.Position = pos
		out.HasPosition = true
	}
	out.Moving = ptzMoving(st.MoveStatus.PanTilt) || ptzMoving(st.MoveStatus.Zoom)
	return out, nil
}

// ParseSOAPFault returns the device's own explanation of a failure, or "" if the body is
// not a fault. Used by the PTZ calls, where a refusal by the camera is an ordinary answer
// and its wording is the only thing that tells an operator what to do about it.
func ParseSOAPFault(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var envelope soapFaultEnvelopeXML
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return ""
	}
	fault := envelope.Body.Fault
	for _, text := range fault.Reason.Text {
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			return trimmed
		}
	}
	if trimmed := strings.TrimSpace(fault.FaultString); trimmed != "" {
		return trimmed
	}
	// No text at all: fall back to the innermost subcode, which on ONVIF devices carries
	// the actionable part ("ter:NoPreset", "ter:TooManyPresets"). Stripped of its
	// namespace prefix, which means nothing to a person.
	code := innermostSubcode(&fault.Code.Subcode)
	if code == "" {
		return ""
	}
	if idx := strings.LastIndex(code, ":"); idx >= 0 && idx+1 < len(code) {
		code = code[idx+1:]
	}
	return code
}

func innermostSubcode(sub *soapSubcodeXML) string {
	value := ""
	for sub != nil {
		if trimmed := strings.TrimSpace(sub.Value); trimmed != "" {
			value = trimmed
		}
		sub = sub.Subcode
	}
	return value
}

// ptzVector converts an ONVIF PTZ vector, reporting whether the device supplied one at
// all. A device that omits PanTilt and Zoom entirely parses as all-zeroes, which is a
// legitimate POSITION — so "did it say anything" has to be answered separately from
// "what did it say".
func ptzVector(v ptzVectorXML) (PTZPosition, bool) {
	pan, panOK := ptzCoord(v.PanTilt.X)
	tilt, tiltOK := ptzCoord(v.PanTilt.Y)
	zoom, zoomOK := ptzCoord(v.Zoom.X)
	if !panOK && !tiltOK && !zoomOK {
		return PTZPosition{}, false
	}
	return PTZPosition{Pan: pan, Tilt: tilt, Zoom: zoom}, true
}

func ptzCoord(value string) (float64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// ptzMoving reads one axis's MoveStatus. ONVIF defines IDLE / MOVING / UNKNOWN; UNKNOWN
// is treated as NOT moving, because a device that never reports MOVING would otherwise
// leave every recall looking permanently in flight and a tour would never take its next
// step.
func ptzMoving(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "MOVING")
}
