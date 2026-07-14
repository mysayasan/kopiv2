package codec

import "testing"

func numeric(keys ...string) []Binding {
	b := make([]Binding, 0, len(keys))
	for _, k := range keys {
		b = append(b, Binding{Key: k, Numeric: true})
	}
	return b
}

func find(t *testing.T, samples []Sample, key string) Sample {
	t.Helper()
	for _, s := range samples {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("no sample for %q in %+v", key, samples)
	return Sample{}
}

func TestDecodeJSON_Zigbee2MQTTShape(t *testing.T) {
	payload := []byte(`{"temperature":21.4,"humidity":55,"battery":92,"linkquality":120}`)
	got, err := DecodeJSON(payload, numeric("temperature", "humidity", "battery"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(got))
	}
	if s := find(t, got, "temperature"); s.Num != 21.4 || !s.IsNum {
		t.Fatalf("temperature: %+v", s)
	}
}

// A key that is ABSENT must yield no sample at all. Zeroing it would write a false reading —
// and a device that reports temperature and battery in one message, then only temperature in
// the next, has not set its battery to 0%. That would fire a low-battery alert on a healthy
// device, every single message.
func TestDecodeJSON_AbsentKeyIsSkippedNotZeroed(t *testing.T) {
	payload := []byte(`{"temperature":21.4}`)
	got, err := DecodeJSON(payload, numeric("temperature", "battery"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("an absent key must produce NO sample, got %+v", got)
	}
}

// Zigbee2MQTT really does send "unavailable" for a numeric field. A fabricated 0 is a reading,
// and 0 degrees is not the same thing as "no reading".
func TestDecodeJSON_UnparseableNumberIsNotZero(t *testing.T) {
	payload := []byte(`{"temperature":"unavailable","humidity":55}`)
	got, err := DecodeJSON(payload, numeric("temperature", "humidity"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "humidity" {
		t.Fatalf(`"unavailable" must yield no sample rather than 0, got %+v`, got)
	}
}

func TestDecodeJSON_DottedPath(t *testing.T) {
	payload := []byte(`{"aenergy":{"total":1234.5},"apower":42.0}`)
	got, err := DecodeJSON(payload, []Binding{
		{Key: "energy", Path: "aenergy.total", Numeric: true},
		{Key: "power", Path: "apower", Numeric: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := find(t, got, "energy"); s.Num != 1234.5 {
		t.Fatalf("dotted path: %+v", s)
	}
}

// Devices in the field are not consistent about booleans: a contact sensor may send true, or
// "ON", or "open". They all mean the same thing, and they all have to chart as 1.
func TestDecodeJSON_BooleanSpellings(t *testing.T) {
	cases := map[string]float64{
		`{"contact":true}`:     1,
		`{"contact":false}`:    0,
		`{"contact":"ON"}`:     1,
		`{"contact":"off"}`:    0,
		`{"contact":"open"}`:   1,
		`{"contact":"closed"}`: 0,
		`{"contact":"1"}`:      1,
		`{"contact":"0"}`:      0,
	}
	for payload, want := range cases {
		got, err := DecodeJSON([]byte(payload), numeric("contact"))
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: decode failed (%v, %+v)", payload, err, got)
		}
		if got[0].Num != want {
			t.Fatalf("%s: expected %v, got %v", payload, want, got[0].Num)
		}
	}
}

func TestDecodeJSON_MalformedPayloadIsAnError(t *testing.T) {
	if _, err := DecodeJSON([]byte(`not json at all`), numeric("temperature")); err == nil {
		t.Fatal("a payload that is not JSON must be reported, not silently ignored")
	}
}

// A string-typed key keeps its string and is never coerced into a number — the badge id from
// an access reader is not an integer to be averaged.
func TestDecodeJSON_StringKeyStaysAString(t *testing.T) {
	got, err := DecodeJSON([]byte(`{"badge":"04A2B9C1"}`), []Binding{{Key: "badge"}})
	if err != nil || len(got) != 1 {
		t.Fatalf("decode: %v %+v", err, got)
	}
	if got[0].Str != "04A2B9C1" || got[0].IsNum {
		t.Fatalf("a string key must stay a string: %+v", got[0])
	}
}

func TestDecodeRaw_BareValueOnItsOwnTopic(t *testing.T) {
	s, ok := DecodeRaw([]byte("21.4"), Binding{Key: "temperature", Numeric: true})
	if !ok || s.Num != 21.4 {
		t.Fatalf("a bare value payload must decode: %+v %v", s, ok)
	}
}
