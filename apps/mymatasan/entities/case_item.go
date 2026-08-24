package entities

// CaseItem is one piece of evidence inside a CaseFile: a span of footage, a recorded
// sighting, an alert, or a written note.
//
// EVERY KIND IS A SPAN ON A CAMERA, and that is deliberate. A sighting, an alert and a
// hand-made bookmark differ in where they came from, not in what they are — each names a
// camera and a moment, and each resolves to footage through the same rule. Storing them
// as one row shape means the case screen, the footage hold and the export bundle each
// have ONE thing to handle instead of three, and a fourth source added later (a plate
// hit, a face match) is a new SourceKind and nothing else.
//
// A note is the exception that proves it: CameraId 0 and a zero span, carrying only text.
// It is the same row because a note in the middle of a timeline of evidence has to sort
// with the evidence, not live in a separate list beside it.
//
// THE SPAN IS COPIED, NOT REFERENCED. StartedAt/EndedAt are stored on the item even when
// SourceId names an observation or an alert that has its own timestamps. Retention deletes
// observations and alerts on their own schedule; if the case had to read the source row to
// find out what footage to protect, the hold would silently release the moment the index
// row expired — while the case still says the sighting is evidence. The case is the
// record, so the case holds the facts.
type CaseItem struct {
	Id int64 `json:"id" form:"id" query:"id" params:"id" skipWhenInsert:"true" pkey:"true" validate:"required"`
	// CaseId is the owning case. Indexed with StartedAt: every read of a case is "this
	// case's items in time order", and the hold sweep scans by case.
	CaseId int64 `json:"caseId" form:"caseId" query:"caseId" idx:"case_time" validate:"required"`
	// Kind is one of the CaseItemKind values below.
	Kind string `json:"kind" form:"kind" query:"kind"`
	// CameraId is the camera the evidence is on (0 for a note). Indexed with StartedAt
	// because the footage hold asks exactly one question — "does this camera have held
	// evidence overlapping this time" — on every purge decision.
	CameraId int64 `json:"cameraId" form:"cameraId" query:"cameraId" idx:"cam_time"`
	// StartedAt / EndedAt bound the evidence in unix seconds. A bookmarked moment stores
	// a real span (the UI pads it), never a zero-length instant: an instant cannot be
	// exported and cannot be held, and an operator who marks "14:07" means the footage
	// around 14:07.
	StartedAt int64 `json:"startedAt" form:"startedAt" query:"startedAt" idx:"case_time,cam_time"`
	EndedAt   int64 `json:"endedAt" form:"endedAt" query:"endedAt"`
	// Label is a short caption — the object class for a sighting, the rule name for an
	// alert, whatever the operator typed for a bookmark.
	Label string `json:"label" form:"label" query:"label"`
	// Note is the operator's annotation. This is the half of the feature that turns a pile
	// of clips into an argument: "this is the same jacket as item 2".
	Note string `json:"note" form:"note" query:"note"`
	// SourceId is the id of the row this came from within its SourceKind's table (an
	// observation id, an alert id), or 0. Kept for provenance and for opening the source
	// screen; NEVER read to reconstruct the span — see the comment on the type.
	SourceId int64 `json:"sourceId" form:"sourceId" query:"sourceId"`
	// SnapshotPath is a still image belonging to the evidence (an alert's snapshot). Not
	// copied into the case's own storage: it lives with the alert and is exported from
	// there. Empty when there is none.
	SnapshotPath string `json:"snapshotPath" form:"snapshotPath" query:"snapshotPath"`

	AddedBy   int64  `json:"addedBy" form:"addedBy" query:"addedBy"`
	AddedName string `json:"addedName" form:"addedName" query:"addedName"`
	AddedAt   int64  `json:"addedAt" form:"addedAt" query:"addedAt"`
	UpdatedAt int64  `json:"updatedAt" form:"updatedAt" query:"updatedAt"`
}

// Case item kinds.
const (
	// CaseItemFootage is a span of recorded video bookmarked straight off the timeline.
	CaseItemFootage = "footage"
	// CaseItemSighting is an object observation from the metadata index.
	CaseItemSighting = "sighting"
	// CaseItemAlert is an AI alert event.
	CaseItemAlert = "alert"
	// CaseItemNote is commentary with no footage behind it.
	CaseItemNote = "note"
)

// HoldsFootage reports whether this item points at video that must survive retention
// while its case is open. A note holds nothing; anything with a camera and a real span
// does.
func (i *CaseItem) HoldsFootage() bool {
	return i != nil && i.Kind != CaseItemNote && i.CameraId > 0 && i.EndedAt > i.StartedAt
}
