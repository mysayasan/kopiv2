// Package codec turns a device's raw payload into typed samples.
//
// Every convention worth supporting (Zigbee2MQTT, Tasmota, Shelly, ESPHome) publishes JSON,
// so JSON with a dotted path is the whole of it. A device that publishes a bare value on a
// per-key topic is the "raw" case.
package codec

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Sample is one decoded datapoint.
type Sample struct {
	Key string
	// Num carries numbers and booleans (as 0/1); Str carries strings. Which is meaningful is
	// the caller's business — it knows the key's declared type.
	Num float64
	Str string
	// IsNum reports whether the payload actually yielded a number. A key declared numeric
	// whose payload is "unavailable" (which Zigbee2MQTT really does send) must not silently
	// become 0 — a 0 is a reading, and 0 degrees is not the same as "no reading".
	IsNum bool
}

// Binding tells the decoder where one key lives in the payload.
type Binding struct {
	Key string
	// Path is a dotted path ("battery.level"). Empty means the key name is the field name.
	Path string
	// Numeric is true when the key is declared "number" or "bool".
	Numeric bool
}

// DecodeJSON pulls the bound keys out of a JSON payload.
//
// A key that is absent is SKIPPED, not zeroed: a device that reports temperature and battery
// in one message and only temperature in the next has not set its battery to 0. Returning a
// sample for it would write a false reading and could fire a low-battery alert.
func DecodeJSON(payload []byte, bindings []Binding) ([]Sample, error) {
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, fmt.Errorf("payload is not a JSON object: %w", err)
	}
	samples := make([]Sample, 0, len(bindings))
	for _, b := range bindings {
		path := b.Path
		if strings.TrimSpace(path) == "" {
			path = b.Key
		}
		raw, ok := lookup(doc, path)
		if !ok {
			continue // absent, not zero
		}
		s, ok := coerce(b, raw)
		if !ok {
			continue // present but not convertible (e.g. "unavailable" for a numeric key)
		}
		samples = append(samples, s)
	}
	return samples, nil
}

// DecodeRaw treats the entire payload as one value for one key — the device that publishes
// "21.4" to home/kitchen/temperature.
func DecodeRaw(payload []byte, b Binding) (Sample, bool) {
	return coerce(b, strings.TrimSpace(string(payload)))
}

// lookup walks a dotted path into a decoded JSON object.
func lookup(doc map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var cur any = doc
	for _, part := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// coerce converts a decoded JSON value into a Sample according to the binding's type.
//
// Booleans become 1/0 because that is what makes a door contact chartable and comparable:
// "open for 4 of the last 10 minutes" is a question about a number. The common string spellings
// are accepted too, since devices in the field are not consistent about this.
func coerce(b Binding, raw any) (Sample, bool) {
	s := Sample{Key: b.Key}
	switch v := raw.(type) {
	case bool:
		s.Num, s.IsNum = 0, true
		if v {
			s.Num = 1
		}
		s.Str = strconv.FormatBool(v)
		return s, true
	case float64:
		s.Num, s.IsNum = v, true
		return s, true
	case string:
		if !b.Numeric {
			s.Str = v
			return s, true
		}
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			s.Num, s.IsNum, s.Str = n, true, v
			return s, true
		}
		if n, ok := boolWord(v); ok {
			s.Num, s.IsNum, s.Str = n, true, v
			return s, true
		}
		// A numeric key whose value is "unavailable" / "null" / garbage. Report nothing
		// rather than a fabricated 0.
		return Sample{}, false
	case nil:
		return Sample{}, false
	default:
		// An object or array where a scalar was expected. Not a reading.
		return Sample{}, false
	}
}

// boolWord maps the spellings devices actually use for a boolean onto 1/0.
func boolWord(v string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "true", "open", "yes", "1", "detected", "alarm", "wet":
		return 1, true
	case "off", "false", "closed", "no", "0", "clear", "normal", "dry":
		return 0, true
	}
	return 0, false
}
