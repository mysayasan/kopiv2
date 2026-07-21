package app

import (
	"encoding/json"
	"testing"

	"github.com/mysayasan/kopiv2/domain/notification"
)

// The node wraps its list payload as {result:{items}}; some layers add {data:{result:{items}}}.
// The replay must read both, and treat garbage as empty rather than panicking.
func TestParseNodeNotifRowsHandlesBothEnvelopes(t *testing.T) {
	plain := []byte(`{"result":{"items":[{"title":"a","cameraId":7,"createdAt":100,"metadata":"{\"__oid\":\"abc\"}"}],"total":1}}`)
	if rows := parseNodeNotifRows(plain); len(rows) != 1 || rows[0].Title != "a" || rows[0].CameraId != 7 {
		t.Fatalf("plain envelope: %+v", rows)
	}
	wrapped := []byte(`{"data":{"result":{"items":[{"title":"b"}]}}}`)
	if rows := parseNodeNotifRows(wrapped); len(rows) != 1 || rows[0].Title != "b" {
		t.Fatalf("wrapped envelope: %+v", rows)
	}
	if rows := parseNodeNotifRows([]byte("not json")); rows != nil {
		t.Fatalf("garbage should parse to nil, got %+v", rows)
	}
	if rows := parseNodeNotifRows([]byte(`{"result":{"items":[]}}`)); len(rows) != 0 {
		t.Fatalf("empty items: %+v", rows)
	}
}

// A pulled row must come back out as a notification carrying the NODE's engine id (from the
// persisted __oid) so dedup keys on the same value the live push uses, and must keep the original
// timestamp + fields + metadata.
func TestNodeRowToNotificationRestoresOriginIdAndFields(t *testing.T) {
	md, _ := json.Marshal(map[string]any{notification.OriginIDKey: "eng-123", "box": []int{1, 2, 3}})
	row := nodeNotifRow{
		Title: "motion", Severity: "warning", CameraId: 7,
		RefType: "alert_event", RefId: 9, CreatedAt: 555, Metadata: string(md),
	}
	n := nodeRowToNotification(row)
	if n.ID != "eng-123" {
		t.Fatalf("origin id not restored: got %q", n.ID)
	}
	if n.CameraId != 7 || n.RefId != 9 || n.CreatedAt != 555 || string(n.Severity) != "warning" {
		t.Fatalf("fields wrong: %+v", n)
	}
	if _, ok := n.Data["box"]; !ok {
		t.Fatalf("metadata data lost: %+v", n.Data)
	}
}

// A row with no persisted origin id (a node too old to carry one) yields an empty ID, which dedup
// treats as "always ingest" — never a crash.
func TestNodeRowToNotificationEmptyMetadata(t *testing.T) {
	n := nodeRowToNotification(nodeNotifRow{Title: "t"})
	if n.ID != "" {
		t.Fatalf("expected empty origin id, got %q", n.ID)
	}
}
