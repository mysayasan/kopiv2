package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"golang.org/x/crypto/bcrypt"
)

// userLoginService struct
type userLoginService struct {
	repo  dbsql.IGenericRepo[entities.UserLogin]
	cache cache.Store
}

const (
	defaultSuperadminUsername = "superadmin"
	defaultSuperadminPassword = "superadmin123"
)

var (
	ErrInvalidCredentialPayload = errors.New("username and password are required")
	ErrInvalidCredential        = errors.New("invalid username or password")
	ErrThirdPartyOnlyAccount    = errors.New("account is managed by third-party login")
	ErrInactiveAccount          = errors.New("account is inactive")
	ErrAccountAlreadyExists     = errors.New("account already exists")
)

// Create new IUserLoginService
func NewUserLoginService(
	repo dbsql.IGenericRepo[entities.UserLogin],
	cacheStore cache.Store,
) IUserLoginService {
	return &userLoginService{
		repo:  repo,
		cache: cacheStore,
	}
}

func (m *userLoginService) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.UserLogin, uint64, error) {
	if len(sorters) == 0 {
		sorters = []sqldataenums.Sorter{
			{
				FieldName: "CreatedAt",
				Sort:      sqldataenums.DESC,
			},
		}
	}

	return m.repo.Get(ctx, "", limit, offset, filters, sorters)
}

func (m *userLoginService) GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error) {
	return m.repo.GetByUnique(ctx, "", "email", email)
}

func (m *userLoginService) AuthenticateDefault(ctx context.Context, username string, password string) (*entities.UserLogin, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return nil, ErrInvalidCredentialPayload
	}

	user, err := m.repo.GetByUnique(ctx, "", "email", username)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, ErrInvalidCredential
		}

		return nil, err
	}

	if user == nil {
		return nil, ErrInvalidCredential
	}

	if !user.IsActive {
		return nil, ErrInactiveAccount
	}

	if strings.TrimSpace(user.Userpwd) == "" {
		return nil, ErrThirdPartyOnlyAccount
	}

	if isBcryptHash(user.Userpwd) {
		if err := bcrypt.CompareHashAndPassword([]byte(user.Userpwd), []byte(password)); err != nil {
			return nil, ErrInvalidCredential
		}

		return user, nil
	}

	if user.Userpwd != password {
		return nil, ErrInvalidCredential
	}

	if upgraded, err := hashPassword(password); err == nil {
		originalPwd := user.Userpwd
		user.Userpwd = upgraded
		if _, err := m.repo.UpdateById(ctx, "", *user); err != nil {
			if _, errByUnique := m.repo.UpdateByUnique(ctx, "", "email", *user); errByUnique != nil {
				user.Userpwd = originalPwd
				log.Printf("user_login migration warning email=%s errById=%v errByUnique=%v", user.Email, err, errByUnique)
			}
		}
	}

	return user, nil
}

func (m *userLoginService) RegisterLocal(ctx context.Context, model entities.UserLogin) (uint64, error) {
	model.Email = strings.TrimSpace(model.Email)
	model.Userpwd = strings.TrimSpace(model.Userpwd)
	if model.Email == "" || model.Userpwd == "" {
		return 0, ErrInvalidCredentialPayload
	}

	existing, err := m.repo.GetByUnique(ctx, "", "email", model.Email)
	if err != nil {
		if !isNotFoundErr(err) {
			return 0, err
		}
	} else if existing != nil {
		if strings.TrimSpace(existing.Userpwd) == "" {
			return 0, ErrThirdPartyOnlyAccount
		}

		return 0, ErrAccountAlreadyExists
	}

	if model.CreatedAt == 0 {
		model.CreatedAt = time.Now().Unix()
	}

	if model.UserRoleId < 0 {
		model.UserRoleId = 0
	}

	model.IsActive = true

	return m.Create(ctx, model)
}

func (m *userLoginService) Create(ctx context.Context, model entities.UserLogin) (uint64, error) {
	if sameCredential(model.Email, model.Userpwd) {
		allowed, err := m.allowDefaultSuperadminSeed(ctx, model)
		if err != nil {
			return 0, err
		}
		if !allowed {
			return 0, fmt.Errorf("username and password cannot be identical")
		}
	}

	if strings.TrimSpace(model.Userpwd) != "" && !isBcryptHash(model.Userpwd) {
		hashed, err := hashPassword(model.Userpwd)
		if err != nil {
			return 0, err
		}

		model.Userpwd = hashed
	}

	return m.repo.Create(ctx, "", model)
}

func sameCredential(username string, password string) bool {
	return strings.TrimSpace(username) == strings.TrimSpace(password)
}

func isNotFoundErr(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "no result found")
}

func isBcryptHash(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "$2a$") || strings.HasPrefix(raw, "$2b$") || strings.HasPrefix(raw, "$2y$")
}

func hashPassword(raw string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return string(hashed), nil
}

func (m *userLoginService) allowDefaultSuperadminSeed(ctx context.Context, model entities.UserLogin) (bool, error) {
	if strings.TrimSpace(model.Email) != defaultSuperadminUsername || strings.TrimSpace(model.Userpwd) != defaultSuperadminPassword {
		return false, nil
	}

	existing, err := m.repo.GetByUnique(ctx, "", "email", defaultSuperadminUsername)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no result found") {
			return true, nil
		}

		return false, err
	}

	return existing == nil, nil
}

func (m *userLoginService) Update(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.repo.UpdateById(ctx, "", model)
}

func (m *userLoginService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.repo.DeleteById(ctx, "", id)
}
