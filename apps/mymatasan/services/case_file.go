package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Case files: the investigation, not the recording.
//
// mymatasan is organised the way the appliance thinks — by camera, and by time. An
// incident is neither. It is one person crossing four cameras over eleven minutes, the
// alert that fired halfway through, and two things an operator noticed afterwards. The
// only container the product had for that was a folder of downloaded files on somebody's
// desktop, which is also where the chain of custody ends.
//
// A case is that container, and it exists for three things the loose files cannot do:
//
//  1. It HOLDS ITS FOOTAGE (case_hold.go). Adding footage to an open case takes it out of
//     reach of retention, "Purge now" and the disk-pressure sweeper. On a box with seven
//     day retention this is the difference between a case and a memo.
//  2. It EXPORTS AS ONE BUNDLE (case_export.go) — every clip, one manifest, the hashes,
//     and the case's own audit trail as a chain of custody.
//  3. It is ASSIGNABLE AND CLOSED WITH A STATED OUTCOME, so "what came of that?" has an
//     answer that outlives the person who was on shift.
//
// Everything here is deliberately unremarkable CRUD around those three. The interesting
// parts are the hold and the export; this file is what makes them possible.

// caseListPageSize caps one page of cases.
const caseListPageSize = uint64(100)

// caseMaxItems bounds one case. Not a storage limit — an export of a hundred clips is not
// evidence, it is a haystack, and a case that has grown that far is two cases.
const caseMaxItems = 100

// caseItemMaxSpanSeconds bounds one item's footage span, matching the single-clip export
// cap. A case is made of clips; an item covering a week is a request to export a week.
const caseItemMaxSpanSeconds = int64(4 * 3600)

// CaseActor is who is doing something. Names are copied onto the row rather than joined
// later, so the record survives the account being deleted.
type CaseActor struct {
	Id   int64
	Name string
}

// CaseSummary is one row of the case list: the case plus the counts the list renders.
// The counts are computed from the item table on read rather than kept on the case row —
// a denormalised counter is a number that eventually disagrees with the thing it counts,
// and this one would disagree in a screen an auditor is reading.
type CaseSummary struct {
	*entities.CaseFile
	ItemCount    int `json:"itemCount"`
	FootageItems int `json:"footageItems"`
	NoteCount    int `json:"noteCount"`
}

// CaseItemView is a case item resolved for the screen: the camera's name, and where its
// footage can actually be played from.
type CaseItemView struct {
	*entities.CaseItem
	CameraName string `json:"cameraName"`
	// SegmentId / Seek are the click-to-play target, resolved through the SAME rule the
	// Objects grid and appearance search use (ObservationService.ResolveFootageFor →
	// pickCovering), so a clip opened from a case is the clip opened from anywhere else.
	SegmentId int64 `json:"segmentId"`
	Seek      int64 `json:"seek"`
	// FootageStartsAt is when this item's footage actually begins, set only when that is
	// LATER than the item itself — a bookmark taken across the moment a camera came back,
	// say. The screen says so; the alternative is a clip that silently starts thirty
	// seconds after the moment somebody marked.
	FootageStartsAt int64 `json:"footageStartsAt,omitempty"`
	// FootageMissing marks evidence whose video is no longer on the appliance. It is a
	// fact the case screen MUST show: an item that silently renders as un-playable reads
	// as a broken player, when what it actually means is that the evidence is gone.
	//
	// It means NOTHING IN THE WHOLE SPAN has footage, not "the first instant has none".
	// Those are different facts, and the first version of this screen reported the second
	// one as the first: a sixty-second bookmark whose opening seconds predated the
	// recording was labelled "Footage gone" and refused to play, while the same case
	// correctly reported that it was holding four clips of it. Found by the screen pass.
	FootageMissing bool `json:"footageMissing"`
}

// CaseDetail is one case with its items and what it is holding.
type CaseDetail struct {
	Case  *entities.CaseFile `json:"case"`
	Items []CaseItemView     `json:"items"`
	Hold  CaseHoldSummary    `json:"hold"`
}

// CaseCreate is a new case.
type CaseCreate struct {
	Title      string
	Summary    string
	AssignedTo int64
	// AssignedName is resolved against the user directory by the API layer, which is the
	// layer that has one. Keeping the lookup out of here means an unresolvable assignee
	// fails where the caller can be told, instead of quietly storing a blank name.
	AssignedName string
	Actor        CaseActor
}

// CaseUpdate changes the editable fields of an open case. Nil means "leave alone", which
// is why these are pointers: an empty summary is a real edit (clearing it) and must be
// distinguishable from a request that did not mention the summary at all.
type CaseUpdate struct {
	Title      *string
	Summary    *string
	AssignedTo *int64
	// AssignedName accompanies AssignedTo; see CaseCreate.
	AssignedName string
}

// CaseItemInput adds one piece of evidence.
type CaseItemInput struct {
	Kind         string
	CameraId     int64
	StartedAt    int64
	EndedAt      int64
	Label        string
	Note         string
	SourceId     int64
	SnapshotPath string
	Actor        CaseActor
}

// CaseItemUpdate edits an item's annotation. Only the human text is editable: the camera
// and the span are what the item IS, and an item whose evidence can be re-pointed after
// the fact is an item nobody can rely on. Re-point by removing and adding.
type CaseItemUpdate struct {
	Label *string
	Note  *string
}

// ICaseService is the case file's read/write surface.
type ICaseService interface {
	List(ctx context.Context, limit, offset uint64, status string, assignedTo int64) ([]CaseSummary, uint64, error)
	Get(ctx context.Context, id int64) (*CaseDetail, error)
	Create(ctx context.Context, req CaseCreate) (*entities.CaseFile, error)
	Update(ctx context.Context, id int64, req CaseUpdate) (*entities.CaseFile, error)
	Close(ctx context.Context, id int64, outcome string, actor CaseActor) (*entities.CaseFile, error)
	Reopen(ctx context.Context, id int64, actor CaseActor) (*entities.CaseFile, error)
	Delete(ctx context.Context, id int64) error

	AddItem(ctx context.Context, caseId int64, in CaseItemInput) (*entities.CaseItem, error)
	UpdateItem(ctx context.Context, caseId, itemId int64, in CaseItemUpdate) (*entities.CaseItem, error)
	RemoveItem(ctx context.Context, caseId, itemId int64) (*entities.CaseItem, error)

	// HoldsFor reports the open-case evidence spans on one camera. This is the read every
	// footage-deletion path goes through — see case_hold.go.
	HoldsFor(ctx context.Context, cameraId int64) ([]FootageHold, error)
	// AnyHolds reports whether ANY open case holds footage, so a purge path that would
	// otherwise touch nothing can skip the per-camera reads entirely.
	AnyHolds(ctx context.Context) bool
}

// caseFootageResolver is the slice of ObservationService the case screen needs: turn a
// moment on a camera into a playable (segment, offset). Narrowed to an interface so the
// case service does not depend on the whole observation stack, and so tests can drive it.
type caseFootageResolver interface {
	ResolveFootageFor(ctx context.Context, points []FootagePoint) []FootageRef
}

// caseCameraNamer is ICameraService's display-name lookup, narrowed for the same reason.
type caseCameraNamer interface {
	DisplayName(ctx context.Context, cameraId int64) string
}

type caseService struct {
	cases     dbsql.IGenericRepo[entities.CaseFile]
	items     dbsql.IGenericRepo[entities.CaseItem]
	recording IRecordingService
	footage   caseFootageResolver
	cameras   caseCameraNamer
	now       func() int64
}

// NewCaseService builds the case file service. footage and cameras may be nil (the case
// still works, it just renders without names or play links).
func NewCaseService(
	cases dbsql.IGenericRepo[entities.CaseFile],
	items dbsql.IGenericRepo[entities.CaseItem],
	recording IRecordingService,
	footage caseFootageResolver,
	cameras caseCameraNamer,
) ICaseService {
	return &caseService{
		cases:     cases,
		items:     items,
		recording: recording,
		footage:   footage,
		cameras:   cameras,
		now:       func() int64 { return time.Now().UTC().Unix() },
	}
}

// --- cases -------------------------------------------------------------------

func (s *caseService) List(ctx context.Context, limit, offset uint64, status string, assignedTo int64) ([]CaseSummary, uint64, error) {
	if limit == 0 || limit > caseListPageSize {
		limit = caseListPageSize
	}
	var filters []sqldataenums.Filter
	if st := strings.TrimSpace(status); st != "" {
		filters = append(filters, sqldataenums.Filter{FieldName: "Status", Compare: sqldataenums.Equal, Value: st})
	}
	if assignedTo > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "AssignedTo", Compare: sqldataenums.Equal, Value: assignedTo})
	}
	sorters := []sqldataenums.Sorter{{FieldName: "UpdatedAt", Sort: sqldataenums.DESC}}
	rows, total, err := s.cases.Get(ctx, "", limit, offset, filters, sorters)
	if err != nil {
		return nil, 0, err
	}
	out := make([]CaseSummary, 0, len(rows))
	ids := make([]int64, 0, len(rows))
	byId := map[int64]*CaseSummary{}
	for _, row := range rows {
		if row == nil {
			continue
		}
		out = append(out, CaseSummary{CaseFile: row})
		ids = append(ids, row.Id)
	}
	for i := range out {
		byId[out[i].Id] = &out[i]
	}
	// One query for the whole page's items rather than a count per row: the list is the
	// screen an operator lands on, and an N+1 there is N+1 on every visit.
	if len(ids) > 0 {
		items, err := s.itemsForCases(ctx, ids)
		if err != nil {
			return nil, 0, err
		}
		for _, item := range items {
			sum := byId[item.CaseId]
			if sum == nil {
				continue
			}
			sum.ItemCount++
			if item.Kind == entities.CaseItemNote {
				sum.NoteCount++
			}
			if item.HoldsFootage() {
				sum.FootageItems++
			}
		}
	}
	return out, total, nil
}

func (s *caseService) Get(ctx context.Context, id int64) (*CaseDetail, error) {
	row, err := s.getCase(ctx, id)
	if err != nil {
		return nil, err
	}
	items, err := s.itemsForCases(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartedAt != items[j].StartedAt {
			return items[i].StartedAt < items[j].StartedAt
		}
		return items[i].Id < items[j].Id
	})

	views := make([]CaseItemView, 0, len(items))
	points := make([]FootagePoint, 0, len(items))
	for _, item := range items {
		view := CaseItemView{CaseItem: item}
		if item.CameraId > 0 && s.cameras != nil {
			view.CameraName = s.cameras.DisplayName(ctx, item.CameraId)
		}
		views = append(views, view)
		points = append(points, FootagePoint{CameraId: item.CameraId, At: item.StartedAt})
	}
	if s.footage != nil && len(points) > 0 {
		refs := s.footage.ResolveFootageFor(ctx, points)
		for i := range views {
			if i >= len(refs) {
				break
			}
			views[i].SegmentId = refs[i].SegmentId
			views[i].Seek = refs[i].Seek
			if !views[i].CaseItem.HoldsFootage() {
				// Only footage-bearing items can be missing footage. A note has none to
				// miss, and saying "footage missing" about a note is a false alarm on a
				// screen whose whole job is to be trusted about what evidence exists.
				continue
			}
			if views[i].SegmentId == 0 {
				// Nothing covers the exact moment, so snap FORWARD to the first footage
				// inside the span — the same thing the timeline's seek does with a gap,
				// and for the same reason: an operator asked to see this stretch, and
				// "the first frame we have of it" is a better answer than nothing.
				if seg := s.firstFootageIn(ctx, items[i]); seg != nil {
					views[i].SegmentId = seg.Id
					views[i].Seek = 0
					if seg.StartedAt > items[i].StartedAt {
						views[i].FootageStartsAt = seg.StartedAt
					}
				}
			}
			views[i].FootageMissing = views[i].SegmentId == 0
		}
	}

	detail := &CaseDetail{Case: row, Items: views}
	detail.Hold = s.summarizeHold(ctx, row, items)
	return detail, nil
}

func (s *caseService) Create(ctx context.Context, req CaseCreate) (*entities.CaseFile, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, errors.New("a case needs a title")
	}
	now := s.now()
	row := entities.CaseFile{
		Title:      title,
		Summary:    strings.TrimSpace(req.Summary),
		Status:     entities.CaseStatusOpen,
		AssignedTo: req.AssignedTo,
		OpenedBy:   req.Actor.Id,
		OpenedName: req.Actor.Name,
		OpenedAt:   now,
		UpdatedAt:  now,
	}
	if row.AssignedTo > 0 {
		row.AssignedName = strings.TrimSpace(req.AssignedName)
		// Assigning to yourself is the common case and needs no directory lookup.
		if row.AssignedName == "" && row.AssignedTo == req.Actor.Id {
			row.AssignedName = req.Actor.Name
		}
	}
	id, err := s.cases.Create(ctx, "", row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)
	return &row, nil
}

func (s *caseService) Update(ctx context.Context, id int64, req CaseUpdate) (*entities.CaseFile, error) {
	row, err := s.getCase(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Status == entities.CaseStatusClosed {
		return nil, errors.New("this case is closed — reopen it before editing")
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return nil, errors.New("a case needs a title")
		}
		row.Title = title
	}
	if req.Summary != nil {
		row.Summary = strings.TrimSpace(*req.Summary)
	}
	if req.AssignedTo != nil {
		row.AssignedTo = *req.AssignedTo
		// Clearing the assignment clears the name with it, so a stale name cannot outlive
		// the assignment it described.
		row.AssignedName = ""
		if row.AssignedTo > 0 {
			row.AssignedName = strings.TrimSpace(req.AssignedName)
		}
	}
	row.UpdatedAt = s.now()
	if _, err := s.cases.UpdateById(ctx, "", *row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *caseService) Close(ctx context.Context, id int64, outcome string, actor CaseActor) (*entities.CaseFile, error) {
	row, err := s.getCase(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Status == entities.CaseStatusClosed {
		return row, nil
	}
	// An outcome is REQUIRED. Closing is the act that releases every footage hold this
	// case holds, and "why" is the only part of that decision anybody can review later.
	if strings.TrimSpace(outcome) == "" {
		return nil, errors.New("say what the case concluded before closing it")
	}
	now := s.now()
	row.Status = entities.CaseStatusClosed
	row.Outcome = strings.TrimSpace(outcome)
	row.ClosedBy = actor.Id
	row.ClosedName = actor.Name
	row.ClosedAt = now
	row.UpdatedAt = now
	if _, err := s.cases.UpdateById(ctx, "", *row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *caseService) Reopen(ctx context.Context, id int64, actor CaseActor) (*entities.CaseFile, error) {
	row, err := s.getCase(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Status == entities.CaseStatusOpen {
		return row, nil
	}
	row.Status = entities.CaseStatusOpen
	// The closure is CLEARED, not kept as history: leaving ClosedBy/ClosedAt populated on
	// an open case makes every later read ambiguous about whether it is closed. The audit
	// trail is what remembers that it was once closed, and by whom.
	row.Outcome = ""
	row.ClosedBy = 0
	row.ClosedName = ""
	row.ClosedAt = 0
	row.UpdatedAt = s.now()
	if _, err := s.cases.UpdateById(ctx, "", *row); err != nil {
		return nil, err
	}
	return row, nil
}

// Delete removes a case and its items. Reaching this is an administrator's decision — the
// operator grant carries POST only, so the DELETE verb is not something the role model can
// hand out. The audit trail keeps the record that the case existed and who removed it.
func (s *caseService) Delete(ctx context.Context, id int64) error {
	if _, err := s.getCase(ctx, id); err != nil {
		return err
	}
	// Items first: an orphaned item still answers the hold query, so a case deleted
	// items-last would leave footage held by a case that no longer exists — held forever,
	// by nothing anyone can find and release.
	items, err := s.itemsForCases(ctx, []int64{id})
	if err != nil {
		return err
	}
	for _, item := range items {
		if _, err := s.items.DeleteById(ctx, "", uint64(item.Id)); err != nil {
			return err
		}
	}
	_, err = s.cases.DeleteById(ctx, "", uint64(id))
	return err
}

// --- items -------------------------------------------------------------------

func (s *caseService) AddItem(ctx context.Context, caseId int64, in CaseItemInput) (*entities.CaseItem, error) {
	row, err := s.getCase(ctx, caseId)
	if err != nil {
		return nil, err
	}
	if row.Status == entities.CaseStatusClosed {
		return nil, errors.New("this case is closed — reopen it before adding evidence")
	}
	item, err := s.buildItem(caseId, in)
	if err != nil {
		return nil, err
	}
	existing, err := s.itemsForCases(ctx, []int64{caseId})
	if err != nil {
		return nil, err
	}
	if len(existing) >= caseMaxItems {
		return nil, fmt.Errorf("a case holds at most %d items — split this into a second case", caseMaxItems)
	}
	// Adding the same evidence twice is the most likely mistake on a screen with an "add
	// to case" button on every row, and a duplicate clip in an export bundle is somebody
	// else's job to explain.
	for _, prev := range existing {
		if prev.Kind == item.Kind && prev.CameraId == item.CameraId &&
			prev.StartedAt == item.StartedAt && prev.EndedAt == item.EndedAt &&
			(item.SourceId == 0 || prev.SourceId == item.SourceId) {
			return nil, errors.New("that evidence is already in this case")
		}
	}
	id, err := s.items.Create(ctx, "", *item)
	if err != nil {
		return nil, err
	}
	item.Id = int64(id)
	s.touch(ctx, row)
	return item, nil
}

func (s *caseService) UpdateItem(ctx context.Context, caseId, itemId int64, in CaseItemUpdate) (*entities.CaseItem, error) {
	row, err := s.getCase(ctx, caseId)
	if err != nil {
		return nil, err
	}
	if row.Status == entities.CaseStatusClosed {
		return nil, errors.New("this case is closed — reopen it before editing it")
	}
	item, err := s.getItem(ctx, caseId, itemId)
	if err != nil {
		return nil, err
	}
	if in.Label != nil {
		item.Label = strings.TrimSpace(*in.Label)
	}
	if in.Note != nil {
		item.Note = strings.TrimSpace(*in.Note)
	}
	item.UpdatedAt = s.now()
	if _, err := s.items.UpdateById(ctx, "", *item); err != nil {
		return nil, err
	}
	s.touch(ctx, row)
	return item, nil
}

// RemoveItem takes evidence back out of a case.
//
// This RELEASES the item's footage hold, and it is worth being clear about what that does
// and does not mean. It does not delete anything: the footage returns to the retention
// policy it would have had if the case had never existed. The hold only ever EXTENDS
// footage past its normal life, so declining to extend it is not destroying evidence —
// which is why an operator may do this at all, given the role model's rule that an
// operator must never be able to destroy the record of an incident they were present at.
//
// It is nonetheless audited with the span it releases, because the case that mattered is
// the one where the removal happened the day before the retention sweep.
func (s *caseService) RemoveItem(ctx context.Context, caseId, itemId int64) (*entities.CaseItem, error) {
	row, err := s.getCase(ctx, caseId)
	if err != nil {
		return nil, err
	}
	if row.Status == entities.CaseStatusClosed {
		return nil, errors.New("this case is closed — reopen it before editing it")
	}
	item, err := s.getItem(ctx, caseId, itemId)
	if err != nil {
		return nil, err
	}
	if _, err := s.items.DeleteById(ctx, "", uint64(itemId)); err != nil {
		return nil, err
	}
	s.touch(ctx, row)
	return item, nil
}

func (s *caseService) buildItem(caseId int64, in CaseItemInput) (*entities.CaseItem, error) {
	kind := strings.TrimSpace(in.Kind)
	switch kind {
	case entities.CaseItemFootage, entities.CaseItemSighting, entities.CaseItemAlert, entities.CaseItemNote:
	case "":
		kind = entities.CaseItemFootage
	default:
		return nil, fmt.Errorf("unknown evidence kind %q", in.Kind)
	}
	now := s.now()
	item := &entities.CaseItem{
		CaseId:       caseId,
		Kind:         kind,
		CameraId:     in.CameraId,
		StartedAt:    in.StartedAt,
		EndedAt:      in.EndedAt,
		Label:        strings.TrimSpace(in.Label),
		Note:         strings.TrimSpace(in.Note),
		SourceId:     in.SourceId,
		SnapshotPath: strings.TrimSpace(in.SnapshotPath),
		AddedBy:      in.Actor.Id,
		AddedName:    in.Actor.Name,
		AddedAt:      now,
		UpdatedAt:    now,
	}
	if kind == entities.CaseItemNote {
		if item.Note == "" {
			return nil, errors.New("a note needs some text")
		}
		// A note is not evidence on a camera. Zeroing these rather than trusting the
		// caller keeps the hold query honest: a note with a stray camera and span would
		// silently pin footage nobody meant to keep.
		item.CameraId = 0
		item.EndedAt = 0
		if item.StartedAt <= 0 {
			item.StartedAt = now
		}
		return item, nil
	}
	if item.CameraId <= 0 {
		return nil, errors.New("evidence needs a camera")
	}
	if item.StartedAt <= 0 {
		return nil, errors.New("evidence needs a time")
	}
	if item.EndedAt <= item.StartedAt {
		return nil, errors.New("evidence needs a span that ends after it starts")
	}
	if item.EndedAt-item.StartedAt > caseItemMaxSpanSeconds {
		return nil, fmt.Errorf("that span is longer than %d hours — bookmark the part that matters", caseItemMaxSpanSeconds/3600)
	}
	return item, nil
}

// --- reads -------------------------------------------------------------------

func (s *caseService) getCase(ctx context.Context, id int64) (*entities.CaseFile, error) {
	if id <= 0 {
		return nil, errors.New("a case id is required")
	}
	// GetById ERRORS rather than returning nil on a missing row, so the not-found case has
	// to be recognised from the error text — see isNoResultFoundErr.
	row, err := s.cases.GetById(ctx, "", uint64(id))
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, errors.New("no such case")
		}
		return nil, err
	}
	if row == nil {
		return nil, errors.New("no such case")
	}
	return row, nil
}

func (s *caseService) getItem(ctx context.Context, caseId, itemId int64) (*entities.CaseItem, error) {
	if itemId <= 0 {
		return nil, errors.New("an item id is required")
	}
	item, err := s.items.GetById(ctx, "", uint64(itemId))
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, errors.New("no such item in this case")
		}
		return nil, err
	}
	// The id is global, so an item from ANOTHER case would otherwise be editable through
	// this case's URL. Checked rather than assumed.
	if item == nil || item.CaseId != caseId {
		return nil, errors.New("no such item in this case")
	}
	return item, nil
}

// itemsForCases reads every item of the given cases in one query.
//
// GetByForeign is deliberately NOT used: it returns a single row, which reads as "this
// case has one item" and is wrong in exactly the direction that loses evidence from an
// export. Filter + Get is the shape that returns them all.
func (s *caseService) itemsForCases(ctx context.Context, caseIds []int64) ([]*entities.CaseItem, error) {
	if len(caseIds) == 0 {
		return nil, nil
	}
	filter := sqldataenums.Filter{FieldName: "CaseId", Compare: sqldataenums.In, Value: caseIds}
	if len(caseIds) == 1 {
		filter = sqldataenums.Filter{FieldName: "CaseId", Compare: sqldataenums.Equal, Value: caseIds[0]}
	}
	sorters := []sqldataenums.Sorter{{FieldName: "StartedAt", Sort: sqldataenums.ASC}}
	// The page cap is the item cap times the page size, so a full page of full cases still
	// comes back whole. A truncated read here under-reports a case's contents, which on the
	// hold path means releasing footage that is still evidence.
	limit := uint64(caseMaxItems) * uint64(len(caseIds))
	var out []*entities.CaseItem
	var offset uint64
	for {
		batch, _, err := s.items.Get(ctx, "", pageOrCap(limit), offset, []sqldataenums.Filter{filter}, sorters)
		if err != nil {
			return nil, err
		}
		for _, row := range batch {
			if row != nil {
				out = append(out, row)
			}
		}
		if uint64(len(batch)) < pageOrCap(limit) || uint64(len(out)) >= limit {
			break
		}
		offset += uint64(len(batch))
	}
	return out, nil
}

// pageOrCap keeps each read inside the repository's own 100-row page cap, so the caller's
// loop is what pages rather than a query that silently returns less than it asked for.
func pageOrCap(want uint64) uint64 {
	const repoPageCap = uint64(100)
	if want > repoPageCap || want == 0 {
		return repoPageCap
	}
	return want
}

// firstFootageIn returns the earliest segment overlapping an item's span, or nil when the
// span holds no footage at all.
func (s *caseService) firstFootageIn(ctx context.Context, item *entities.CaseItem) *entities.RecordingSegment {
	segs := s.segmentsOverlapping(ctx, item.CameraId, item.StartedAt, item.EndedAt)
	var best *entities.RecordingSegment
	for _, seg := range segs {
		if best == nil || seg.StartedAt < best.StartedAt {
			best = seg
		}
	}
	return best
}

func (s *caseService) touch(ctx context.Context, row *entities.CaseFile) {
	row.UpdatedAt = s.now()
	_, _ = s.cases.UpdateById(ctx, "", *row)
}
