package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/control"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Critical-clip archive: take a copy of the footage that matters OFF the appliance.
//
// A mymatasan node is the sole holder of its own recordings. That is fine until the
// failure mode a customer actually fears — the appliance is stolen, burned, submerged, or
// wiped by whoever set off the alarm — which destroys the evidence of precisely the event
// it was recording. Retention, tamper detection and continuity monitoring all assume the
// box survives. This is the one feature that does not.
//
// PULL, NOT PUSH — the one real design decision here, and it goes against the phrasing in
// the hardening plan, so here is the reasoning.
//
// A push needs the node to hold a durable queue of clips it has not yet delivered, retry
// them against a control plane that may be down for a week, and manage the disk that
// queue consumes — on the appliance whose disk is already the scarce resource and already
// has a guard that pauses RECORDING when it fills. All of that is new machinery on the
// device we are least able to debug remotely.
//
// A pull needs none of it. The control plane already learns of every node alert the
// instant it happens (the notification is forwarded live up the control channel) and
// again on reconnect (the 72h replay backfills whatever was missed while the node was
// away), so it always knows what it owes. The queue lives here, where the database is,
// where the operator is, and where "this clip was never retrieved" can actually be shown
// to somebody. Retry is not a mechanism, it is just a row that is still pending.
//
// And the transport already exists: recording playback over the tunnel (see
// apis/recording_stream.go) fetches bounded byte ranges from the node's segment download,
// because a whole clip cannot fit in one control-channel message. This walks the same
// path with the same chunk size. No new listener, no new protocol, no new port.

const (
	// clipChunkBytes bounds each tunneled range. Matches the playback proxy's chunk: the
	// control channel caps a single frame at 16 MiB and the response carries headers and
	// JSON framing besides the payload.
	clipChunkBytes = 8 << 20
	// clipMaxBytes refuses an implausibly large clip outright. A rule's pre/post roll is
	// measured in seconds, so anything approaching this is a misconfiguration or a
	// runaway, and the control plane must not fill its disk finding out.
	clipMaxBytes = 512 << 20
	// clipMaxAttempts is how many times a fetch that REACHED the node may fail before the
	// job is marked failed. A node that is simply offline never burns one.
	clipMaxAttempts = 6
	// clipRetryBase is the first backoff; it doubles per attempt.
	clipRetryBase = 2 * time.Minute
	// clipCutoff is how long after the event the control plane keeps waiting for a clip
	// that has never appeared. An event clip is cut after its post-roll, so a job created
	// when the alert arrives is deliberately early — but a clip that has not appeared
	// within this window is not coming (recording disabled on that camera, the node
	// restarted mid-cut, ffmpeg failed), and saying so is more useful than retrying
	// silently for a week.
	clipCutoff = 30 * time.Minute
	// clipRetentionDays is how long archived media is kept. The row outlives the media.
	clipRetentionDays = 90
	// clipArchiveMaxBytes caps the whole archive. On reaching it the archive STOPS and
	// says so rather than evicting the oldest clip to make room: this is evidence, and a
	// system that silently deletes evidence to keep ingesting more of it is worse than
	// one that stops and complains.
	clipArchiveMaxBytes = int64(20) << 30
	// clipListPageSize bounds a listing read.
	clipListPageSize = 500
)

// ErrClipUnavailable is returned when an archived clip's media is gone (expired, or the
// job never completed).
var ErrClipUnavailable = errors.New("this clip's media is not available")

// IClipArchiveService owns the fleet's copy of the footage that matters.
type IClipArchiveService interface {
	// Consider enqueues an archive job when the node's alert asks for one. Called for
	// every ingested node notification; the vast majority are not flagged and cost one
	// map lookup. Idempotent per (nodeId, alertId).
	Consider(ctx context.Context, nodeID string, n notification.Notification) (*entities.ArchivedClip, bool)
	// RunOnce works the queue: one pass over the jobs that are due. Leader-gated by the
	// caller — two instances fetching the same clip would duplicate the traffic and race
	// on the file.
	RunOnce(ctx context.Context)
	// List returns archived clips, newest event first.
	List(ctx context.Context, limit, offset uint64, nodeID string, state string) ([]*entities.ArchivedClip, uint64, error)
	Get(ctx context.Context, id int64) (*entities.ArchivedClip, error)
	// OpenMedia returns a seekable plaintext reader for a stored clip (or its snapshot),
	// plus a cleanup func. Seekable because the browser plays it with Range requests.
	OpenMedia(ctx context.Context, id int64, snapshot bool) (io.ReadSeeker, func(), string, error)
	// Purge applies retention: media older than the window is deleted, its row kept and
	// marked expired. Returns how many were expired.
	Purge(ctx context.Context, now int64) (int, error)
	// Stats reports what the archive is holding, for the operator and for the cap.
	Stats(ctx context.Context) (ClipArchiveStats, error)
}

// ClipArchiveStats is the archive's own health.
type ClipArchiveStats struct {
	Total     int   `json:"total"`
	Stored    int   `json:"stored"`
	Pending   int   `json:"pending"`
	Failed    int   `json:"failed"`
	Expired   int   `json:"expired"`
	UsedBytes int64 `json:"usedBytes"`
	CapBytes  int64 `json:"capBytes"`
	// Full is true when the cap has been reached and archiving has stopped.
	Full bool `json:"full"`
}

// clipNodeLookup is the sliver of the registry this needs: naming a node, and knowing
// whether it is reachable right now.
type clipNodeLookup interface {
	List(ctx context.Context) ([]*entities.ManagedNode, error)
}

type clipArchiveService struct {
	repo   dbsql.IGenericRepo[entities.ArchivedClip]
	sender ControlSender
	nodes  clipNodeLookup
	// connected reports whether a node currently holds a control channel. Without it the
	// worker would burn a retry every pass on every node that is simply away.
	connected func(nodeID string) bool
	cipher    *atrest.Cipher
	dir       string
	// notify raises an operator-visible alert for the things that must not fail quietly:
	// the archive filling up, and a clip giving up. May be nil.
	notify func(n notification.Notification)
	logf   func(string, ...any)

	// mu serialises RunOnce against itself. The ticker and a manual trigger can both
	// arrive, and two passes would claim the same job.
	mu sync.Mutex
}

// NewClipArchiveService builds the archive over the control-plane database. dir is where
// media lands; cipher may be nil (encryption disabled), matching the site service.
func NewClipArchiveService(
	db dbsql.IDbCrud,
	sender ControlSender,
	nodes clipNodeLookup,
	connected func(nodeID string) bool,
	cipher *atrest.Cipher,
	dir string,
	notify func(notification.Notification),
	logf func(string, ...any),
) IClipArchiveService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &clipArchiveService{
		repo:      dbsql.NewGenericRepo[entities.ArchivedClip](db),
		sender:    sender,
		nodes:     nodes,
		connected: connected,
		cipher:    cipher,
		dir:       dir,
		notify:    notify,
		logf:      logf,
	}
}

// --- enqueue -----------------------------------------------------------------------

func (s *clipArchiveService) Consider(ctx context.Context, nodeID string, n notification.Notification) (*entities.ArchivedClip, bool) {
	if s == nil || nodeID == "" || !archiveRequested(n) {
		return nil, false
	}
	alertID := dataInt64(n.Data, notification.DataAlertId)
	if alertID <= 0 {
		// Without the node-local alert id there is no way to find the clip: matching on
		// timestamps would pick a neighbouring event's footage on a busy camera, and an
		// archive that sometimes holds the wrong clip is worse than one that holds none.
		s.logf("clip archive: node %s asked to archive an alert with no alert id; ignoring", nodeID)
		return nil, false
	}

	// Dedup. The same alert reaches the control plane twice by design — live on the
	// control channel, then again through replay-on-reconnect — and the notification
	// dedup upstream does not cover a job we created from the first copy.
	if existing, err := s.byAlert(ctx, nodeID, alertID); err == nil && existing != nil {
		return existing, false
	}

	now := time.Now().Unix()
	eventAt := n.CreatedAt
	if eventAt <= 0 {
		eventAt = now
	}
	clip := entities.ArchivedClip{
		NodeId:     nodeID,
		AlertId:    alertID,
		NodeName:   s.nodeName(ctx, nodeID),
		CameraId:   n.CameraId,
		CameraName: dataString(n.Data, "cameraName"),
		RuleName:   dataString(n.Data, notification.DataRuleName),
		Title:      n.Title,
		EventAt:    eventAt,
		State:      entities.ClipStatePending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	id, err := s.repo.Create(ctx, "", clip)
	if err != nil {
		s.logf("clip archive: could not queue alert %d from node %s: %v", alertID, nodeID, err)
		return nil, false
	}
	clip.Id = int64(id)
	s.logf("clip archive: queued alert %d from node %s (%s)", alertID, nodeID, clip.Title)
	return &clip, true
}

// archiveRequested reports whether this notification carries the node's request to keep a
// copy. Accepts the bool a Go node sends and the string a JSON round-trip could produce.
func archiveRequested(n notification.Notification) bool {
	if n.Data == nil {
		return false
	}
	switch v := n.Data[notification.DataArchiveClip].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	}
	return false
}

func (s *clipArchiveService) byAlert(ctx context.Context, nodeID string, alertID int64) (*entities.ArchivedClip, error) {
	rows, _, err := s.repo.Get(ctx, "", 1, 0, []sqldataenums.Filter{
		{FieldName: "NodeId", Compare: sqldataenums.Equal, Value: nodeID},
		{FieldName: "AlertId", Compare: sqldataenums.Equal, Value: alertID},
	}, nil)
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (s *clipArchiveService) nodeName(ctx context.Context, nodeID string) string {
	if s.nodes == nil {
		return nodeID
	}
	nodes, err := s.nodes.List(ctx)
	if err != nil {
		return nodeID
	}
	for _, n := range nodes {
		if n.NodeId == nodeID {
			if n.Name != "" {
				return n.Name
			}
			return nodeID
		}
	}
	return nodeID
}

// --- the worker --------------------------------------------------------------------

func (s *clipArchiveService) RunOnce(ctx context.Context) {
	if s == nil {
		return
	}
	if !s.mu.TryLock() {
		return
	}
	defer s.mu.Unlock()

	now := time.Now().Unix()
	jobs, err := s.due(ctx, now)
	if err != nil {
		s.logf("clip archive: could not read the queue: %v", err)
		return
	}
	if len(jobs) == 0 {
		return
	}

	stats, err := s.Stats(ctx)
	if err == nil && stats.Full {
		// Loud, once per pass, and NOT a silent stall: an archive that has quietly
		// stopped keeping evidence looks exactly like one with nothing to keep.
		s.raise(notification.Critical, "Clip archive is full",
			fmt.Sprintf("The fleet clip archive has reached its %s limit, so %d clip(s) waiting to be archived cannot be fetched. Free space by removing older archived clips.",
				humanBytes(stats.CapBytes), len(jobs)))
		return
	}

	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}
		s.work(ctx, job)
	}
}

func (s *clipArchiveService) due(ctx context.Context, now int64) ([]*entities.ArchivedClip, error) {
	rows, _, err := s.repo.Get(ctx, "", 50, 0, []sqldataenums.Filter{
		{FieldName: "State", Compare: sqldataenums.Equal, Value: entities.ClipStatePending},
		{FieldName: "NextAttemptAt", Compare: sqldataenums.LessThanOrEqualTo, Value: now},
	}, []sqldataenums.Sorter{{FieldName: "EventAt", Sort: sqldataenums.ASC}})
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	return rows, nil
}

func (s *clipArchiveService) work(ctx context.Context, job *entities.ArchivedClip) {
	now := time.Now().Unix()

	// A node that is away is not a failure. Burning an attempt here would let a week's
	// planned shutdown exhaust every pending clip's retries for a reason that has nothing
	// to do with any of them.
	if s.connected != nil && !s.connected(job.NodeId) {
		return
	}

	// The clip is cut after its post-roll, so "not there yet" is expected for the first
	// minute or two. Past the cutoff it is not coming.
	segID, err := s.findClipSegment(ctx, job)
	if err != nil {
		s.fail(ctx, job, err.Error())
		return
	}
	if segID == 0 {
		if now-job.EventAt > int64(clipCutoff.Seconds()) {
			s.giveUp(ctx, job, "the node never produced an event clip for this alert — recording may be disabled on that camera, or the clip failed to cut")
			return
		}
		s.retryLater(ctx, job, "waiting for the node to finish cutting the event clip", false)
		return
	}

	job.SegmentId = segID
	if err := s.fetch(ctx, job); err != nil {
		s.fail(ctx, job, err.Error())
		return
	}
}

// findClipSegment asks the node for the recording segment that IS this alert's event
// clip. The node stores it as a segment carrying the alert id, so this is an exact
// lookup rather than a time-range guess — on a busy camera a guess picks up a
// neighbouring event's footage, and an archive that sometimes holds the wrong clip is
// worse than one that holds nothing.
func (s *clipArchiveService) findClipSegment(ctx context.Context, job *entities.ArchivedClip) (int64, error) {
	resp, err := s.call(ctx, job.NodeId, http.MethodGet,
		fmt.Sprintf("/api/recording/segments?alertId=%d&limit=5", job.AlertId), nil)
	if err != nil {
		return 0, err
	}
	if resp.Status != http.StatusOK {
		return 0, fmt.Errorf("the node refused the clip lookup (%s)", nodeStatusReason(resp))
	}
	var body struct {
		Result struct {
			Items []struct {
				Id        int64 `json:"id"`
				CameraId  int64 `json:"cameraId"`
				StartedAt int64 `json:"startedAt"`
				EndedAt   int64 `json:"endedAt"`
				FileSize  int64 `json:"fileSize"`
			} `json:"items"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return 0, fmt.Errorf("the node's clip lookup could not be read: %w", err)
	}
	if len(body.Result.Items) == 0 {
		return 0, nil
	}
	it := body.Result.Items[0]
	if it.FileSize > clipMaxBytes {
		return 0, fmt.Errorf("the event clip is %s, larger than the %s this archive accepts",
			humanBytes(it.FileSize), humanBytes(clipMaxBytes))
	}
	job.StartedAt, job.EndedAt = it.StartedAt, it.EndedAt
	if job.CameraId == 0 {
		job.CameraId = it.CameraId
	}
	return it.Id, nil
}

// fetch pulls the clip (and its snapshot) and stores them encrypted.
func (s *clipArchiveService) fetch(ctx context.Context, job *entities.ArchivedClip) error {
	if err := os.MkdirAll(s.nodeDir(job.NodeId), 0o755); err != nil {
		return fmt.Errorf("could not prepare the archive directory: %w", err)
	}

	// Claim the job so a second pass cannot start the same download.
	job.State = entities.ClipStateFetching
	job.Attempts++
	job.UpdatedAt = time.Now().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *job); err != nil {
		return fmt.Errorf("could not claim the job: %w", err)
	}

	tmpPath := filepath.Join(s.nodeDir(job.NodeId), fmt.Sprintf("clip_%d.part", job.Id))
	defer os.Remove(tmpPath)

	sum, size, err := s.pullSegment(ctx, job, tmpPath)
	if err != nil {
		return err
	}

	finalPath := filepath.Join(s.nodeDir(job.NodeId), fmt.Sprintf("clip_%d.mp4", job.Id))
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("could not store the clip: %w", err)
	}
	// Encrypted at rest with the same cipher that protects the fleet CA key and the floor
	// plans. This is decrypted footage of somebody's premises and it now lives on a
	// second machine; the node encrypts its copy, so the fleet's copy cannot be the weak
	// one.
	if s.cipher != nil {
		if err := s.cipher.EncryptFileInPlace(finalPath); err != nil {
			_ = os.Remove(finalPath)
			return fmt.Errorf("could not encrypt the stored clip: %w", err)
		}
	}

	// Best-effort: the snapshot is a nicety, the footage is the point. A missing
	// snapshot must never fail an otherwise complete archive.
	snapPath := s.pullSnapshot(ctx, job)

	job.StoredPath = finalPath
	job.SnapshotPath = snapPath
	job.Sha256 = sum
	job.SizeBytes = size
	job.State = entities.ClipStateStored
	job.LastError = ""
	job.UpdatedAt = time.Now().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *job); err != nil {
		return fmt.Errorf("the clip was stored but the record could not be updated: %w", err)
	}
	s.logf("clip archive: stored alert %d from node %s (%s, %s)",
		job.AlertId, job.NodeId, humanBytes(size), sum[:12])
	return nil
}

// pullSegment walks the node's segment download in bounded byte ranges, writing the
// plaintext to dst and hashing as it goes. Returns the digest and the size.
//
// Chunked because the control channel caps one message; hashed HERE, over the bytes that
// actually arrived, because a digest the source reports about itself proves only that the
// source is consistent with itself.
func (s *clipArchiveService) pullSegment(ctx context.Context, job *entities.ArchivedClip, dst string) (string, int64, error) {
	f, err := os.Create(dst)
	if err != nil {
		return "", 0, fmt.Errorf("could not open the archive file: %w", err)
	}
	defer f.Close()

	hash := sha256.New()
	var written int64
	var total int64 = -1
	path := fmt.Sprintf("/api/recording/segments/%d/download", job.SegmentId)

	for {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		end := written + clipChunkBytes - 1
		resp, err := s.call(ctx, job.NodeId, http.MethodGet, path, map[string]string{
			"Range": fmt.Sprintf("bytes=%d-%d", written, end),
		})
		if err != nil {
			return "", 0, err
		}
		switch resp.Status {
		case http.StatusPartialContent:
			if total < 0 {
				total = parseRangeTotal(resp.Headers["Content-Range"])
			}
		case http.StatusOK:
			// The node ignored the Range (an older build, or a clip it cannot seek). The
			// whole file arrived in one message, which only happens when it fits inside
			// the channel's cap — take it and stop.
			if written > 0 {
				return "", 0, errors.New("the node stopped honouring byte ranges partway through the clip")
			}
			total = int64(len(resp.Body))
		default:
			return "", 0, fmt.Errorf("the node refused the clip download (%s)", nodeStatusReason(resp))
		}

		if len(resp.Body) == 0 {
			break
		}
		if written+int64(len(resp.Body)) > clipMaxBytes {
			return "", 0, fmt.Errorf("the clip exceeded the %s limit this archive accepts", humanBytes(clipMaxBytes))
		}
		if _, err := f.Write(resp.Body); err != nil {
			return "", 0, fmt.Errorf("could not write the archive file: %w", err)
		}
		hash.Write(resp.Body)
		written += int64(len(resp.Body))

		if resp.Status == http.StatusOK || (total > 0 && written >= total) {
			break
		}
	}

	if written == 0 {
		return "", 0, errors.New("the node returned an empty clip")
	}
	// A short read is the failure this whole loop exists to catch: a truncated clip is
	// still a playable video, so nothing downstream would ever notice that the last
	// thirty seconds — the part with the incident in it — are missing.
	if total > 0 && written != total {
		return "", 0, fmt.Errorf("the clip arrived incomplete (%s of %s)", humanBytes(written), humanBytes(total))
	}
	if err := f.Sync(); err != nil {
		return "", 0, fmt.Errorf("could not flush the archive file: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}

// pullSnapshot fetches the alert's still image. Best-effort: returns "" on any failure.
func (s *clipArchiveService) pullSnapshot(ctx context.Context, job *entities.ArchivedClip) string {
	resp, err := s.call(ctx, job.NodeId, http.MethodGet,
		fmt.Sprintf("/api/vision/alerts/%d/snapshot", job.AlertId), nil)
	if err != nil || resp.Status != http.StatusOK || len(resp.Body) == 0 {
		return ""
	}
	// Store an IMAGE or store nothing. A 200 is not a promise: the node answers some
	// error conditions with a JSON envelope, and writing that to a .jpg produces an
	// archive entry that shows a broken image months later and makes an operator doubt
	// everything else in it. Checking the magic bytes costs nothing and the alternative
	// is a file that lies about what it is.
	if !looksLikeImage(resp.Body) {
		s.logf("clip archive: node %s returned a non-image snapshot for alert %d; skipping it",
			job.NodeId, job.AlertId)
		return ""
	}
	path := filepath.Join(s.nodeDir(job.NodeId), fmt.Sprintf("clip_%d.jpg", job.Id))
	if err := os.WriteFile(path, resp.Body, 0o600); err != nil {
		return ""
	}
	if s.cipher != nil {
		if err := s.cipher.EncryptFileInPlace(path); err != nil {
			_ = os.Remove(path)
			return ""
		}
	}
	return path
}

// call tunnels one request to a node, asserting the fleet's own read role.
//
// "operator", not "viewer": on a mymatasan node the viewer role has NO access to
// /api/recording at all (see its RBAC defaults), so a viewer assertion would fail on
// every clip with a 403 that looks like a bug in this code. And not "admin", because
// fetching a clip needs no write authority anywhere — the archive should not hold more
// power over an appliance than the job requires.
func (s *clipArchiveService) call(ctx context.Context, nodeID, method, path string, headers map[string]string) (control.Response, error) {
	if s.sender == nil {
		return control.Response{}, errors.New("the fleet control channel is not available")
	}
	resp, err := s.sender.SendRequest(ctx, nodeID, control.Request{
		Method:  method,
		Path:    path,
		Role:    "operator",
		Actor:   "clip-archive",
		Headers: headers,
	})
	if err != nil {
		if errors.Is(err, ErrNodeOffline) {
			return control.Response{}, errors.New("the node is not connected")
		}
		return control.Response{}, fmt.Errorf("the node could not be reached: %w", err)
	}
	return resp, nil
}

// --- job state transitions ----------------------------------------------------------

func (s *clipArchiveService) retryLater(ctx context.Context, job *entities.ArchivedClip, reason string, countAttempt bool) {
	if countAttempt {
		job.Attempts++
	}
	backoff := clipRetryBase * time.Duration(1<<minInt(job.Attempts, 5))
	job.State = entities.ClipStatePending
	job.NextAttemptAt = time.Now().Add(backoff).Unix()
	job.LastError = reason
	job.UpdatedAt = time.Now().Unix()
	_, _ = s.repo.UpdateById(ctx, "", *job)
}

func (s *clipArchiveService) fail(ctx context.Context, job *entities.ArchivedClip, reason string) {
	if job.Attempts >= clipMaxAttempts {
		s.giveUp(ctx, job, reason)
		return
	}
	s.logf("clip archive: alert %d from node %s: %s (attempt %d)", job.AlertId, job.NodeId, reason, job.Attempts)
	s.retryLater(ctx, job, reason, true)
}

// giveUp marks a job failed AND tells somebody. A clip the fleet was asked to keep and
// could not is the one outcome of this feature that must never be discovered months later
// by an operator looking for footage that was never there.
func (s *clipArchiveService) giveUp(ctx context.Context, job *entities.ArchivedClip, reason string) {
	job.State = entities.ClipStateFailed
	job.LastError = reason
	job.UpdatedAt = time.Now().Unix()
	_, _ = s.repo.UpdateById(ctx, "", *job)
	s.logf("clip archive: GAVE UP on alert %d from node %s: %s", job.AlertId, job.NodeId, reason)
	s.raise(notification.Warning, "Critical clip could not be archived",
		fmt.Sprintf("%s on %s (%s) was flagged to be kept by the fleet, but the clip could not be retrieved: %s",
			firstNonEmpty(job.Title, "An alert"), firstNonEmpty(job.NodeName, job.NodeId),
			firstNonEmpty(job.CameraName, "camera "+strconv.FormatInt(job.CameraId, 10)), reason))
}

func (s *clipArchiveService) raise(sev notification.Severity, title, body string) {
	if s.notify == nil {
		return
	}
	s.notify(notification.Notification{
		Category: notification.CategorySystem,
		Severity: sev,
		Title:    title,
		Body:     body,
		Source:   "clip-archive",
	})
}

// --- reads -------------------------------------------------------------------------

func (s *clipArchiveService) List(ctx context.Context, limit, offset uint64, nodeID, state string) ([]*entities.ArchivedClip, uint64, error) {
	if limit == 0 || limit > clipListPageSize {
		limit = 100
	}
	var filters []sqldataenums.Filter
	if nodeID != "" {
		filters = append(filters, sqldataenums.Filter{FieldName: "NodeId", Compare: sqldataenums.Equal, Value: nodeID})
	}
	if state != "" {
		filters = append(filters, sqldataenums.Filter{FieldName: "State", Compare: sqldataenums.Equal, Value: state})
	}
	rows, total, err := s.repo.Get(ctx, "", limit, offset, filters,
		[]sqldataenums.Sorter{{FieldName: "EventAt", Sort: sqldataenums.DESC}, {FieldName: "Id", Sort: sqldataenums.DESC}})
	if err != nil {
		if isNoResultErr(err) {
			return []*entities.ArchivedClip{}, 0, nil
		}
		return nil, 0, err
	}
	if rows == nil {
		rows = []*entities.ArchivedClip{}
	}
	return rows, total, nil
}

func (s *clipArchiveService) Get(ctx context.Context, id int64) (*entities.ArchivedClip, error) {
	rows, _, err := s.repo.Get(ctx, "", 1, 0,
		[]sqldataenums.Filter{{FieldName: "Id", Compare: sqldataenums.Equal, Value: id}}, nil)
	if err != nil && !isNoResultErr(err) {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (s *clipArchiveService) OpenMedia(ctx context.Context, id int64, snapshot bool) (io.ReadSeeker, func(), string, error) {
	clip, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, "", err
	}
	if clip == nil {
		return nil, nil, "", ErrClipUnavailable
	}
	path, contentType := clip.StoredPath, "video/mp4"
	if snapshot {
		path, contentType = clip.SnapshotPath, "image/jpeg"
	}
	if path == "" || clip.State == entities.ClipStateExpired {
		return nil, nil, "", ErrClipUnavailable
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return nil, nil, "", ErrClipUnavailable
	}

	// Decrypted to a short-lived temp file rather than streamed: the browser plays it
	// with Range requests, which need a seekable source, and the decrypt stream is not
	// seekable. Same approach the node takes for its own encrypted segments.
	if s.cipher != nil {
		tmp, cleanup, derr := s.cipher.DecryptToTempFile(path)
		if derr != nil {
			return nil, nil, "", fmt.Errorf("the archived clip could not be decrypted: %w", derr)
		}
		f, oerr := os.Open(tmp)
		if oerr != nil {
			cleanup()
			return nil, nil, "", ErrClipUnavailable
		}
		return f, func() { f.Close(); cleanup() }, contentType, nil
	}
	f, oerr := os.Open(path)
	if oerr != nil {
		return nil, nil, "", ErrClipUnavailable
	}
	return f, func() { f.Close() }, contentType, nil
}

func (s *clipArchiveService) Stats(ctx context.Context) (ClipArchiveStats, error) {
	out := ClipArchiveStats{CapBytes: clipArchiveMaxBytes}
	rows, _, err := s.repo.Get(ctx, "", clipListPageSize, 0, nil, nil)
	if err != nil && !isNoResultErr(err) {
		return out, err
	}
	for _, c := range rows {
		out.Total++
		switch c.State {
		case entities.ClipStateStored:
			out.Stored++
			out.UsedBytes += c.SizeBytes
		case entities.ClipStateFailed:
			out.Failed++
		case entities.ClipStateExpired:
			out.Expired++
		default:
			out.Pending++
		}
	}
	out.Full = out.UsedBytes >= out.CapBytes
	return out, nil
}

// Purge deletes media past the retention window and marks the row expired. The ROW is
// kept: that an incident was archived, and when, outlives the footage itself, and a
// deleted row would leave an operator unable to tell "we never kept it" from "we kept it
// and it aged out".
func (s *clipArchiveService) Purge(ctx context.Context, now int64) (int, error) {
	if now <= 0 {
		now = time.Now().Unix()
	}
	cutoff := now - int64(clipRetentionDays)*86400
	rows, _, err := s.repo.Get(ctx, "", clipListPageSize, 0, []sqldataenums.Filter{
		{FieldName: "State", Compare: sqldataenums.Equal, Value: entities.ClipStateStored},
		{FieldName: "EventAt", Compare: sqldataenums.LessThan, Value: cutoff},
	}, nil)
	if err != nil && !isNoResultErr(err) {
		return 0, err
	}
	expired := 0
	for _, clip := range rows {
		if clip.StoredPath != "" {
			_ = os.Remove(clip.StoredPath)
		}
		if clip.SnapshotPath != "" {
			_ = os.Remove(clip.SnapshotPath)
		}
		clip.State = entities.ClipStateExpired
		clip.StoredPath = ""
		clip.SnapshotPath = ""
		clip.SizeBytes = 0
		clip.UpdatedAt = now
		if _, uerr := s.repo.UpdateById(ctx, "", *clip); uerr != nil {
			return expired, uerr
		}
		expired++
	}
	return expired, nil
}

func (s *clipArchiveService) nodeDir(nodeID string) string {
	return filepath.Join(s.dir, safePathSegment(nodeID))
}

// --- small helpers -------------------------------------------------------------------

// safePathSegment keeps a node-supplied id from escaping the archive directory. The id is
// generated by the node and asserted over mTLS, so this is defence in depth rather than a
// live threat — but it is one line, and the alternative is a path traversal driven by a
// value this process did not choose.
func safePathSegment(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "node"
	}
	return string(out)
}

// looksLikeImage reports whether b starts with a JPEG or PNG signature. Deliberately a
// signature check rather than a Content-Type check: the header is whatever the other end
// says, and the bytes are what will actually be served back.
func looksLikeImage(b []byte) bool {
	if len(b) < 8 {
		return false
	}
	if b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF {
		return true // JPEG
	}
	// PNG: 89 'P' 'N' 'G' CR LF 1A LF.
	return b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' &&
		b[4] == 0x0D && b[5] == 0x0A && b[6] == 0x1A && b[7] == 0x0A
}

// parseRangeTotal reads the total size out of "bytes 0-8388607/12345678".
func parseRangeTotal(h string) int64 {
	i := strings.LastIndex(h, "/")
	if i < 0 {
		return -1
	}
	v, err := strconv.ParseInt(strings.TrimSpace(h[i+1:]), 10, 64)
	if err != nil {
		return -1
	}
	return v
}

// nodeStatusReason turns a node's refusal into something an operator can act on. A bare
// status code in an archive failure tells them nothing about which of the two machines is
// at fault.
func nodeStatusReason(resp control.Response) string {
	switch resp.Status {
	case http.StatusForbidden, http.StatusUnauthorized:
		return "HTTP 403 — the node refused the fleet's read role; check that this node still has an operator role with recording access"
	case http.StatusNotFound:
		return "HTTP 404 — the node no longer has that clip"
	case http.StatusTooManyRequests:
		return "HTTP 429 — the node is rate-limiting the control plane"
	default:
		return "HTTP " + strconv.Itoa(resp.Status)
	}
}

func dataInt64(data map[string]any, key string) int64 {
	if data == nil {
		return 0
	}
	switch v := data[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		// JSON numbers decode as float64; every id that crosses the fleet link arrives
		// this way.
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}

func dataString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
