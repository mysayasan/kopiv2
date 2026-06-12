package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"golang.org/x/crypto/bcrypt"
)

type fakeUserLoginRepo struct {
	usersByEmail map[string]*entities.UserLogin
	createCount  int
	updateCount  int
	lastCreated  *entities.UserLogin
	lastUpdated  *entities.UserLogin
	nextID       uint64
}

func newFakeUserLoginRepo() *fakeUserLoginRepo {
	return &fakeUserLoginRepo{
		usersByEmail: map[string]*entities.UserLogin{},
		nextID:       1,
	}
}

func (f *fakeUserLoginRepo) Get(_ context.Context, _ string, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.UserLogin, uint64, error) {
	return nil, 0, nil
}

func (f *fakeUserLoginRepo) GetJoin(_ context.Context, _ string, _ any, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter, _ ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}

func (f *fakeUserLoginRepo) GetJoinWithSpec(_ context.Context, _ string, _ any, _ uint64, _ uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter, _ ...dbsql.JoinSpec) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}

func (f *fakeUserLoginRepo) GetSingle(_ context.Context, _ string, _ []sqldataenums.Filter) (*entities.UserLogin, error) {
	return nil, nil
}

func (f *fakeUserLoginRepo) GetById(_ context.Context, _ string, _ uint64) (*entities.UserLogin, error) {
	return nil, nil
}

func (f *fakeUserLoginRepo) GetByUnique(_ context.Context, _ string, keyGroup string, uids ...any) (*entities.UserLogin, error) {
	if keyGroup != "email" || len(uids) == 0 {
		return nil, errors.New("select by unique failed: no result found")
	}

	email, ok := uids[0].(string)
	if !ok {
		return nil, errors.New("select by unique failed: no result found")
	}

	user, found := f.usersByEmail[email]
	if !found {
		return nil, errors.New("select by unique failed: no result found")
	}

	copy := *user
	return &copy, nil
}

func (f *fakeUserLoginRepo) GetByForeign(_ context.Context, _ string, _ string, _ ...any) ([]*entities.UserLogin, error) {
	return nil, nil
}

func (f *fakeUserLoginRepo) Create(_ context.Context, _ string, model entities.UserLogin) (uint64, error) {
	f.createCount++
	id := f.nextID
	f.nextID++
	model.Id = int64(id)
	copy := model
	f.lastCreated = &copy
	f.usersByEmail[model.Email] = &copy
	return id, nil
}

func (f *fakeUserLoginRepo) CreateMultiple(_ context.Context, _ string, _ []entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginRepo) UpdateById(_ context.Context, _ string, model entities.UserLogin) (uint64, error) {
	f.updateCount++
	copy := model
	f.lastUpdated = &copy
	f.usersByEmail[model.Email] = &copy
	return 1, nil
}

func (f *fakeUserLoginRepo) UpdateByUnique(_ context.Context, _ string, _ string, _ entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginRepo) UpdateByForeign(_ context.Context, _ string, _ string, _ entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginRepo) Delete(_ context.Context, _ string, _ []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginRepo) DeleteById(_ context.Context, _ string, _ uint64) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginRepo) DeleteByUnique(_ context.Context, _ string, _ string, _ ...any) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginRepo) DeleteByForeign(_ context.Context, _ string, _ string, _ ...any) (uint64, error) {
	return 0, nil
}

func TestAuthenticateDefault_BcryptSuccess(t *testing.T) {
	repo := newFakeUserLoginRepo()
	hashed, err := hashPassword("secret123")
	if err != nil {
		t.Fatalf("hashPassword returned error: %v", err)
	}

	repo.usersByEmail["alice"] = &entities.UserLogin{
		Id:       10,
		Email:    "alice",
		Userpwd:  hashed,
		IsActive: true,
	}

	svc := NewUserLoginService(repo, nil)
	user, err := svc.AuthenticateDefault(context.Background(), "alice", "secret123")
	if err != nil {
		t.Fatalf("AuthenticateDefault returned error: %v", err)
	}
	if user == nil || user.Email != "alice" {
		t.Fatalf("expected authenticated user alice, got %#v", user)
	}
	if repo.updateCount != 0 {
		t.Fatalf("expected no migration update for bcrypt password, got %d", repo.updateCount)
	}
}

func TestAuthenticateDefault_BcryptWrongPassword(t *testing.T) {
	repo := newFakeUserLoginRepo()
	hashed, err := hashPassword("secret123")
	if err != nil {
		t.Fatalf("hashPassword returned error: %v", err)
	}

	repo.usersByEmail["alice"] = &entities.UserLogin{
		Id:       11,
		Email:    "alice",
		Userpwd:  hashed,
		IsActive: true,
	}

	svc := NewUserLoginService(repo, nil)
	_, err = svc.AuthenticateDefault(context.Background(), "alice", "wrong")
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("expected ErrInvalidCredential, got %v", err)
	}
}

func TestAuthenticateDefault_ThirdPartyOnlyAccount(t *testing.T) {
	repo := newFakeUserLoginRepo()
	repo.usersByEmail["oauth-user"] = &entities.UserLogin{
		Id:       12,
		Email:    "oauth-user",
		Userpwd:  "",
		IsActive: true,
	}

	svc := NewUserLoginService(repo, nil)
	_, err := svc.AuthenticateDefault(context.Background(), "oauth-user", "anything")
	if !errors.Is(err, ErrThirdPartyOnlyAccount) {
		t.Fatalf("expected ErrThirdPartyOnlyAccount, got %v", err)
	}
}

func TestAuthenticateDefault_LegacyPlaintextMigratesToBcrypt(t *testing.T) {
	repo := newFakeUserLoginRepo()
	repo.usersByEmail["legacy"] = &entities.UserLogin{
		Id:       13,
		Email:    "legacy",
		Userpwd:  "legacy-pass",
		IsActive: true,
	}

	svc := NewUserLoginService(repo, nil)
	user, err := svc.AuthenticateDefault(context.Background(), "legacy", "legacy-pass")
	if err != nil {
		t.Fatalf("AuthenticateDefault returned error: %v", err)
	}

	if repo.updateCount != 1 {
		t.Fatalf("expected one migration update, got %d", repo.updateCount)
	}
	if user == nil {
		t.Fatalf("expected authenticated user")
	}
	if !isBcryptHash(user.Userpwd) {
		t.Fatalf("expected returned user password to be bcrypt hash, got %q", user.Userpwd)
	}
	if err := comparePassword(user.Userpwd, "legacy-pass"); err != nil {
		t.Fatalf("expected migrated hash to match original password: %v", err)
	}
}

func TestCreate_HashesPassword(t *testing.T) {
	repo := newFakeUserLoginRepo()
	svc := NewUserLoginService(repo, nil)

	_, err := svc.Create(context.Background(), entities.UserLogin{
		Email:   "new-user",
		Userpwd: "plain-pass",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if repo.lastCreated == nil {
		t.Fatalf("expected create to persist model")
	}
	if !isBcryptHash(repo.lastCreated.Userpwd) {
		t.Fatalf("expected created password to be bcrypt hash, got %q", repo.lastCreated.Userpwd)
	}
}

func TestRegisterLocal_RejectsThirdPartyOnlyExistingAccount(t *testing.T) {
	repo := newFakeUserLoginRepo()
	repo.usersByEmail["oauth-only"] = &entities.UserLogin{
		Id:       14,
		Email:    "oauth-only",
		Userpwd:  "",
		IsActive: true,
	}

	svc := NewUserLoginService(repo, nil)
	_, err := svc.RegisterLocal(context.Background(), entities.UserLogin{
		Email:   "oauth-only",
		Userpwd: "new-pass",
	})
	if !errors.Is(err, ErrThirdPartyOnlyAccount) {
		t.Fatalf("expected ErrThirdPartyOnlyAccount, got %v", err)
	}
	if repo.createCount != 0 {
		t.Fatalf("expected no new record to be created")
	}
}

func comparePassword(hashed string, plain string) error {
	if !isBcryptHash(hashed) {
		return errors.New("not bcrypt")
	}

	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
}
