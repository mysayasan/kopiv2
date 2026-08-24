package onvif

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const relayOutputsResponse = `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tds:GetRelayOutputsResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
      <tds:RelayOutputs token="RelayOutputToken_1">
        <tt:Properties>
          <tt:Mode>Monostable</tt:Mode>
          <tt:DelayTime>PT5S</tt:DelayTime>
          <tt:IdleState>closed</tt:IdleState>
        </tt:Properties>
      </tds:RelayOutputs>
      <tds:RelayOutputs token="RelayOutputToken_2">
        <tt:Properties>
          <tt:Mode>Bistable</tt:Mode>
          <tt:DelayTime>PT0S</tt:DelayTime>
          <tt:IdleState>open</tt:IdleState>
        </tt:Properties>
      </tds:RelayOutputs>
      <tds:RelayOutputs token="RelayOutputToken_3">
        <tt:Properties>
          <tt:IdleState>closed</tt:IdleState>
        </tt:Properties>
      </tds:RelayOutputs>
      <tds:RelayOutputs>
        <tt:Properties><tt:Mode>Monostable</tt:Mode></tt:Properties>
      </tds:RelayOutputs>
    </tds:GetRelayOutputsResponse>
  </s:Body>
</s:Envelope>`

func TestParseRelayOutputs(t *testing.T) {
	outputs, err := ParseRelayOutputs([]byte(relayOutputsResponse))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The tokenless output is dropped: the token is the only handle, so it is a row
	// nothing can switch.
	if len(outputs) != 3 {
		t.Fatalf("want 3 outputs, got %d: %+v", len(outputs), outputs)
	}

	if outputs[0].Bistable {
		t.Fatal("a Monostable relay returns to idle by itself")
	}
	if outputs[0].DelaySeconds != 5 {
		t.Fatalf("delay = %d, want 5", outputs[0].DelaySeconds)
	}
	if outputs[1].Bistable != true {
		t.Fatal("a Bistable relay stays where it is put")
	}

	// THE SAFE ASSUMPTION. A device that does not say whether its relay returns to idle
	// must be driven as though it does NOT — because that is the reading where WE are
	// responsible for switching it back off. The other way round leaves a siren running
	// and nobody holding the responsibility for stopping it.
	if !outputs[2].Bistable {
		t.Fatal("a relay whose mode the device did not state must be treated as bistable")
	}
}

func TestParseXSDurationSeconds(t *testing.T) {
	cases := map[string]int{
		"PT5S":    5,
		"PT1M30S": 90,
		"PT2H":    7200,
		"PT0S":    0,
		"":        0,
		"5":       0,
		"P1D":     0,
		"PTxyzS":  0,
	}
	for in, want := range cases {
		if got := parseXSDurationSeconds(in); got != want {
			t.Fatalf("parseXSDurationSeconds(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestSetRelayOutputStateSendsTheRightLogicalState(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 8192)
		n, _ := r.Body.Read(buf)
		seen = string(buf[:n])
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>
			<tds:SetRelayOutputStateResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>
		</s:Body></s:Envelope>`))
	}))
	defer server.Close()

	client := NewClient()
	if err := client.SetRelayOutputState(context.Background(), RelayRequest{
		RelayServiceURL: server.URL, Token: "RelayOutputToken_1", Active: true,
	}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if !strings.Contains(seen, "<tds:LogicalState>active</tds:LogicalState>") {
		t.Fatalf("activate did not send active: %s", seen)
	}
	if err := client.SetRelayOutputState(context.Background(), RelayRequest{
		RelayServiceURL: server.URL, Token: "RelayOutputToken_1", Active: false,
	}); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if !strings.Contains(seen, "<tds:LogicalState>inactive</tds:LogicalState>") {
		t.Fatalf("deactivate did not send inactive: %s", seen)
	}
	// A relay token is device-supplied text going into XML.
	if err := client.SetRelayOutputState(context.Background(), RelayRequest{
		RelayServiceURL: server.URL, Token: `a"&<b`, Active: true,
	}); err != nil {
		t.Fatalf("escaped token: %v", err)
	}
	if !strings.Contains(seen, "a&quot;&amp;&lt;b") {
		t.Fatalf("token not escaped: %s", seen)
	}
}

func TestRelaySurfacesTheDeviceRefusal(t *testing.T) {
	// A camera saying "this relay does not exist" or "the output is not configurable" is
	// an ordinary answer, and its wording is the only thing that tells an installer what
	// to do. It arrives as HTTP 500.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><s:Fault>
			<s:Reason><s:Text xml:lang="en">The relay token is not valid</s:Text></s:Reason>
		</s:Fault></s:Body></s:Envelope>`))
	}))
	defer server.Close()

	err := NewClient().SetRelayOutputState(context.Background(), RelayRequest{
		RelayServiceURL: server.URL, Token: "nope", Active: true,
	})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "relay token is not valid") || strings.Contains(err.Error(), "500") {
		t.Fatalf("the device's explanation was lost: %v", err)
	}
}
