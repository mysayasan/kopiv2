package apis

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mypintusan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
)

// The administrative trail's own tests.
//
// What they are guarding is not "does Record write a row" — the shared package already covers that.
// It is the property this app's version of the trail adds: THAT NOTHING ACCEPTED GOES UNRECORDED.
// Seven live benches on this app found the same shape of defect seven times — a mechanism that was
// correct and that nothing could reach — and a trail wired handler-by-handler is that shape by
// construction, because the failure mode is a route somebody added and forgot to instrument.

// recordingTrail is an in-memory IAuditService. The entries are kept in order so a test can assert
// what was written and, just as importantly, that nothing was written twice.
type recordingTrail struct {
	entries []services.AuditEntry
}

func (t *recordingTrail) Record(_ context.Context, e services.AuditEntry) {
	t.entries = append(t.entries, e)
}

func (t *recordingTrail) List(_ context.Context, limit, offset uint64, f services.AuditFilter) ([]*sharedaudit.AuditLog, uint64, error) {
	var out []*sharedaudit.AuditLog
	for i, e := range t.entries {
		if f.Action != "" && e.Action != f.Action {
			continue
		}
		out = append(out, &sharedaudit.AuditLog{
			Id: int64(i + 1), Action: e.Action, ActorEmail: e.ActorEmail, ActorId: e.ActorId,
			TargetType: e.TargetType, TargetId: e.TargetId, Outcome: e.Outcome, Detail: e.Detail,
			ClientIp: e.ClientIp, UserAgent: e.UserAgent, CreatedAt: 1700000000,
		})
	}
	return out, uint64(len(out)), nil
}

func (t *recordingTrail) PurgeOlderThan(context.Context, int, string) (services.PurgeResult, error) {
	return services.PurgeResult{}, nil
}

// signedIn wraps a handler the way the real auth middleware does, so the audit middleware — which is
// registered INSIDE it — sees a principal exactly as it would in production.
func signedIn(user *sharedservices.AuthenticatedUser) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(sharedapis.WithLocalUser(r.Context(), user)))
		})
	}
}

// testRouter builds a router shaped like the app's: auth outside, the audit middleware inside, and
// whatever handlers the test registers.
func testRouter(t *testing.T, trail *recordingTrail, register func(r *mux.Router, auditor *Auditor)) *mux.Router {
	t.Helper()
	auditor := NewAuditor(trail, nil)
	r := mux.NewRouter()
	r.Use(signedIn(&sharedservices.AuthenticatedUser{Id: 7, Username: "rania", RoleId: 1, IsAdmin: true}))
	r.Use(NewAuditMiddleware(auditor))
	register(r, auditor)
	return r
}

// utf8BOM is spelled as raw bytes rather than as a U+FEFF literal: a Go source file containing an
// actual byte order mark is rejected by the compiler outright ("illegal byte order mark").
const utf8BOM = "\xef\xbb\xbf"

func do(r *mux.Router, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("User-Agent", "bench/1.0")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// THE test. A route that nobody remembered to instrument is still in the trail.
//
// This is the whole reason the middleware exists rather than a rule in a review checklist. Handler
// authors forget; the guarantee has to hold anyway, because the symptom of a missed route is an
// empty query in an investigation two years later, and by then nobody can tell an appliance where
// nothing happened from one where the recording was never wired.
func TestAuditMiddleware_RecordsAnUninstrumentedWrite(t *testing.T) {
	trail := &recordingTrail{}
	r := testRouter(t, trail, func(r *mux.Router, _ *Auditor) {
		r.HandleFunc("/api/some/new/thing", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}).Methods("POST")
	})

	if got := do(r, "POST", "/api/some/new/thing").Code; got != http.StatusOK {
		t.Fatalf("handler status = %d, want 200", got)
	}
	if len(trail.entries) != 1 {
		t.Fatalf("entries = %d, want 1 — an accepted write reached no handler that audited itself and must be recorded anyway", len(trail.entries))
	}
	e := trail.entries[0]
	if e.Action != services.ActionApiWrite || e.Outcome != services.OutcomeSuccess {
		t.Errorf("entry = %s/%s, want %s/%s", e.Action, e.Outcome, services.ActionApiWrite, services.OutcomeSuccess)
	}
	if e.TargetId != "/api/some/new/thing" {
		t.Errorf("targetId = %q, want the request path", e.TargetId)
	}
	// The actor comes from the SESSION. A trail whose actor could be supplied by the caller is not a
	// record, it is a place to write a chosen name next to somebody else's action.
	if e.ActorId != 7 || e.ActorEmail != "rania" {
		t.Errorf("actor = %d/%q, want 7/rania from the session", e.ActorId, e.ActorEmail)
	}
	if e.UserAgent != "bench/1.0" {
		t.Errorf("userAgent = %q, want the request's", e.UserAgent)
	}
}

// A handler that audits itself is recorded ONCE, with its own detail — the generic row must not
// arrive alongside it. Two entries per action would make every count in the trail wrong.
func TestAuditMiddleware_DoesNotDoubleRecord(t *testing.T) {
	trail := &recordingTrail{}
	r := testRouter(t, trail, func(r *mux.Router, auditor *Auditor) {
		r.HandleFunc("/api/grants", func(w http.ResponseWriter, req *http.Request) {
			auditor.Success(req, services.ActionGrantCreate, services.TargetGrant, "42",
				"group \"Night cleaners\" granted door \"Loading bay\"", nil)
			w.WriteHeader(http.StatusOK)
		}).Methods("POST")
	})

	do(r, "POST", "/api/grants")
	if len(trail.entries) != 1 {
		t.Fatalf("entries = %d, want exactly 1", len(trail.entries))
	}
	if trail.entries[0].Action != services.ActionGrantCreate {
		t.Errorf("action = %q, want the handler's own %q", trail.entries[0].Action, services.ActionGrantCreate)
	}
}

// Reading is not an event. A trail that recorded every GET would be a request log, and the entries
// that matter would be one row in ten thousand.
func TestAuditMiddleware_IgnoresReads(t *testing.T) {
	trail := &recordingTrail{}
	r := testRouter(t, trail, func(r *mux.Router, _ *Auditor) {
		r.HandleFunc("/api/doors", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}).Methods("GET")
	})

	do(r, "GET", "/api/doors")
	if len(trail.entries) != 0 {
		t.Fatalf("entries = %d, want 0 — a read is not an administrative act", len(trail.entries))
	}
}

// Marking a feed notification read is exempt, and this test is here so that exemption stays a
// decision somebody made rather than something that quietly grows.
func TestAuditMiddleware_ExemptsNotificationReads(t *testing.T) {
	trail := &recordingTrail{}
	r := testRouter(t, trail, func(r *mux.Router, _ *Auditor) {
		r.HandleFunc("/api/notifications/{id}/read", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}).Methods("POST")
	})

	do(r, "POST", "/api/notifications/9/read")
	if len(trail.entries) != 0 {
		t.Fatalf("entries = %d, want 0 — marking a feed entry read changes nothing about who may enter", len(trail.entries))
	}
}

// A handler's own "administrators only" gate produces a DENIED row.
//
// This is the refusal worth recording: the caller passed the permission matrix and was still turned
// away, which means somebody with real access tried something they are not allowed to do. A
// matrix-level refusal never reaches this middleware and is not claimed to be here.
func TestAuditMiddleware_RecordsAHandlerRefusal(t *testing.T) {
	trail := &recordingTrail{}
	r := testRouter(t, trail, func(r *mux.Router, _ *Auditor) {
		r.HandleFunc("/api/lockdown", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}).Methods("POST")
	})

	do(r, "POST", "/api/lockdown")
	if len(trail.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(trail.entries))
	}
	if got := trail.entries[0].Outcome; got != services.OutcomeDenied {
		t.Errorf("outcome = %q, want %q", got, services.OutcomeDenied)
	}
}

// A validation failure is not a security event. Recording every mistyped form would bury the
// entries that are — the failure mode that makes an audit log worthless without ever looking broken.
func TestAuditMiddleware_IgnoresValidationFailures(t *testing.T) {
	trail := &recordingTrail{}
	r := testRouter(t, trail, func(r *mux.Router, _ *Auditor) {
		r.HandleFunc("/api/schedules", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}).Methods("POST")
	})

	do(r, "POST", "/api/schedules")
	if len(trail.entries) != 0 {
		t.Fatalf("entries = %d, want 0 for a 400", len(trail.entries))
	}
}

// The trail is APPEND-ONLY over HTTP: it serves two GETs and nothing else. A DELETE or a PUT that
// answered anything but 405 would hand the person the trail is about a way to edit it.
func TestAuditApi_OffersNoWriteRoute(t *testing.T) {
	trail := &recordingTrail{}
	r := mux.NewRouter()
	NewAuditApi(r, trail)

	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, path := range []string{"/audit", "/audit/1", "/audit.csv"} {
			w := do(r, method, path)
			if w.Code == http.StatusOK {
				t.Errorf("%s %s answered 200 — the trail must offer no way to change itself", method, path)
			}
		}
	}
}

// The CSV export carries the same rows the listing does, with a header row an auditor can read.
func TestAuditApi_ExportsCSV(t *testing.T) {
	trail := &recordingTrail{}
	trail.Record(context.Background(), services.AuditEntry{
		Action: services.ActionHolidayDelete, ActorEmail: "rania", TargetType: services.TargetHoliday,
		TargetId: "3", Outcome: services.OutcomeSuccess,
		Detail: "holiday \"Deepavali\" on 2026-11-08 removed — the site is no longer closed that day",
	})
	r := mux.NewRouter()
	NewAuditApi(r, trail)

	w := do(r, "GET", "/audit.csv")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("content-type = %q, want text/csv", ct)
	}
	// The BOM is stripped before parsing — it is there for Excel, not for a CSV reader, and a
	// parser that keeps it would report the first column as a byte order mark glued to "time".
	body := strings.TrimPrefix(w.Body.String(), utf8BOM)
	if !strings.HasPrefix(w.Body.String(), utf8BOM) {
		t.Error("the export has no UTF-8 BOM; Excel on Windows will render the em dashes as mojibake")
	}
	rows, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("the export is not valid CSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d (including the header), want 2", len(rows))
	}
	if rows[0][1] != "action" || rows[1][1] != services.ActionHolidayDelete {
		t.Errorf("export = %v / %v, want an action column carrying %q", rows[0], rows[1], services.ActionHolidayDelete)
	}
	// The DETAIL is what makes the export worth handing to somebody. An export of ids would be a
	// table nobody outside the product can read.
	if !strings.Contains(rows[1][7], "Deepavali") {
		t.Errorf("detail column = %q, want the human sentence", rows[1][7])
	}
}

// --- the settings diff -------------------------------------------------------

// A settings save names WHAT MOVED. "Settings changed" is the least useful entry an audit log can
// hold, and the screen posts the whole object on every save, so the request body cannot answer it.
func TestDescribeSettingsChange_NamesTheFieldsThatMoved(t *testing.T) {
	before := services.AccessSettings{Timezone: "Asia/Kuala_Lumpur", Offline: false, TickSeconds: 1}
	after := services.AccessSettings{Timezone: "UTC", Offline: true, TickSeconds: 1}

	detail, meta := describeSettingsChange(before, after)
	for _, want := range []string{"timezone", "Asia/Kuala_Lumpur", "UTC", "offline"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail %q is missing %q", detail, want)
		}
	}
	if strings.Contains(detail, "tickSeconds") {
		t.Errorf("detail %q names a field that did not change", detail)
	}
	if _, ok := meta["timezone"]; !ok {
		t.Error("metadata should carry the before/after for a changed field")
	}
}

// A REKEY is recorded as a fact, never as a value.
//
// The trail is readable by every administrator and is exported to CSV. A site base key in it would
// be a key handed out: anyone holding it can decrypt the RS-485 bus and impersonate a reader, which
// is the exact attack Secure Channel exists to stop.
func TestDescribeSettingsChange_NeverRecordsKeyMaterial(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	before := services.AccessSettings{Buses: []services.BusSettings{{
		Port: "COM3", Readers: []services.ReaderSettings{{Address: 1, SCBK: "old-key-value"}},
	}}}
	after := services.AccessSettings{Buses: []services.BusSettings{{
		Port: "COM3", Readers: []services.ReaderSettings{{Address: 1, SCBK: secret}},
	}}}

	detail, meta := describeSettingsChange(before, after)
	if !strings.Contains(detail, "rekeyed") {
		t.Errorf("detail %q should say the reader was rekeyed", detail)
	}
	if strings.Contains(detail, secret) || strings.Contains(detail, "old-key-value") {
		t.Fatalf("THE DETAIL CONTAINS KEY MATERIAL: %q", detail)
	}
	for k, v := range meta {
		if s, ok := v.(string); ok && (strings.Contains(s, secret) || strings.Contains(s, "old-key-value")) {
			t.Fatalf("THE METADATA CONTAINS KEY MATERIAL under %q", k)
		}
	}
}

// A reader added in the middle of a bus must not report every reader after it as changed. Keyed by
// port and address rather than by position, so a settings diff stays readable.
func TestDescribeSettingsChange_KeysReadersByAddressNotPosition(t *testing.T) {
	before := services.AccessSettings{Buses: []services.BusSettings{{
		Port: "COM3", Readers: []services.ReaderSettings{{Address: 1}, {Address: 3}},
	}}}
	after := services.AccessSettings{Buses: []services.BusSettings{{
		Port: "COM3", Readers: []services.ReaderSettings{{Address: 1}, {Address: 2}, {Address: 3}},
	}}}

	detail, _ := describeSettingsChange(before, after)
	if !strings.Contains(detail, "reader COM3@2 added") {
		t.Errorf("detail %q should name the reader that was added", detail)
	}
	if strings.Contains(detail, "COM3@3") {
		t.Errorf("detail %q reports a reader that did not change", detail)
	}
}
