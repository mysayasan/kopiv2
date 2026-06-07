package dbsql

import (
	"context"
	"errors"
	"testing"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type genericRepoTestModel struct {
	Id   int64  `json:"id"`
	Name string `json:"name"`
}

type genericRepoFakeCrud struct {
	selectErr error
}

func (f genericRepoFakeCrud) Select(context.Context, interface{}, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, string, ...string) ([]map[string]interface{}, uint64, error) {
	return nil, 0, f.selectErr
}

func (f genericRepoFakeCrud) SelectSingle(context.Context, interface{}, []sqldataenums.Filter, string) (map[string]interface{}, error) {
	return nil, nil
}

func (f genericRepoFakeCrud) SelectById(context.Context, interface{}, string, uint64) (map[string]interface{}, error) {
	return nil, nil
}

func (f genericRepoFakeCrud) SelectByUnique(context.Context, interface{}, string, string, ...any) (map[string]interface{}, error) {
	return nil, nil
}

func (f genericRepoFakeCrud) SelectByForeign(context.Context, interface{}, string, string, ...any) ([]map[string]interface{}, error) {
	return nil, nil
}

func (f genericRepoFakeCrud) Insert(context.Context, interface{}, string) (uint64, error) {
	return 0, nil
}

func (f genericRepoFakeCrud) UpdateById(context.Context, interface{}, string) (uint64, error) {
	return 0, nil
}

func (f genericRepoFakeCrud) UpdateByUnique(context.Context, interface{}, string, string) (uint64, error) {
	return 0, nil
}

func (f genericRepoFakeCrud) UpdateByForeign(context.Context, interface{}, string, string) (uint64, error) {
	return 0, nil
}

func (f genericRepoFakeCrud) Delete(context.Context, interface{}, string, []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}

func (f genericRepoFakeCrud) DeleteById(context.Context, interface{}, string, uint64) (uint64, error) {
	return 0, nil
}

func (f genericRepoFakeCrud) DeleteByUnique(context.Context, interface{}, string, string, ...any) (uint64, error) {
	return 0, nil
}

func (f genericRepoFakeCrud) DeleteByForeign(context.Context, interface{}, string, string, ...any) (uint64, error) {
	return 0, nil
}

func (f genericRepoFakeCrud) Ping(context.Context) error {
	return nil
}

func (f genericRepoFakeCrud) BeginTx(context.Context) error {
	return nil
}

func (f genericRepoFakeCrud) RollbackTx() error {
	return nil
}

func (f genericRepoFakeCrud) CommitTx() error {
	return nil
}

func TestGenericRepoGetReturnsEmptyListForNoResult(t *testing.T) {
	repo := NewGenericRepo[genericRepoTestModel](genericRepoFakeCrud{
		selectErr: errors.New("no result found"),
	})

	rows, total, err := repo.Get(context.Background(), "test_model", 25, 0, nil, nil)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected total 0, got %d", total)
	}
	if len(rows) != 0 {
		t.Fatalf("expected empty rows, got %d", len(rows))
	}
}

func TestGenericRepoGetPropagatesOtherSelectErrors(t *testing.T) {
	repo := NewGenericRepo[genericRepoTestModel](genericRepoFakeCrud{
		selectErr: errors.New("syntax error"),
	})

	_, _, err := repo.Get(context.Background(), "test_model", 25, 0, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
