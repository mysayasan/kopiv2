package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type localCrudTestModel struct {
	Id          int64  `pkey:"true" skipWhenInsert:"true"`
	Name        string `ukey:"name"`
	Age         int64
	Active      bool
	Description sql.NullString
}

func TestSQLiteDbCrudRepositoryCRUD(t *testing.T) {
	ctx := context.Background()
	crud := newTestCrud(t)
	repo := dbsql.NewGenericRepo[localCrudTestModel](crud)

	id, err := repo.Create(ctx, "", localCrudTestModel{
		Name:        "alpha",
		Age:         32,
		Active:      true,
		Description: sql.NullString{String: "first", Valid: true},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if id == 0 {
		t.Fatalf("Create() id = 0")
	}

	if _, err = repo.Create(ctx, "", localCrudTestModel{Name: "beta", Age: 24, Active: false}); err != nil {
		t.Fatalf("Create(beta) error = %v", err)
	}

	got, err := repo.GetById(ctx, "", id)
	if err != nil {
		t.Fatalf("GetById() error = %v", err)
	}
	if got.Name != "alpha" || got.Age != 32 || !got.Active || !got.Description.Valid {
		t.Fatalf("GetById() = %+v", got)
	}

	got.Age = 33
	got.Description = sql.NullString{}
	if affected, err := repo.UpdateById(ctx, "", *got); err != nil {
		t.Fatalf("UpdateById() error = %v", err)
	} else if affected != 1 {
		t.Fatalf("UpdateById() affected = %d", affected)
	}

	rows, total, err := repo.Get(ctx, "", 10, 0, []sqldataenums.Filter{
		{FieldName: "Active", Compare: sqldataenums.Equal, Value: true},
	}, []sqldataenums.Sorter{
		{FieldName: "Name", Sort: sqldataenums.ASC},
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].Age != 33 || rows[0].Description.Valid {
		t.Fatalf("Get() rows=%+v total=%d", rows, total)
	}

	if affected, err := repo.DeleteByUnique(ctx, "", "name", "alpha"); err != nil {
		t.Fatalf("DeleteByUnique() error = %v", err)
	} else if affected != 1 {
		t.Fatalf("DeleteByUnique() affected = %d", affected)
	}
}

// TestSQLiteSelectByUniqueUnknownKeyGroup guards against a severe auth bug: a
// GetByUnique whose key group matches NO field on the model used to resolve to an
// empty filter set and run an unfiltered `LIMIT 1` query, returning the FIRST row.
// Several services looked users/roles up via GetByUnique(ctx,"","id",…) although no
// entity tags Id with ukey:"id" — so every lookup resolved to the first user/role
// (the stock superadmin), making everyone a superadmin. The lookup must fail closed.
func TestSQLiteSelectByUniqueUnknownKeyGroup(t *testing.T) {
	ctx := context.Background()
	crud := newTestCrud(t)
	repo := dbsql.NewGenericRepo[localCrudTestModel](crud)

	if _, err := repo.Create(ctx, "", localCrudTestModel{Name: "alpha", Age: 1, Active: true}); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if _, err := repo.Create(ctx, "", localCrudTestModel{Name: "beta", Age: 2}); err != nil {
		t.Fatalf("create beta: %v", err)
	}

	// "id" is not a ukey on this model — must return nil, NOT the first row.
	got, err := repo.GetByUnique(ctx, "", "id", int64(2))
	if err != nil {
		t.Fatalf("GetByUnique(id): %v", err)
	}
	if got != nil {
		t.Fatalf("GetByUnique with an unknown key group must return nil, got %+v", got)
	}

	// A valid ukey still resolves correctly.
	got, err = repo.GetByUnique(ctx, "", "name", "beta")
	if err != nil || got == nil || got.Name != "beta" {
		t.Fatalf("GetByUnique(name) = %+v err=%v", got, err)
	}
}

func TestSQLiteDbCrudScopedTxRollback(t *testing.T) {
	ctx := context.Background()
	crud := newTestCrud(t)

	txCrud, err := crud.(dbsql.ScopedTxStarter).BeginScopedTx(ctx)
	if err != nil {
		t.Fatalf("BeginScopedTx() error = %v", err)
	}
	txRepo := dbsql.NewGenericRepo[localCrudTestModel](txCrud)
	if _, err := txRepo.Create(ctx, "", localCrudTestModel{Name: "rolled-back", Age: 11}); err != nil {
		t.Fatalf("tx Create() error = %v", err)
	}
	if err := txCrud.RollbackTx(); err != nil {
		t.Fatalf("RollbackTx() error = %v", err)
	}

	repo := dbsql.NewGenericRepo[localCrudTestModel](crud)
	rows, total, err := repo.Get(ctx, "", 10, 0, nil, nil)
	if err != nil {
		t.Fatalf("Get() after rollback error = %v", err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("rollback left rows=%+v total=%d", rows, total)
	}
}

func newTestCrud(t *testing.T) dbsql.IDbCrud {
	t.Helper()
	crud, err := NewDbCrud(dbsql.DbConfigModel{
		Engine: "sqlite",
		DbName: filepath.Join(t.TempDir(), "sqlite-crud.db"),
	})
	if err != nil {
		t.Fatalf("NewDbCrud() error = %v", err)
	}
	raw := crud.(*dbCrud)
	t.Cleanup(func() { raw.db.Close() })
	_, err = raw.db.Exec(`
CREATE TABLE local_crud_test (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	age INTEGER NOT NULL,
	active INTEGER NOT NULL,
	description TEXT
)
`)
	if err != nil {
		t.Fatalf("create test table error = %v", err)
	}
	return crud
}
