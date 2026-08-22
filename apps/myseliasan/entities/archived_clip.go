package entities

// Archive states, in the order a clip moves through them.
const (
	// ClipStatePending — the alert has been seen and the clip is wanted. The node may
	// not have finished producing it yet: an event clip is cut after its post-roll, so
	// a job created the instant the alert arrives is deliberately early.
	ClipStatePending = "pending"
	// ClipStateFetching — a worker is pulling the bytes right now. Nothing else may
	// claim the job while it holds this.
	ClipStateFetching = "fetching"
	// ClipStateStored — the footage is on the control plane, encrypted at rest, and its
	// digest matches what the node reported. This is the only state that means the
	// appliance is now expendable.
	ClipStateStored = "stored"
	// ClipStateFailed — retries exhausted. The row is KEPT rather than deleted, because
	// "we tried to keep this and could not" is the single most important thing this
	// feature can tell an operator, and a deleted row tells them nothing at all.
	ClipStateFailed = "failed"
	// ClipStateExpired — retention removed the media; the record of the event and of the
	// fact it was archived survives it.
	ClipStateExpired = "expired"
)

// ArchivedClip is one event clip the fleet has taken a copy of, off the appliance.
//
// WHY: a mymatasan node is the sole holder of its own footage. It sits in the building it
// is watching, which means the failure modes that matter most to a customer — the
// appliance is stolen, burned, submerged, or simply wiped by whoever set off the alarm —
// destroy the evidence of the very event they are evidence of. Recording retention,
// tamper detection and continuity monitoring all assume the box survives; this is the one
// feature that does not.
//
// Deliberately NARROW. Not "upload everything" — the fleet keeps only what a rule was
// explicitly flagged to keep (entities.DetectionRule.ArchiveClip on the node), because a
// control plane holding every clip from every camera is a storage problem pretending to
// be a security feature, and the events that matter would be buried in it.
//
// The row is the record, the file is the payload. A job that never manages to fetch its
// bytes still leaves a row saying an alert on that camera was supposed to be kept and was
// not — see ClipStateFailed.
//
// DELIBERATELY OUTSIDE `.selbackup`, and this is not an oversight to be tidied up later.
// The bundle is a control-plane CONFIGURATION backup — RBAC, the fleet CA, sites, rules —
// small enough to download over a VPN and restore onto a fresh install. This table's rows
// are worthless without their media, and the media is measured in gigabytes; including
// either half alone produces a restore that lists incidents whose footage is missing,
// which is worse than listing nothing. The clip directory is backed up the way footage is
// always backed up: with a file backup of the data directory.
type ArchivedClip struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// NodeId + AlertId together identify the source event, and are the DEDUP key: the
	// same alert can reach the control plane twice (live on the control channel, then
	// again through replay-on-reconnect), and archiving it twice would double the
	// storage and show the operator the same event twice.
	NodeId  string `json:"nodeId" form:"nodeId" query:"nodeId" validate:"required"`
	AlertId int64  `json:"alertId" form:"alertId" query:"alertId"`
	// NodeName / CameraName are snapshotted at archive time, not resolved on read. A
	// clip outlives the node that produced it — that is the entire point — so a report
	// that resolves names live shows "unknown node" for exactly the incidents where the
	// appliance is gone, which are the ones anybody is looking at.
	NodeName   string `json:"nodeName" form:"nodeName" query:"nodeName"`
	CameraId   int64  `json:"cameraId" form:"cameraId" query:"cameraId"`
	CameraName string `json:"cameraName" form:"cameraName" query:"cameraName"`
	// RuleName + Title record WHY this was kept, in the words the operator wrote.
	RuleName string `json:"ruleName" form:"ruleName" query:"ruleName"`
	Title    string `json:"title" form:"title" query:"title"`
	// EventAt is when the alert fired (node clock); StartedAt/EndedAt bound the clip
	// itself, which spans the rule's pre- and post-roll around it.
	EventAt   int64 `json:"eventAt" form:"eventAt" query:"eventAt"`
	StartedAt int64 `json:"startedAt" form:"startedAt" query:"startedAt"`
	EndedAt   int64 `json:"endedAt" form:"endedAt" query:"endedAt"`
	// State is one of the ClipState* values.
	State string `json:"state" form:"state" query:"state"`
	// Attempts counts FETCH attempts that actually reached the node. A node that is
	// simply offline does not burn one — otherwise a week's holiday shutdown would
	// exhaust the retries of every pending clip and mark them all failed for a reason
	// that has nothing to do with them.
	Attempts int `json:"attempts" form:"attempts" query:"attempts"`
	// NextAttemptAt spaces retries out (exponential backoff).
	NextAttemptAt int64 `json:"nextAttemptAt" form:"nextAttemptAt" query:"nextAttemptAt"`
	// LastError is the most recent failure, in a sentence an operator can act on.
	LastError string `json:"lastError" form:"lastError" query:"lastError"`

	// SegmentId is the node-side recording segment the clip was pulled from, kept so a
	// human can correlate the archived copy with the appliance's own record.
	SegmentId int64 `json:"segmentId" form:"segmentId" query:"segmentId"`
	// StoredPath / SnapshotPath are on-disk paths under the control plane's clip
	// directory. Both files are encrypted at rest with the same cipher that protects
	// floor plans and the fleet CA key — this is decrypted footage of somebody's
	// premises, and it now lives on a second machine.
	StoredPath   string `json:"-" form:"storedPath" query:"storedPath"`
	SnapshotPath string `json:"-" form:"snapshotPath" query:"snapshotPath"`
	// SizeBytes is the PLAINTEXT size of the clip.
	SizeBytes int64 `json:"sizeBytes" form:"sizeBytes" query:"sizeBytes"`
	// Sha256 is the digest of the plaintext clip, computed by this control plane as the
	// bytes arrived. It is what makes the archived copy defensible: a copy nobody can
	// prove is the same footage the appliance recorded is a copy, not evidence.
	Sha256 string `json:"sha256" form:"sha256" query:"sha256"`

	CreatedAt int64 `json:"createdAt" form:"createdAt" query:"createdAt"`
	UpdatedAt int64 `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}
