package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mysayasan/kopiv2/domain/notification"
)

type fakeForwarder struct {
	kind  string
	body  []byte
	calls int
}

func (f *fakeForwarder) ForwardEvent(kind string, body []byte) {
	f.kind = kind
	f.body = body
	f.calls++
}

func TestControlEventSinkForwardsNotification(t *testing.T) {
	f := &fakeForwarder{}
	sink := NewControlEventSink(f)
	if sink.Name() != "control-channel" {
		t.Fatalf("Name() = %q", sink.Name())
	}

	n := notification.Notification{Title: "Intruder", Category: notification.CategoryVisionAlert, Severity: notification.Critical, CameraId: 7}
	if err := sink.Send(context.Background(), n); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if f.calls != 1 || f.kind != "notification" {
		t.Fatalf("forwarder calls=%d kind=%q", f.calls, f.kind)
	}
	var got notification.Notification
	if err := json.Unmarshal(f.body, &got); err != nil {
		t.Fatalf("unmarshal forwarded body: %v", err)
	}
	if got.Title != "Intruder" || got.CameraId != 7 {
		t.Fatalf("forwarded notification mismatch: %+v", got)
	}
}
