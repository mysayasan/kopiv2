package onvif

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const pullMessagesResponse = `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tev:PullMessagesResponse xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
        xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2" xmlns:tt="http://www.onvif.org/ver10/schema">
      <tev:CurrentTime>2026-08-24T10:00:00Z</tev:CurrentTime>
      <tev:TerminationTime>2026-08-24T10:01:00Z</tev:TerminationTime>
      <wsnt:NotificationMessage>
        <wsnt:Topic Dialect="http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet">tns1:Device/Trigger/DigitalInput</wsnt:Topic>
        <wsnt:Message>
          <tt:Message UtcTime="2026-08-24T09:59:58Z" PropertyOperation="Changed">
            <tt:Source><tt:SimpleItem Name="InputToken" Value="DIGIT_INPUT_000"/></tt:Source>
            <tt:Data><tt:SimpleItem Name="LogicalState" Value="true"/></tt:Data>
          </tt:Message>
        </wsnt:Message>
      </wsnt:NotificationMessage>
      <wsnt:NotificationMessage>
        <wsnt:Topic>tns1:Device/Trigger/DigitalInput</wsnt:Topic>
        <wsnt:Message>
          <tt:Message UtcTime="2026-08-24T09:59:00Z" PropertyOperation="Initialized">
            <tt:Source><tt:SimpleItem Name="InputToken" Value="DIGIT_INPUT_001"/></tt:Source>
            <tt:Data><tt:SimpleItem Name="LogicalState" Value="false"/></tt:Data>
          </tt:Message>
        </wsnt:Message>
      </wsnt:NotificationMessage>
      <wsnt:NotificationMessage>
        <wsnt:Topic>tns1:VideoSource/MotionAlarm</wsnt:Topic>
        <wsnt:Message>
          <tt:Message UtcTime="2026-08-24T09:59:59Z" PropertyOperation="Changed">
            <tt:Source><tt:SimpleItem Name="VideoSourceConfigurationToken" Value="VSC_1"/></tt:Source>
            <tt:Data><tt:SimpleItem Name="State" Value="true"/></tt:Data>
          </tt:Message>
        </wsnt:Message>
      </wsnt:NotificationMessage>
      <wsnt:NotificationMessage>
        <wsnt:Topic>tns1:Device/HeartBeat</wsnt:Topic>
        <wsnt:Message>
          <tt:Message UtcTime="2026-08-24T09:59:59Z" PropertyOperation="Changed"/>
        </wsnt:Message>
      </wsnt:NotificationMessage>
    </tev:PullMessagesResponse>
  </s:Body>
</s:Envelope>`

func TestParsePullMessages(t *testing.T) {
	events, sub, err := ParsePullMessages([]byte(pullMessagesResponse))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The heartbeat carries no data and is dropped: passing it on would produce an alert
	// row that says a device did something without saying what.
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d: %+v", len(events), events)
	}

	first := events[0]
	if EventKind(first.Topic) != "input" {
		t.Fatalf("kind = %q, want input", EventKind(first.Topic))
	}
	if first.Operation != "Changed" {
		t.Fatalf("operation = %q", first.Operation)
	}
	if first.SourceToken() != "DIGIT_INPUT_000" {
		t.Fatalf("source token = %q", first.SourceToken())
	}
	state, ok := first.State()
	if !ok || !state {
		t.Fatalf("state = %v ok=%v, want true", state, ok)
	}
	if first.UTCTime.IsZero() {
		t.Fatal("the device's timestamp was lost")
	}

	// INITIALIZED IS NOT AN EVENT. It has to survive parsing so the service layer can use
	// it as a baseline, but it must be distinguishable — otherwise every subscription
	// start raises an alarm for every input that happens to be closed.
	if events[1].Operation != "Initialized" {
		t.Fatalf("the initial-state message must keep its operation, got %q", events[1].Operation)
	}

	// A different vendor spelling of the source key still yields a token.
	if events[2].SourceToken() != "VSC_1" {
		t.Fatalf("motion source token = %q", events[2].SourceToken())
	}
	if EventKind(events[2].Topic) != "motion" {
		t.Fatalf("motion kind = %q", EventKind(events[2].Topic))
	}

	// The lease travels with the messages, so the renewal loop can follow the DEVICE's
	// idea of the deadline rather than its own.
	if sub == nil || sub.Lease() != time.Minute {
		t.Fatalf("lease = %v, want 1m", sub.Lease())
	}
}

func TestEventStateSpellings(t *testing.T) {
	cases := []struct {
		data      map[string]string
		want      bool
		wantKnown bool
	}{
		{data: map[string]string{"LogicalState": "true"}, want: true, wantKnown: true},
		{data: map[string]string{"LogicalState": "false"}, want: false, wantKnown: true},
		{data: map[string]string{"State": "active"}, want: true, wantKnown: true},
		{data: map[string]string{"IsMotion": "1"}, want: true, wantKnown: true},
		{data: map[string]string{"Value": "off"}, want: false, wantKnown: true},
		// Something we do not understand must report "no state" rather than false — the
		// difference between "the door is closed" and "the camera said something we could
		// not read" is the whole value of the feature.
		{data: map[string]string{"Something": "maybe"}, want: false, wantKnown: false},
	}
	for _, tc := range cases {
		got, known := Event{Data: tc.data}.State()
		if got != tc.want || known != tc.wantKnown {
			t.Fatalf("%v -> (%v, %v), want (%v, %v)", tc.data, got, known, tc.want, tc.wantKnown)
		}
	}
}

func TestEventKind(t *testing.T) {
	cases := map[string]string{
		"tns1:Device/Trigger/DigitalInput":     "input",
		"tns1:Device/Trigger/Relay":            "relay",
		"tns1:VideoSource/MotionAlarm":         "motion",
		"tns1:VideoSource/SignalLoss":          "signal-loss",
		"tns1:VideoSource/ImageTooBlurry/-":    "tamper",
		"tns1:RuleEngine/CellMotionDetector/-": "analytics",
		"tns1:Monitoring/ProcessorUsage":       "other",
	}
	for topic, want := range cases {
		if got := EventKind(topic); got != want {
			t.Fatalf("EventKind(%q) = %q, want %q", topic, got, want)
		}
	}
}

func TestSubscriptionLeaseUsesTheDeviceClock(t *testing.T) {
	// A camera whose clock is wrong is common enough that this app has a whole date/time
	// screen. Measuring the lease against OUR clock would renew far too early or far too
	// late on every such camera; the difference between the device's two timestamps is
	// correct whatever its clock says.
	sub := EventSubscription{
		CurrentTime:     time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC),
		TerminationTime: time.Date(2019, 1, 1, 0, 1, 0, 0, time.UTC),
	}
	if sub.Lease() != time.Minute {
		t.Fatalf("lease = %v, want 1m", sub.Lease())
	}
	// An already-expired subscription reports zero, never a negative duration that a
	// caller would happily wait for.
	expired := EventSubscription{
		CurrentTime:     time.Date(2019, 1, 1, 0, 2, 0, 0, time.UTC),
		TerminationTime: time.Date(2019, 1, 1, 0, 1, 0, 0, time.UTC),
	}
	if expired.Lease() != 0 {
		t.Fatalf("expired lease = %v, want 0", expired.Lease())
	}
	if (EventSubscription{}).Lease() != 0 {
		t.Fatal("a subscription with no times must report no lease")
	}
}

func TestCreateSubscriptionWithoutAnAddressIsAnError(t *testing.T) {
	// The subscription then exists on the CAMERA and we can neither pull from it nor
	// cancel it — a leak on the device, invisible from here.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>
			<tev:CreatePullPointSubscriptionResponse xmlns:tev="http://www.onvif.org/ver10/events/wsdl"/>
		</s:Body></s:Envelope>`))
	}))
	defer server.Close()

	client := NewClient()
	if _, err := client.CreatePullPointSubscription(context.Background(), EventRequest{
		EventServiceURL: server.URL, LeaseSeconds: 60,
	}); err == nil {
		t.Fatal("a subscription with no address must fail")
	}
}

func TestSubscriptionCallsCarryWSAddressing(t *testing.T) {
	// Strict devices reject a subscription-manager request with no wsa:To / wsa:Action.
	// Lenient ones accept it, which is worse: the feature works on the camera it was
	// developed against and fails on somebody's site.
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 8192)
		n, _ := r.Body.Read(buf)
		seen = string(buf[:n])
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>
			<tev:PullMessagesResponse xmlns:tev="http://www.onvif.org/ver10/events/wsdl"/>
		</s:Body></s:Envelope>`))
	}))
	defer server.Close()

	client := NewClient()
	if _, _, err := client.PullMessages(context.Background(), PullRequest{
		SubscriptionURL: server.URL, TimeoutSeconds: 1, MessageLimit: 4,
	}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if !strings.Contains(seen, "wsa:Action") || !strings.Contains(seen, "wsa:To") {
		t.Fatalf("the pull carried no WS-Addressing header: %s", seen)
	}
	if !strings.Contains(seen, "PullMessagesRequest") {
		t.Fatalf("the action is not the pull action: %s", seen)
	}
	// One header element, not two: a second <s:Header> is not valid SOAP, and the
	// security header is emitted separately.
	if strings.Count(seen, "<s:Header>") > 1 {
		t.Fatalf("more than one SOAP header: %s", seen)
	}
}

func TestPullMessagesUsesALongPollDeadline(t *testing.T) {
	// PullMessages is a LONG POLL. The client's ordinary 5s timeout would abort every one
	// of them, tearing the subscription down and rebuilding it forever while reporting a
	// network error each time.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		_, _ = w.Write([]byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>
			<tev:PullMessagesResponse xmlns:tev="http://www.onvif.org/ver10/events/wsdl"/>
		</s:Body></s:Envelope>`))
	}))
	defer server.Close()

	client := NewClient()
	client.HTTPClient = &http.Client{Timeout: 300 * time.Millisecond}
	if _, _, err := client.PullMessages(context.Background(), PullRequest{
		SubscriptionURL: server.URL, TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("a slow long poll must not be cut off by the ordinary client timeout: %v", err)
	}

	// ...and the shared client is not mutated by that: an ordinary call still fails fast,
	// or a camera that has gone away would hang a settings page for a minute.
	if client.HTTPClient.Timeout != 300*time.Millisecond {
		t.Fatalf("the shared client's timeout was changed to %v", client.HTTPClient.Timeout)
	}
}
