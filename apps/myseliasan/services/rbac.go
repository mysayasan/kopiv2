package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	sharedservices "github.com/mysayasan/kopiv2/domain/shared/services"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"golang.org/x/crypto/bcrypt"
)

// ErrUserDisabled is returned when a disabled user attempts to authenticate.
var ErrUserDisabled = errors.New("user is disabled")

// ErrInvalidCredentials is returned for a bad local username/password.
var ErrInvalidCredentials = errors.New("invalid username or password")

// minPasswordLen is the floor for a new local password.
const minPasswordLen = 8

// IControlUserService owns myseliasan's users (local stock + federated). Roles and
// the permission matrix live in the shared accessrbac core (domain/shared/services); this service is the
// app-specific identity layer and adapts a user to an sharedservices.AccessPrincipal.
type IControlUserService interface {
	// EnsureStockSuperadmin seeds (or refreshes, while untouched) the bootstrap
	// local superadmin (must-change-pw).
	EnsureStockSuperadmin(ctx context.Context, username, password string) error
	// UpsertFederated provisions/refreshes a myidsan user, assigning the viewer role
	// on first sight. Returns ErrUserDisabled for a disabled account.
	UpsertFederated(ctx context.Context, ssoUserId int64, email, name string) (*entities.ControlUser, error)
	GetById(ctx context.Context, id int64) (*entities.ControlUser, error)
	AuthenticateLocal(ctx context.Context, username, password string) (*entities.ControlUser, error)
	ChangePassword(ctx context.Context, userId int64, current, next string) error
	List(ctx context.Context) ([]*entities.ControlUser, error)
	SetRole(ctx context.Context, userId, roleId int64) error
	SetDisabled(ctx context.Context, userId int64, disabled bool) error
	// RetireStock disables every stock account (called after a real superadmin is
	// elevated). Returns how many were retired.
	RetireStock(ctx context.Context) (int, error)
	// ResolveAccessUser implements sharedservices.AccessUserResolver.
	ResolveAccessUser(ctx context.Context, userId int64) (*sharedservices.AccessPrincipal, error)
}

type controlUserService struct {
	repo  dbsql.IGenericRepo[entities.ControlUser]
	roles sharedservices.IAccessRoleService
}

// NewControlUserService builds the user service over the control-plane database.
func NewControlUserService(db dbsql.IDbCrud, roles sharedservices.IAccessRoleService) IControlUserService {
	return newControlUserService(dbsql.NewGenericRepo[entities.ControlUser](db), roles)
}

func newControlUserService(repo dbsql.IGenericRepo[entities.ControlUser], roles sharedservices.IAccessRoleService) *controlUserService {
	return &controlUserService{repo: repo, roles: roles}
}

func (s *controlUserService) ResolveAccessUser(ctx context.Context, userId int64) (*sharedservices.AccessPrincipal, error) {
	u, err := s.GetById(ctx, userId)
	if err != nil || u == nil {
		return nil, err
	}
	return &sharedservices.AccessPrincipal{
		RoleId:             u.RoleId,
		Disabled:           u.Disabled,
		MustChangePassword: u.MustChangePassword,
	}, nil
}

func (s *controlUserService) EnsureStockSuperadmin(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	if strings.TrimSpace(password) == "" {
		password = "admin"
	}
	existing, err := s.getByUsername(ctx, username)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if existing != nil {
		// Refresh the bootstrap password from config ONLY while the account is still
		// the untouched stock account (must-change + active). Once the admin has set
		// their own password or the account was retired at handoff, config no longer
		// overrides it.
		if existing.IsStock && existing.MustChangePassword && !existing.Disabled {
			existing.PasswordHash = string(hash)
			existing.UpdatedAt = time.Now().Unix()
			_, uerr := s.repo.UpdateById(ctx, "", *existing)
			return uerr
		}
		return nil
	}
	role, err := s.roles.GetByName(ctx, sharedservices.RoleSuperadmin)
	if err != nil {
		return err
	}
	if role == nil {
		return errors.New("superadmin role not seeded")
	}
	now := time.Now().Unix()
	_, err = s.repo.Create(ctx, "", entities.ControlUser{
		Kind:               "local",
		Username:           username,
		Name:               "Stock Superadmin",
		PasswordHash:       string(hash),
		RoleId:             role.Id,
		MustChangePassword: true,
		IsStock:            true,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	return err
}

func (s *controlUserService) UpsertFederated(ctx context.Context, ssoUserId int64, email, name string) (*entities.ControlUser, error) {
	existing, err := s.findFederated(ctx, ssoUserId, email)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if existing != nil {
		if existing.Disabled {
			return existing, ErrUserDisabled
		}
		existing.Email = strings.TrimSpace(email)
		existing.Name = strings.TrimSpace(name)
		existing.SsoUserId = ssoUserId
		existing.LastLoginAt = now
		existing.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *existing); err != nil {
			return nil, err
		}
		return existing, nil
	}
	viewer, err := s.roles.GetByName(ctx, sharedservices.RoleViewer)
	if err != nil {
		return nil, err
	}
	if viewer == nil {
		return nil, errors.New("viewer role not seeded")
	}
	user := entities.ControlUser{
		Kind:        "federated",
		SsoUserId:   ssoUserId,
		Email:       strings.TrimSpace(email),
		Name:        strings.TrimSpace(name),
		RoleId:      viewer.Id,
		LastLoginAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	id, err := s.repo.Create(ctx, "", user)
	if err != nil {
		return nil, err
	}
	user.Id = int64(id)
	return &user, nil
}

func (s *controlUserService) GetById(ctx context.Context, id int64) (*entities.ControlUser, error) {
	row, err := s.repo.GetByUnique(ctx, "", "id", id)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

func (s *controlUserService) AuthenticateLocal(ctx context.Context, username, password string) (*entities.ControlUser, error) {
	u, err := s.getByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrInvalidCredentials
	}
	if u.Disabled {
		return nil, ErrUserDisabled
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	u.LastLoginAt = time.Now().Unix()
	u.UpdatedAt = u.LastLoginAt
	_, _ = s.repo.UpdateById(ctx, "", *u)
	return u, nil
}

func (s *controlUserService) ChangePassword(ctx context.Context, userId int64, current, next string) error {
	if len(next) < minPasswordLen {
		return fmt.Errorf("new password must be at least %d characters", minPasswordLen)
	}
	u, err := s.GetById(ctx, userId)
	if err != nil {
		return err
	}
	if u == nil {
		return errors.New("user not found")
	}
	if u.Kind != "local" {
		return errors.New("password change applies to local accounts only")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	u.MustChangePassword = false
	u.UpdatedAt = time.Now().Unix()
	_, err = s.repo.UpdateById(ctx, "", *u)
	return err
}

func (s *controlUserService) List(ctx context.Context) ([]*entities.ControlUser, error) {
	rows, _, err := s.repo.Get(ctx, "", 1000, 0, nil, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return []*entities.ControlUser{}, nil
		}
		return nil, err
	}
	return rows, nil
}

func (s *controlUserService) SetRole(ctx context.Context, userId, roleId int64) error {
	u, err := s.GetById(ctx, userId)
	if err != nil {
		return err
	}
	if u == nil {
		return errors.New("user not found")
	}
	u.RoleId = roleId
	u.UpdatedAt = time.Now().Unix()
	_, err = s.repo.UpdateById(ctx, "", *u)
	return err
}

func (s *controlUserService) SetDisabled(ctx context.Context, userId int64, disabled bool) error {
	u, err := s.GetById(ctx, userId)
	if err != nil {
		return err
	}
	if u == nil {
		return errors.New("user not found")
	}
	u.Disabled = disabled
	u.UpdatedAt = time.Now().Unix()
	_, err = s.repo.UpdateById(ctx, "", *u)
	return err
}

func (s *controlUserService) RetireStock(ctx context.Context) (int, error) {
	all, err := s.List(ctx)
	if err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	retired := 0
	for _, u := range all {
		if u.IsStock && !u.Disabled {
			u.Disabled = true
			u.UpdatedAt = now
			if _, err := s.repo.UpdateById(ctx, "", *u); err != nil {
				return retired, err
			}
			retired++
		}
	}
	return retired, nil
}

// findFederated locates a federated user by SSO id, falling back to email (so a
// myidsan id change doesn't orphan an existing account).
func (s *controlUserService) findFederated(ctx context.Context, ssoUserId int64, email string) (*entities.ControlUser, error) {
	if ssoUserId > 0 {
		rows, err := s.query(ctx,
			sqldataenums.Filter{FieldName: "Kind", Compare: sqldataenums.Equal, Value: "federated"},
			sqldataenums.Filter{FieldName: "SsoUserId", Compare: sqldataenums.Equal, Value: ssoUserId})
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return rows[0], nil
		}
	}
	if e := strings.TrimSpace(email); e != "" {
		rows, err := s.query(ctx,
			sqldataenums.Filter{FieldName: "Kind", Compare: sqldataenums.Equal, Value: "federated"},
			sqldataenums.Filter{FieldName: "Email", Compare: sqldataenums.Equal, Value: e})
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return rows[0], nil
		}
	}
	return nil, nil
}

func (s *controlUserService) getByUsername(ctx context.Context, username string) (*entities.ControlUser, error) {
	rows, err := s.query(ctx,
		sqldataenums.Filter{FieldName: "Kind", Compare: sqldataenums.Equal, Value: "local"},
		sqldataenums.Filter{FieldName: "Username", Compare: sqldataenums.Equal, Value: username})
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows[0], nil
	}
	return nil, nil
}

func (s *controlUserService) query(ctx context.Context, filters ...sqldataenums.Filter) ([]*entities.ControlUser, error) {
	rows, _, err := s.repo.Get(ctx, "", 1000, 0, filters, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	return rows, nil
}
