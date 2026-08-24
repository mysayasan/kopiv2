package services

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// The fakes below HONOUR the filters, the sorters and the offset.
//
// That is not gold-plating. Every interesting thing in this feature is query-shaped —
// "the open cases", "this camera's held spans", "page past the rows I could not delete" —
// so a fake that ignored filters would pass every test in this file with the filters
// deleted from the code. That has already happened once in this programme (W2-2's memRepo)
// and the fix was the same: make the fake answer the question the code is asking.

type fakeCaseRepo struct {
	dbsql.IGenericRepo[entities.CaseFile]
	rows []*entities.CaseFile
	seq  int64
	// failGet makes every read fail, so the fail-closed paths can be driven.
	failGet bool
}

func (f *fakeCaseRepo) Get(_ context.Context, _ string, limit, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.CaseFile, uint64, error) {
	if f.failGet {
		return nil, 0, errors.New("database is unavailable")
	}
	var out []*entities.CaseFile
	for _, row := range f.rows {
		keep := true
		for _, fl := range filters {
			switch fl.FieldName {
			case "Status":
				keep = keep && row.Status == fl.Value.(string)
			case "AssignedTo":
				keep = keep && row.AssignedTo == fl.Value.(int64)
			}
		}
		if keep {
			cp := *row
			out = append(out, &cp)
		}
	}
	total := uint64(len(out))
	sort.SliceStable(out, func(i, j int) bool {
		for _, s := range sorters {
			if s.FieldName == "UpdatedAt" && s.Sort == sqldataenums.DESC {
				return out[i].UpdatedAt > out[j].UpdatedAt
			}
		}
		return out[i].Id < out[j].Id
	})
	if offset >= uint64(len(out)) {
		return nil, total, nil
	}
	out = out[offset:]
	if limit > 0 && uint64(len(out)) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *fakeCaseRepo) GetById(_ context.Context, _ string, id uint64) (*entities.CaseFile, error) {
	for _, row := range f.rows {
		if row.Id == int64(id) {
			cp := *row
			return &cp, nil
		}
	}
	// The real repo ERRORS on a missing row rather than returning nil, and the service
	// only recognises that through the error text. A fake that returned (nil, nil) would
	// hide a not-found path that panics in production.
	return nil, errors.New("no result found")
}

func (f *fakeCaseRepo) Create(_ context.Context, _ string, model entities.CaseFile) (uint64, error) {
	f.seq++
	model.Id = f.seq
	f.rows = append(f.rows, &model)
	return uint64(model.Id), nil
}

func (f *fakeCaseRepo) UpdateById(_ context.Context, _ string, model entities.CaseFile) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

func (f *fakeCaseRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

type fakeCaseItemRepo struct {
	dbsql.IGenericRepo[entities.CaseItem]
	rows []*entities.CaseItem
	seq  int64
}

func (f *fakeCaseItemRepo) Get(_ context.Context, _ string, limit, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.CaseItem, uint64, error) {
	var out []*entities.CaseItem
	for _, row := range f.rows {
		keep := true
		for _, fl := range filters {
			switch fl.FieldName {
			case "CaseId":
				if fl.Compare == sqldataenums.In {
					ids, _ := fl.Value.([]int64)
					found := false
					for _, id := range ids {
						if id == row.CaseId {
							found = true
						}
					}
					keep = keep && found
				} else {
					keep = keep && row.CaseId == fl.Value.(int64)
				}
			case "CameraId":
				keep = keep && row.CameraId == fl.Value.(int64)
			}
		}
		if keep {
			cp := *row
			out = append(out, &cp)
		}
	}
	total := uint64(len(out))
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	if offset >= uint64(len(out)) {
		return nil, total, nil
	}
	out = out[offset:]
	if limit > 0 && uint64(len(out)) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *fakeCaseItemRepo) GetById(_ context.Context, _ string, id uint64) (*entities.CaseItem, error) {
	for _, row := range f.rows {
		if row.Id == int64(id) {
			cp := *row
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}

func (f *fakeCaseItemRepo) Create(_ context.Context, _ string, model entities.CaseItem) (uint64, error) {
	f.seq++
	model.Id = f.seq
	f.rows = append(f.rows, &model)
	return uint64(model.Id), nil
}

func (f *fakeCaseItemRepo) UpdateById(_ context.Context, _ string, model entities.CaseItem) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == model.Id {
			cp := model
			f.rows[i] = &cp
			return 1, nil
		}
	}
	return 0, errors.New("no result found")
}

func (f *fakeCaseItemRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, row := range f.rows {
		if row.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

// caseFixture builds a case service over the fakes, with a fixed clock.
type caseFixture struct {
	svc   *caseService
	cases *fakeCaseRepo
	items *fakeCaseItemRepo
	now   int64
}

func newCaseFixture(t *testing.T) *caseFixture {
	t.Helper()
	f := &caseFixture{
		cases: &fakeCaseRepo{},
		items: &fakeCaseItemRepo{},
		now:   1_700_000_000,
	}
	f.svc = &caseService{cases: f.cases, items: f.items}
	f.svc.now = func() int64 { return f.now }
	return f
}

func (f *caseFixture) openCase(t *testing.T, title string) *entities.CaseFile {
	t.Helper()
	row, err := f.svc.Create(context.Background(), CaseCreate{
		Title: title, Actor: CaseActor{Id: 7, Name: "sam"},
	})
	if err != nil {
		t.Fatalf("create case: %v", err)
	}
	return row
}

func TestCaseNeedsATitle(t *testing.T) {
	f := newCaseFixture(t)
	if _, err := f.svc.Create(context.Background(), CaseCreate{Title: "   "}); err == nil {
		t.Fatal("a case with a blank title must be refused")
	}
}

// Closing is the act that releases every footage hold the case holds. An outcome is the
// only part of that decision anybody can review afterwards, so it is required.
func TestClosingACaseRequiresAnOutcome(t *testing.T) {
	f := newCaseFixture(t)
	row := f.openCase(t, "Loading bay")
	if _, err := f.svc.Close(context.Background(), row.Id, "  ", CaseActor{Id: 7}); err == nil {
		t.Fatal("closing with no outcome must be refused")
	}
	closed, err := f.svc.Close(context.Background(), row.Id, "handed to police", CaseActor{Id: 7, Name: "sam"})
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if closed.Status != entities.CaseStatusClosed || closed.Outcome != "handed to police" || closed.ClosedName != "sam" {
		t.Fatalf("closure not recorded: %+v", closed)
	}
}

// Reopening clears the closure rather than leaving it beside an open status, which would
// make every later read ambiguous about whether the case is closed.
func TestReopeningClearsTheClosure(t *testing.T) {
	f := newCaseFixture(t)
	row := f.openCase(t, "Loading bay")
	if _, err := f.svc.Close(context.Background(), row.Id, "no further action", CaseActor{Id: 7}); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, err := f.svc.Reopen(context.Background(), row.Id, CaseActor{Id: 7})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.Status != entities.CaseStatusOpen || reopened.Outcome != "" || reopened.ClosedAt != 0 {
		t.Fatalf("closure not cleared: %+v", reopened)
	}
}

func TestClosedCaseRefusesNewEvidence(t *testing.T) {
	f := newCaseFixture(t)
	row := f.openCase(t, "Loading bay")
	if _, err := f.svc.Close(context.Background(), row.Id, "done", CaseActor{Id: 7}); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, err := f.svc.AddItem(context.Background(), row.Id, CaseItemInput{
		CameraId: 3, StartedAt: f.now - 600, EndedAt: f.now - 300,
	})
	if err == nil {
		t.Fatal("a closed case must refuse new evidence")
	}
}

func TestEvidenceNeedsARealSpan(t *testing.T) {
	f := newCaseFixture(t)
	row := f.openCase(t, "Loading bay")
	ctx := context.Background()

	// A zero-length instant cannot be exported and cannot be held. Refused, rather than
	// stored as evidence that silently protects and produces nothing.
	if _, err := f.svc.AddItem(ctx, row.Id, CaseItemInput{CameraId: 3, StartedAt: f.now, EndedAt: f.now}); err == nil {
		t.Fatal("a zero-length span must be refused")
	}
	if _, err := f.svc.AddItem(ctx, row.Id, CaseItemInput{CameraId: 0, StartedAt: f.now - 60, EndedAt: f.now}); err == nil {
		t.Fatal("evidence with no camera must be refused")
	}
	if _, err := f.svc.AddItem(ctx, row.Id, CaseItemInput{
		CameraId: 3, StartedAt: f.now - caseItemMaxSpanSeconds - 60, EndedAt: f.now,
	}); err == nil {
		t.Fatal("a span longer than the cap must be refused")
	}
}

// A note carries no camera and no span. If a caller passes one anyway it is zeroed, or the
// note would pin footage nobody meant to keep — a hold created by a typo.
func TestANoteHoldsNoFootage(t *testing.T) {
	f := newCaseFixture(t)
	row := f.openCase(t, "Loading bay")
	item, err := f.svc.AddItem(context.Background(), row.Id, CaseItemInput{
		Kind: entities.CaseItemNote, Note: "the same jacket as item 2",
		CameraId: 3, StartedAt: f.now - 600, EndedAt: f.now,
	})
	if err != nil {
		t.Fatalf("add note: %v", err)
	}
	if item.CameraId != 0 || item.EndedAt != 0 {
		t.Fatalf("a note must carry no camera and no span: %+v", item)
	}
	if item.HoldsFootage() {
		t.Fatal("a note must not hold footage")
	}
	if _, err := f.svc.AddItem(context.Background(), row.Id, CaseItemInput{Kind: entities.CaseItemNote}); err == nil {
		t.Fatal("an empty note must be refused")
	}
}

func TestDuplicateEvidenceIsRefused(t *testing.T) {
	f := newCaseFixture(t)
	row := f.openCase(t, "Loading bay")
	in := CaseItemInput{CameraId: 3, StartedAt: f.now - 600, EndedAt: f.now - 300}
	if _, err := f.svc.AddItem(context.Background(), row.Id, in); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := f.svc.AddItem(context.Background(), row.Id, in); err == nil {
		t.Fatal("the same evidence must not be addable twice")
	}
}

// Item ids are global, so a case's URL must not be able to reach another case's evidence.
func TestAnItemCannotBeReachedThroughAnotherCase(t *testing.T) {
	f := newCaseFixture(t)
	mine := f.openCase(t, "Mine")
	theirs := f.openCase(t, "Theirs")
	item, err := f.svc.AddItem(context.Background(), theirs.Id, CaseItemInput{
		CameraId: 3, StartedAt: f.now - 600, EndedAt: f.now - 300,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := f.svc.RemoveItem(context.Background(), mine.Id, item.Id); err == nil {
		t.Fatal("an item must not be removable through a different case")
	}
	note := "reassigned"
	if _, err := f.svc.UpdateItem(context.Background(), mine.Id, item.Id, CaseItemUpdate{Note: &note}); err == nil {
		t.Fatal("an item must not be editable through a different case")
	}
}

// Deleting a case must take its items with it. An orphaned item still answers the hold
// query, so footage would stay held by a case nobody can find or release.
func TestDeletingACaseLeavesNoOrphanedHold(t *testing.T) {
	f := newCaseFixture(t)
	row := f.openCase(t, "Loading bay")
	if _, err := f.svc.AddItem(context.Background(), row.Id, CaseItemInput{
		CameraId: 3, StartedAt: f.now - 600, EndedAt: f.now - 300,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := f.svc.Delete(context.Background(), row.Id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(f.items.rows) != 0 {
		t.Fatalf("items outlived their case: %d left", len(f.items.rows))
	}
	holds, err := f.svc.HoldsFor(context.Background(), 3)
	if err != nil {
		t.Fatalf("holds: %v", err)
	}
	if len(holds) != 0 {
		t.Fatalf("a deleted case still holds footage: %+v", holds)
	}
}

// The list page's counts come from the item table, so they cannot drift from it. They also
// have to distinguish notes from footage — "3 items" on a case of three notes would tell an
// operator there is evidence to export when there is none.
func TestListCountsSeparateNotesFromFootage(t *testing.T) {
	f := newCaseFixture(t)
	row := f.openCase(t, "Loading bay")
	ctx := context.Background()
	if _, err := f.svc.AddItem(ctx, row.Id, CaseItemInput{CameraId: 3, StartedAt: f.now - 600, EndedAt: f.now - 300}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := f.svc.AddItem(ctx, row.Id, CaseItemInput{Kind: entities.CaseItemNote, Note: "context"}); err != nil {
		t.Fatalf("add note: %v", err)
	}
	rows, total, err := f.svc.List(ctx, 0, 0, "", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("expected one case, got %d", len(rows))
	}
	if rows[0].ItemCount != 2 || rows[0].FootageItems != 1 || rows[0].NoteCount != 1 {
		t.Fatalf("counts wrong: %+v", rows[0])
	}
}

func TestListFiltersByStatus(t *testing.T) {
	f := newCaseFixture(t)
	open := f.openCase(t, "Still open")
	shut := f.openCase(t, "Finished")
	if _, err := f.svc.Close(context.Background(), shut.Id, "done", CaseActor{Id: 7}); err != nil {
		t.Fatalf("close: %v", err)
	}
	rows, _, err := f.svc.List(context.Background(), 0, 0, entities.CaseStatusOpen, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Id != open.Id {
		t.Fatalf("status filter ignored: %+v", rows)
	}
}
