package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/control"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeClipNode answers tunneled requests the way a real mymatasan node does — including
// honouring Range, which is the whole mechanism under test. A fake that returned the
// whole file in one response would let a broken chunk loop pass.
type fakeClipNode struct {
	clip     []byte
	snapshot []byte
	// segments controls the alert→segment lookup: nil means "the clip is not cut yet".
	segments []map[string]any
	// status overrides the reply status for the download (403, 404, …).
	downloadStatus int
	// truncateAt, when >0, makes the node stop returning bytes after this many — the
	// silent-corruption case a naive loop would store as a valid short video.
	truncateAt int
	// maxRange caps how much the node will return per request, so a test can force
	// several chunks out of a small clip.
	maxRange int
	calls    []string
	offline  bool
}

func (f *fakeClipNode) SendRequest(_ context.Context, _ string, req control.Request) (control.Response, error) {
	f.calls = append(f.calls, req.Method+" "+req.Path)
	if f.offline {
		return control.Response{}, ErrNodeOffline
	}
	switch {
	case strings.HasPrefix(req.Path, "/api/recording/segments?alertId="):
		body, _ := json.Marshal(map[string]any{"result": map[string]any{"items": f.segments}})
		return control.Response{Status: http.StatusOK, Body: body}, nil

	case strings.Contains(req.Path, "/download"):
		if f.downloadStatus != 0 {
			return control.Response{Status: f.downloadStatus}, nil
		}
		start, end := parseByteRangeHeader(req.Headers["Range"])
		limit := f.maxRange
		if limit <= 0 {
			limit = clipChunkBytes
		}
		if end-start+1 > int64(limit) {
			end = start + int64(limit) - 1
		}
		total := int64(len(f.clip))
		if f.truncateAt > 0 && start >= int64(f.truncateAt) {
			// Past the truncation point the node returns an empty body — the file looks
			// like it ended early.
			return control.Response{
				Status:  http.StatusPartialContent,
				Headers: map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", start, start, total)},
			}, nil
		}
		if end >= total {
			end = total - 1
		}
		if start > end {
			return control.Response{Status: http.StatusPartialContent,
				Headers: map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", start, start, total)}}, nil
		}
		return control.Response{
			Status:  http.StatusPartialContent,
			Headers: map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", start, end, total)},
			Body:    f.clip[start : end+1],
		}, nil

	case strings.Contains(req.Path, "/snapshot"):
		if len(f.snapshot) == 0 {
			return control.Response{Status: http.StatusNotFound}, nil
		}
		return control.Response{Status: http.StatusOK, Body: f.snapshot}, nil
	}
	return control.Response{Status: http.StatusNotFound}, nil
}

func parseByteRangeHeader(h string) (int64, int64) {
	var start, end int64
	if _, err := fmt.Sscanf(strings.TrimPrefix(h, "bytes="), "%d-%d", &start, &end); err != nil {
		return 0, clipChunkBytes - 1
	}
	return start, end
}

// clipRepo is an in-memory repo that ACTUALLY APPLIES the filters and sorters the archive
// service builds. That property is the point: every interesting query here is a filter —
// "the pending jobs whose next attempt is due", "this node's row for this alert" — and a
// fake that ignored them would pass just as happily with the filters deleted, which is
// the exact bug it would need to catch.
type clipRepo struct {
	dbsql.IGenericRepo[entities.ArchivedClip]
	rows []entities.ArchivedClip
	next int64
}

func newClipRepo() *clipRepo { return &clipRepo{next: 1} }

func (r *clipRepo) matches(row entities.ArchivedClip, filters []sqldataenums.Filter) bool {
	for _, f := range filters {
		var got int64
		var gotStr string
		isStr := false
		switch f.FieldName {
		case "NodeId":
			gotStr, isStr = row.NodeId, true
		case "State":
			gotStr, isStr = row.State, true
		case "AlertId":
			got = row.AlertId
		case "Id":
			got = row.Id
		case "NextAttemptAt":
			got = row.NextAttemptAt
		case "EventAt":
			got = row.EventAt
		default:
			return false
		}
		if isStr {
			want, _ := f.Value.(string)
			if f.Compare != sqldataenums.Equal || gotStr != want {
				return false
			}
			continue
		}
		var want int64
		switch v := f.Value.(type) {
		case int64:
			want = v
		case int:
			want = int64(v)
		default:
			return false
		}
		switch f.Compare {
		case sqldataenums.Equal:
			if got != want {
				return false
			}
		case sqldataenums.LessThan:
			if got >= want {
				return false
			}
		case sqldataenums.LessThanOrEqualTo:
			if got > want {
				return false
			}
		case sqldataenums.GreaterThanOrEqualTo:
			if got < want {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (r *clipRepo) Get(_ context.Context, _ string, limit uint64, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.ArchivedClip, uint64, error) {
	out := []*entities.ArchivedClip{}
	for i := range r.rows {
		if r.matches(r.rows[i], filters) {
			cp := r.rows[i]
			out = append(out, &cp)
		}
	}
	total := uint64(len(out))
	if offset >= total {
		return []*entities.ArchivedClip{}, total, nil
	}
	out = out[offset:]
	if limit > 0 && uint64(len(out)) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (r *clipRepo) Create(_ context.Context, _ string, m entities.ArchivedClip) (uint64, error) {
	m.Id = r.next
	r.next++
	r.rows = append(r.rows, m)
	return uint64(m.Id), nil
}

func (r *clipRepo) UpdateById(_ context.Context, _ string, m entities.ArchivedClip) (uint64, error) {
	for i := range r.rows {
		if r.rows[i].Id == m.Id {
			r.rows[i] = m
			return 1, nil
		}
	}
	return 0, errNoResultFound
}

type fakeClipNodes struct{ rows []*entities.ManagedNode }

func (f *fakeClipNodes) List(context.Context) ([]*entities.ManagedNode, error) { return f.rows, nil }

type clipFixture struct {
	svc   *clipArchiveService
	repo  *clipRepo
	node  *fakeClipNode
	dir   string
	notes []notification.Notification
}

func newClipFixture(t *testing.T, node *fakeClipNode) *clipFixture {
	t.Helper()
	repo := newClipRepo()
	f := &clipFixture{repo: repo, node: node, dir: t.TempDir()}
	f.svc = &clipArchiveService{
		repo:      repo,
		sender:    node,
		nodes:     &fakeClipNodes{rows: []*entities.ManagedNode{{NodeId: "n1", Name: "Lobby NVR"}}},
		connected: func(string) bool { return !node.offline },
		dir:       f.dir,
		notify:    func(n notification.Notification) { f.notes = append(f.notes, n) },
		logf:      func(string, ...any) {},
	}
	return f
}

func alertNotification(archive bool, alertID int64) notification.Notification {
	data := map[string]any{
		// float64 on purpose: every id that crosses the fleet link has been through JSON.
		notification.DataAlertId:  float64(alertID),
		notification.DataRuleName: "Perimeter gate",
		"cameraName":              "Gate",
	}
	if archive {
		data[notification.DataArchiveClip] = true
	}
	return notification.Notification{
		Category:  notification.CategoryVisionAlert,
		Title:     "Person at the gate",
		CameraId:  7,
		Data:      data,
		CreatedAt: time.Now().Unix(),
	}
}

func clipBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

// Only a FLAGGED alert is archived. Everything else on a busy fleet flows through this
// same path, so a mistake here fills the control plane with every clip from every camera.
func TestOnlyFlaggedAlertsAreArchived(t *testing.T) {
	f := newClipFixture(t, &fakeClipNode{})
	ctx := context.Background()

	if _, queued := f.svc.Consider(ctx, "n1", alertNotification(false, 11)); queued {
		t.Fatal("an unflagged alert was queued for archiving")
	}
	if _, queued := f.svc.Consider(ctx, "n1", alertNotification(true, 12)); !queued {
		t.Fatal("a flagged alert was not queued")
	}
	if len(f.repo.rows) != 1 || f.repo.rows[0].AlertId != 12 {
		t.Fatalf("queue = %+v, want only alert 12", f.repo.rows)
	}
	// The node name is snapshotted, not resolved on read: the clip outlives the node.
	if f.repo.rows[0].NodeName != "Lobby NVR" {
		t.Fatalf("nodeName = %q, want the node's name captured at queue time", f.repo.rows[0].NodeName)
	}
}

// THE CRUX FOR THE OFFLINE CASE. The same alert arrives twice by design — live on the
// control channel, then again through replay-on-reconnect. Archiving it twice doubles
// the storage and shows the operator the same incident twice.
func TestTheSameAlertIsQueuedOnlyOnce(t *testing.T) {
	f := newClipFixture(t, &fakeClipNode{})
	ctx := context.Background()
	n := alertNotification(true, 42)

	if _, queued := f.svc.Consider(ctx, "n1", n); !queued {
		t.Fatal("first delivery was not queued")
	}
	if _, queued := f.svc.Consider(ctx, "n1", n); queued {
		t.Fatal("the replayed copy was queued a second time")
	}
	if len(f.repo.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(f.repo.rows))
	}
	// A DIFFERENT node reporting the same node-local alert id is a different event.
	f.svc.nodes = &fakeClipNodes{rows: []*entities.ManagedNode{{NodeId: "n2", Name: "Dock"}}}
	if _, queued := f.svc.Consider(ctx, "n2", n); !queued {
		t.Fatal("an alert from another node was mistaken for a duplicate")
	}
}

// An alert with no id cannot be archived: finding the clip by timestamp would pick a
// neighbouring event's footage on a busy camera, and an archive that sometimes holds the
// wrong clip is worse than one that holds none.
func TestAnAlertWithNoIdIsNotQueued(t *testing.T) {
	f := newClipFixture(t, &fakeClipNode{})
	n := alertNotification(true, 0)
	delete(n.Data, notification.DataAlertId)
	if _, queued := f.svc.Consider(context.Background(), "n1", n); queued {
		t.Fatal("an alert with no id was queued")
	}
}

// The happy path, end to end: several chunks, hashed over what actually arrived.
func TestFetchWalksTheClipInChunksAndHashesWhatArrived(t *testing.T) {
	payload := clipBytes(5000)
	node := &fakeClipNode{
		clip:     payload,
		snapshot: []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 1, 2, 3},
		maxRange: 1000, // force five chunks out of a small clip
		segments: []map[string]any{{"id": 99, "cameraId": 7, "startedAt": 100, "endedAt": 160, "fileSize": len(payload)}},
	}
	f := newClipFixture(t, node)
	ctx := context.Background()
	f.svc.Consider(ctx, "n1", alertNotification(true, 42))

	f.svc.RunOnce(ctx)

	clip := f.repo.rows[0]
	if clip.State != entities.ClipStateStored {
		t.Fatalf("state = %q (%s), want stored", clip.State, clip.LastError)
	}
	if clip.SizeBytes != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", clip.SizeBytes, len(payload))
	}
	want := sha256.Sum256(payload)
	if clip.Sha256 != hex.EncodeToString(want[:]) {
		t.Fatalf("digest = %s, want the digest of the bytes that arrived", clip.Sha256)
	}
	if clip.SegmentId != 99 || clip.StartedAt != 100 || clip.EndedAt != 160 {
		t.Fatalf("clip window not recorded: %+v", clip)
	}
	// The stored file is really there and really matches.
	stored, err := os.ReadFile(clip.StoredPath)
	if err != nil {
		t.Fatalf("read stored clip: %v", err)
	}
	if string(stored) != string(payload) {
		t.Fatalf("stored %d bytes, want the %d fetched", len(stored), len(payload))
	}
	if clip.SnapshotPath == "" {
		t.Fatal("the snapshot was not stored")
	}
	// Several ranged calls, not one — otherwise the chunk loop is not being exercised.
	downloads := 0
	for _, c := range node.calls {
		if strings.Contains(c, "/download") {
			downloads++
		}
	}
	if downloads < 5 {
		t.Fatalf("download calls = %d, want the clip walked in chunks", downloads)
	}
}

// THE MOST DANGEROUS FAILURE THIS CODE CAN HAVE. A truncated clip is still a playable
// video: nothing downstream would ever notice that the last thirty seconds — the part
// with the incident in it — are missing. It must fail loudly, not store a short file.
func TestATruncatedClipIsNeverStored(t *testing.T) {
	payload := clipBytes(5000)
	node := &fakeClipNode{
		clip:       payload,
		maxRange:   1000,
		truncateAt: 2000,
		segments:   []map[string]any{{"id": 99, "cameraId": 7, "fileSize": len(payload)}},
	}
	f := newClipFixture(t, node)
	ctx := context.Background()
	f.svc.Consider(ctx, "n1", alertNotification(true, 42))

	f.svc.RunOnce(ctx)

	clip := f.repo.rows[0]
	if clip.State == entities.ClipStateStored {
		t.Fatalf("a truncated clip was stored as complete (%d bytes)", clip.SizeBytes)
	}
	if !strings.Contains(clip.LastError, "incomplete") {
		t.Fatalf("error = %q, want it to name the truncation", clip.LastError)
	}
	// And nothing partial is left lying around pretending to be evidence.
	entries, _ := os.ReadDir(f.dir + "/n1")
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".mp4") {
			t.Fatalf("a partial clip file survived: %s", e.Name())
		}
	}
}

// A node that is simply away must not burn a retry. Otherwise a week's planned shutdown
// exhausts every pending clip's attempts and marks them all failed for a reason that has
// nothing to do with any of them.
func TestAnOfflineNodeDoesNotBurnRetries(t *testing.T) {
	node := &fakeClipNode{offline: true}
	f := newClipFixture(t, node)
	ctx := context.Background()
	f.svc.Consider(ctx, "n1", alertNotification(true, 42))

	for i := 0; i < 10; i++ {
		f.svc.RunOnce(ctx)
	}
	clip := f.repo.rows[0]
	if clip.Attempts != 0 {
		t.Fatalf("attempts = %d, want 0 while the node is offline", clip.Attempts)
	}
	if clip.State != entities.ClipStatePending {
		t.Fatalf("state = %q, want it still pending", clip.State)
	}
	if len(f.notes) != 0 {
		t.Fatalf("an offline node raised %d alert(s); it is not a failure yet", len(f.notes))
	}

	// And when it comes back, the clip is fetched — the "retry queue for the offline
	// case" is just a row that is still pending.
	node.offline = false
	node.clip = clipBytes(64)
	node.segments = []map[string]any{{"id": 5, "fileSize": 64}}
	f.svc.RunOnce(ctx)
	if f.repo.rows[0].State != entities.ClipStateStored {
		t.Fatalf("state = %q after the node returned, want stored", f.repo.rows[0].State)
	}
}

// A clip that never appears is given up on — with an alert. An operator discovering
// months later that footage they thought was kept was never fetched is the worst outcome
// this feature has.
func TestAClipThatNeverAppearsGivesUpLoudly(t *testing.T) {
	f := newClipFixture(t, &fakeClipNode{segments: nil})
	ctx := context.Background()
	clipRow, _ := f.svc.Consider(ctx, "n1", alertNotification(true, 42))

	// Before the cutoff: patience, no attempt burned, nothing raised.
	f.svc.RunOnce(ctx)
	if f.repo.rows[0].State != entities.ClipStatePending {
		t.Fatalf("state = %q, want pending while the node is still cutting", f.repo.rows[0].State)
	}
	if len(f.notes) != 0 {
		t.Fatal("gave up before the cutoff")
	}

	// Past the cutoff: fail, and say so.
	clipRow.EventAt = time.Now().Add(-2 * clipCutoff).Unix()
	clipRow.NextAttemptAt = 0
	_, _ = f.repo.UpdateById(ctx, "", *clipRow)
	f.svc.RunOnce(ctx)

	got := f.repo.rows[0]
	if got.State != entities.ClipStateFailed {
		t.Fatalf("state = %q, want failed", got.State)
	}
	if len(f.notes) != 1 {
		t.Fatalf("raised %d alert(s), want exactly 1", len(f.notes))
	}
	if !strings.Contains(f.notes[0].Body, "could not be retrieved") {
		t.Fatalf("alert body = %q", f.notes[0].Body)
	}
	if !strings.Contains(got.LastError, "recording may be disabled") {
		t.Fatalf("error = %q, want it to suggest the likely cause", got.LastError)
	}
}

// A node refusing the fleet's asserted role must produce an error that names the cause.
// A bare "403" in an archive failure tells an operator nothing about which of the two
// machines is at fault.
func TestARefusedRoleIsReportedInWordsAnOperatorCanActrOn(t *testing.T) {
	node := &fakeClipNode{
		segments:       []map[string]any{{"id": 99, "fileSize": 100}},
		downloadStatus: http.StatusForbidden,
	}
	f := newClipFixture(t, node)
	ctx := context.Background()
	f.svc.Consider(ctx, "n1", alertNotification(true, 42))
	f.svc.RunOnce(ctx)

	got := f.repo.rows[0].LastError
	if !strings.Contains(got, "403") || !strings.Contains(got, "operator role") {
		t.Fatalf("error = %q, want it to name the 403 and the role it needs", got)
	}
}

// Retention removes the media and KEEPS the row. A deleted row leaves an operator unable
// to tell "we never kept it" from "we kept it and it aged out".
func TestRetentionRemovesMediaButKeepsTheRecord(t *testing.T) {
	node := &fakeClipNode{clip: clipBytes(128), segments: []map[string]any{{"id": 5, "fileSize": 128}}}
	f := newClipFixture(t, node)
	ctx := context.Background()
	f.svc.Consider(ctx, "n1", alertNotification(true, 42))
	f.svc.RunOnce(ctx)

	stored := &f.repo.rows[0]
	path := stored.StoredPath
	if stored.State != entities.ClipStateStored {
		t.Fatalf("setup failed: state %q (%s)", stored.State, stored.LastError)
	}

	// Age the event past the window.
	stored.EventAt = time.Now().Add(-time.Duration(clipRetentionDays+1) * 24 * time.Hour).Unix()
	_, _ = f.repo.UpdateById(ctx, "", *stored)

	n, err := f.svc.Purge(ctx, time.Now().Unix())
	if err != nil || n != 1 {
		t.Fatalf("Purge = %d, %v; want 1", n, err)
	}
	got := f.repo.rows[0]
	if got.State != entities.ClipStateExpired {
		t.Fatalf("state = %q, want expired", got.State)
	}
	if got.EventAt == 0 || got.Title == "" {
		t.Fatal("the record of the incident was lost with the media")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("the media file survived retention")
	}
	// And a read of expired media says so rather than 500ing.
	if _, _, _, err := f.svc.OpenMedia(ctx, got.Id, false); err != ErrClipUnavailable {
		t.Fatalf("OpenMedia on expired media = %v, want ErrClipUnavailable", err)
	}
}

// A stored clip reads back byte-identical through OpenMedia — the path a browser uses.
func TestStoredClipReadsBackIntact(t *testing.T) {
	payload := clipBytes(2048)
	node := &fakeClipNode{clip: payload, maxRange: 700, segments: []map[string]any{{"id": 5, "fileSize": len(payload)}}}
	f := newClipFixture(t, node)
	ctx := context.Background()
	f.svc.Consider(ctx, "n1", alertNotification(true, 42))
	f.svc.RunOnce(ctx)

	reader, cleanup, contentType, err := f.svc.OpenMedia(ctx, f.repo.rows[0].Id, false)
	if err != nil {
		t.Fatalf("OpenMedia: %v", err)
	}
	defer cleanup()
	if contentType != "video/mp4" {
		t.Fatalf("contentType = %q", contentType)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("read back %d bytes, want the %d archived", len(got), len(payload))
	}
}

// The archive stops rather than evicting. A system that silently deletes evidence to keep
// ingesting more of it is worse than one that stops and complains.
func TestAFullArchiveStopsAndSaysSo(t *testing.T) {
	node := &fakeClipNode{clip: clipBytes(128), segments: []map[string]any{{"id": 5, "fileSize": 128}}}
	f := newClipFixture(t, node)
	ctx := context.Background()

	// One stored clip that already fills the cap, plus a pending job.
	_, _ = f.repo.Create(ctx, "", entities.ArchivedClip{
		NodeId: "n1", AlertId: 1, State: entities.ClipStateStored, SizeBytes: clipArchiveMaxBytes,
	})
	f.svc.Consider(ctx, "n1", alertNotification(true, 42))

	f.svc.RunOnce(ctx)

	var pending *entities.ArchivedClip
	for i := range f.repo.rows {
		if f.repo.rows[i].AlertId == 42 {
			pending = &f.repo.rows[i]
		}
	}
	if pending.State != entities.ClipStatePending {
		t.Fatalf("state = %q, want it left pending rather than fetched", pending.State)
	}
	if len(f.notes) != 1 || !strings.Contains(f.notes[0].Title, "full") {
		t.Fatalf("notifications = %+v, want one saying the archive is full", f.notes)
	}
	// Nothing was evicted to make room.
	if f.repo.rows[0].State != entities.ClipStateStored {
		t.Fatalf("the existing clip was evicted: %q", f.repo.rows[0].State)
	}
}

// The archive must not walk out of its own directory on a node-supplied id.
func TestNodeIdCannotEscapeTheArchiveDirectory(t *testing.T) {
	for _, in := range []string{"../../etc", "a/b", "..", ""} {
		got := safePathSegment(in)
		if strings.ContainsAny(got, `/\.`) || got == "" {
			t.Fatalf("safePathSegment(%q) = %q, which can leave the archive directory", in, got)
		}
	}
}

// A 200 is not a promise that the body is an image. The node answers some conditions
// with a JSON envelope, and writing that to a .jpg produces an archive entry that shows
// a broken image months later and makes an operator doubt everything else in it. The
// live bench walked straight into this: an alert raised through the API carries no
// image, and the archive would happily have kept the refusal as the "snapshot".
func TestANonImageSnapshotIsNeverStored(t *testing.T) {
	node := &fakeClipNode{
		clip:     clipBytes(256),
		snapshot: []byte(`{"statsCode":400,"message":"no snapshot for that alert"}`),
		segments: []map[string]any{{"id": 5, "fileSize": 256}},
	}
	f := newClipFixture(t, node)
	ctx := context.Background()
	f.svc.Consider(ctx, "n1", alertNotification(true, 42))
	f.svc.RunOnce(ctx)

	got := f.repo.rows[0]
	if got.State != entities.ClipStateStored {
		t.Fatalf("state = %q (%s), want the FOOTAGE stored regardless", got.State, got.LastError)
	}
	if got.SnapshotPath != "" {
		t.Fatalf("a non-image body was stored as a snapshot: %s", got.SnapshotPath)
	}
	// And asking for the missing snapshot says so rather than serving the refusal.
	if _, _, _, err := f.svc.OpenMedia(ctx, got.Id, true); err != ErrClipUnavailable {
		t.Fatalf("OpenMedia(snapshot) = %v, want ErrClipUnavailable", err)
	}
}

// A real image IS stored — the check must not reject everything.
func TestARealSnapshotIsStoredBesideTheClip(t *testing.T) {
	node := &fakeClipNode{
		clip:     clipBytes(256),
		snapshot: []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 1, 2, 3},
		segments: []map[string]any{{"id": 5, "fileSize": 256}},
	}
	f := newClipFixture(t, node)
	ctx := context.Background()
	f.svc.Consider(ctx, "n1", alertNotification(true, 42))
	f.svc.RunOnce(ctx)

	if f.repo.rows[0].SnapshotPath == "" {
		t.Fatalf("a valid JPEG snapshot was not stored (%s)", f.repo.rows[0].LastError)
	}
}

func TestLooksLikeImageAcceptsOnlyImages(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 1}
	if !looksLikeImage([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 1, 2, 3}) || !looksLikeImage(png) {
		t.Fatal("a real JPEG or PNG was rejected")
	}
	for _, bad := range [][]byte{nil, []byte("{}"), []byte("not an image at all"), {0xFF, 0xD8}} {
		if looksLikeImage(bad) {
			t.Fatalf("%q was accepted as an image", bad)
		}
	}
}
