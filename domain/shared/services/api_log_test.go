package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type fakeApiLogRepo struct {
	deleteCount uint64
	lastFilters []sqldataenums.Filter
}

func (f *fakeApiLogRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.ApiLog, uint64, error) {
	return nil, 0, nil
}

func (f *fakeApiLogRepo) GetJoin(_ context.Context, _ string, _ any, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter, _ ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}

func (f *fakeApiLogRepo) GetSingle(_ context.Context, _ string, _ []sqldataenums.Filter) (*entities.ApiLog, error) {
	return nil, nil
}

func (f *fakeApiLogRepo) GetById(_ context.Context, _ string, _ uint64) (*entities.ApiLog, error) {
	return nil, nil
}

func (f *fakeApiLogRepo) GetByUnique(_ context.Context, _ string, _ string, _ ...any) (*entities.ApiLog, error) {
	return nil, nil
}

func (f *fakeApiLogRepo) GetByForeign(_ context.Context, _ string, _ string, _ ...any) ([]*entities.ApiLog, error) {
	return nil, nil
}

func (f *fakeApiLogRepo) Create(_ context.Context, _ string, _ entities.ApiLog) (uint64, error) {
	return 0, nil
}

func (f *fakeApiLogRepo) CreateMultiple(_ context.Context, _ string, _ []entities.ApiLog) (uint64, error) {
	return 0, nil
}

func (f *fakeApiLogRepo) UpdateById(_ context.Context, _ string, _ entities.ApiLog) (uint64, error) {
	return 0, nil
}

func (f *fakeApiLogRepo) UpdateByUnique(_ context.Context, _ string, _ string, _ entities.ApiLog) (uint64, error) {
	return 0, nil
}

func (f *fakeApiLogRepo) UpdateByForeign(_ context.Context, _ string, _ string, _ entities.ApiLog) (uint64, error) {
	return 0, nil
}

func (f *fakeApiLogRepo) Delete(_ context.Context, _ string, filters []sqldataenums.Filter) (uint64, error) {
	f.lastFilters = filters
	return f.deleteCount, nil
}

func (f *fakeApiLogRepo) DeleteById(_ context.Context, _ string, _ uint64) (uint64, error) {
	return 0, nil
}

func (f *fakeApiLogRepo) DeleteByUnique(_ context.Context, _ string, _ string, _ ...any) (uint64, error) {
	return 0, nil
}

func (f *fakeApiLogRepo) DeleteByForeign(_ context.Context, _ string, _ string, _ ...any) (uint64, error) {
	return 0, nil
}

func TestApiLogServiceRejectsCurrentMonthDelete(t *testing.T) {
	repo := &fakeApiLogRepo{}
	service := NewApiLogService(repo, nil)
	now := time.Now().UTC()

	_, err := service.DeleteByMonth(context.Background(), now.Year(), int(now.Month()))
	if !errors.Is(err, ErrCurrentMonthApiLogDelete) {
		t.Fatalf("expected current month delete error, got %v", err)
	}

	if len(repo.lastFilters) != 0 {
		t.Fatalf("expected no delete filters when current month is rejected")
	}
}

func TestApiLogServiceDeleteByMonthUsesMonthRange(t *testing.T) {
	repo := &fakeApiLogRepo{deleteCount: 7}
	service := NewApiLogService(repo, nil)

	deleted, err := service.DeleteByMonth(context.Background(), 2025, 12)
	if err != nil {
		t.Fatalf("delete by month failed: %v", err)
	}
	if deleted != 7 {
		t.Fatalf("expected deleted count 7, got %d", deleted)
	}
	if len(repo.lastFilters) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(repo.lastFilters))
	}

	start := time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC).Unix()
	end := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
	if repo.lastFilters[0].FieldName != "CreatedAt" || repo.lastFilters[0].Compare != sqldataenums.GreaterThanOrEqualTo || repo.lastFilters[0].Value != start {
		t.Fatalf("unexpected start filter: %#v", repo.lastFilters[0])
	}
	if repo.lastFilters[1].FieldName != "CreatedAt" || repo.lastFilters[1].Compare != sqldataenums.LessThan || repo.lastFilters[1].Value != end {
		t.Fatalf("unexpected end filter: %#v", repo.lastFilters[1])
	}
}

func TestApiLogServiceDeleteOlderThanUsesCutoff(t *testing.T) {
	repo := &fakeApiLogRepo{deleteCount: 3}
	service := NewApiLogService(repo, nil)

	before := time.Now().UTC().AddDate(0, 0, -30).Unix()
	deleted, err := service.DeleteOlderThan(context.Background(), 30)
	after := time.Now().UTC().AddDate(0, 0, -30).Unix()
	if err != nil {
		t.Fatalf("delete older than failed: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("expected deleted count 3, got %d", deleted)
	}
	if len(repo.lastFilters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(repo.lastFilters))
	}
	filter := repo.lastFilters[0]
	if filter.FieldName != "CreatedAt" || filter.Compare != sqldataenums.LessThan {
		t.Fatalf("unexpected cutoff filter: %#v", filter)
	}
	cutoff, ok := filter.Value.(int64)
	if !ok {
		t.Fatalf("expected int64 cutoff, got %T", filter.Value)
	}
	if cutoff < before || cutoff > after {
		t.Fatalf("expected cutoff between %d and %d, got %d", before, after, cutoff)
	}
}
