package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// accessrbac is the shared, single-app RBAC core: roles + a per-endpoint
// permission matrix, with NO app_code dimension. Each app has its own roles in its
// own database and supplies its own user layer via AccessUserResolver. A role
// flagged IsSuperadmin bypasses the matrix; every other role is allowed only what
// its permission rows grant. The enforcement middleware lives in
// domain/utils/middlewares (AccessSessionMidware).

// Built-in role names every app seeds.
const (
	RoleSuperadmin = "superadmin"
	RoleViewer     = "viewer"
)

// AccessPrincipal is the app-agnostic view of the authenticated user that the RBAC
// middleware needs: its role plus account state. Apps map their own user record to
// this via AccessUserResolver.
type AccessPrincipal struct {
	RoleId             int64
	Disabled           bool
	MustChangePassword bool
}

// AccessUserResolver adapts an app's user store to an AccessPrincipal. Returning
// (nil, nil) means "no such user" (treated as signed out).
type AccessUserResolver interface {
	ResolveAccessUser(ctx context.Context, userId int64) (*AccessPrincipal, error)
}

// ---------------------------------------------------------------- role service ---

type accessRoleService struct {
	repo dbsql.IGenericRepo[entities.AccessRole]
}

// NewAccessRoleService builds the role service over an app's database.
func NewAccessRoleService(db dbsql.IDbCrud) IAccessRoleService {
	return NewAccessRoleServiceWithRepo(dbsql.NewGenericRepo[entities.AccessRole](db))
}

// NewAccessRoleServiceWithRepo is the repo-injecting constructor (used by tests).
func NewAccessRoleServiceWithRepo(repo dbsql.IGenericRepo[entities.AccessRole]) IAccessRoleService {
	return &accessRoleService{repo: repo}
}

func (s *accessRoleService) EnsureBuiltins(ctx context.Context) error {
	builtins := []entities.AccessRole{
		{Name: RoleSuperadmin, Description: "Full control of this application.", IsSuperadmin: true, Builtin: true},
		{Name: RoleViewer, Description: "Read-only access to this application.", Builtin: true},
	}
	for _, b := range builtins {
		existing, err := s.GetByName(ctx, b.Name)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		now := time.Now().Unix()
		b.CreatedAt = now
		b.UpdatedAt = now
		if _, err := s.repo.Create(ctx, "", b); err != nil {
			return err
		}
	}
	return nil
}

func (s *accessRoleService) GetByName(ctx context.Context, name string) (*entities.AccessRole, error) {
	row, err := s.repo.GetByUnique(ctx, "", "name", name)
	if err != nil {
		if accessNoResult(err) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

func (s *accessRoleService) GetById(ctx context.Context, id int64) (*entities.AccessRole, error) {
	row, err := s.repo.GetByUnique(ctx, "", "id", id)
	if err != nil {
		if accessNoResult(err) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

func (s *accessRoleService) List(ctx context.Context) ([]*entities.AccessRole, error) {
	rows, _, err := s.repo.Get(ctx, "", 1000, 0, nil, nil)
	if err != nil {
		if accessNoResult(err) {
			return []*entities.AccessRole{}, nil
		}
		return nil, err
	}
	return rows, nil
}

func (s *accessRoleService) Create(ctx context.Context, name, description string) (*entities.AccessRole, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("role name is required")
	}
	if existing, err := s.GetByName(ctx, name); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, fmt.Errorf("a role named %q already exists", name)
	}
	now := time.Now().Unix()
	role := entities.AccessRole{Name: name, Description: strings.TrimSpace(description), CreatedAt: now, UpdatedAt: now}
	id, err := s.repo.Create(ctx, "", role)
	if err != nil {
		return nil, err
	}
	role.Id = int64(id)
	return &role, nil
}

func (s *accessRoleService) Update(ctx context.Context, id int64, name, description string) error {
	role, err := s.GetById(ctx, id)
	if err != nil {
		return err
	}
	if role == nil {
		return errors.New("role not found")
	}
	if strings.TrimSpace(name) != "" {
		role.Name = strings.TrimSpace(name)
	}
	role.Description = strings.TrimSpace(description)
	role.UpdatedAt = time.Now().Unix()
	_, err = s.repo.UpdateById(ctx, "", *role)
	return err
}

func (s *accessRoleService) Delete(ctx context.Context, id int64) error {
	role, err := s.GetById(ctx, id)
	if err != nil {
		return err
	}
	if role == nil {
		return nil
	}
	if role.Builtin {
		return errors.New("built-in roles cannot be deleted")
	}
	_, err = s.repo.DeleteById(ctx, "", uint64(id))
	return err
}

// ---------------------------------------------------- permission matrix service ---

type accessPermissionService struct {
	repo dbsql.IGenericRepo[entities.AccessRolePermission]
}

// NewAccessPermissionService builds the permission service over an app's database.
func NewAccessPermissionService(db dbsql.IDbCrud) IAccessPermissionService {
	return NewAccessPermissionServiceWithRepo(dbsql.NewGenericRepo[entities.AccessRolePermission](db))
}

// NewAccessPermissionServiceWithRepo is the repo-injecting constructor (tests).
func NewAccessPermissionServiceWithRepo(repo dbsql.IGenericRepo[entities.AccessRolePermission]) IAccessPermissionService {
	return &accessPermissionService{repo: repo}
}

func (s *accessPermissionService) EnsureViewerDefaults(ctx context.Context, viewerRoleId int64) error {
	rows, err := s.ListForRole(ctx, viewerRoleId)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		return nil
	}
	now := time.Now().Unix()
	_, err = s.repo.Create(ctx, "", entities.AccessRolePermission{
		RoleId: viewerRoleId, Path: "/api", CanGet: true, CreatedAt: now, UpdatedAt: now,
	})
	return err
}

func (s *accessPermissionService) Authorize(ctx context.Context, roleId int64, path, method string) (bool, error) {
	rows, err := s.ListForRole(ctx, roleId)
	if err != nil {
		return false, err
	}
	var best *entities.AccessRolePermission
	for _, r := range rows {
		if accessPathMatches(r.Path, path) && (best == nil || len(r.Path) > len(best.Path)) {
			best = r
		}
	}
	if best == nil {
		return false, nil
	}
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "HEAD", "OPTIONS":
		return best.CanGet, nil
	case "POST":
		return best.CanPost, nil
	case "PUT", "PATCH":
		return best.CanPut, nil
	case "DELETE":
		return best.CanDelete, nil
	}
	return false, nil
}

func (s *accessPermissionService) ListForRole(ctx context.Context, roleId int64) ([]*entities.AccessRolePermission, error) {
	rows, _, err := s.repo.Get(ctx, "", 1000, 0,
		[]sqldataenums.Filter{{FieldName: "RoleId", Compare: sqldataenums.Equal, Value: roleId}}, nil)
	if err != nil {
		if accessNoResult(err) {
			return []*entities.AccessRolePermission{}, nil
		}
		return nil, err
	}
	return rows, nil
}

func (s *accessPermissionService) Set(ctx context.Context, perm entities.AccessRolePermission) (*entities.AccessRolePermission, error) {
	perm.Path = accessNormalizePath(perm.Path)
	rows, err := s.ListForRole(ctx, perm.RoleId)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	for _, r := range rows {
		if r.Path == perm.Path {
			r.CanGet, r.CanPost, r.CanPut, r.CanDelete = perm.CanGet, perm.CanPost, perm.CanPut, perm.CanDelete
			r.UpdatedAt = now
			if _, err := s.repo.UpdateById(ctx, "", *r); err != nil {
				return nil, err
			}
			return r, nil
		}
	}
	perm.CreatedAt = now
	perm.UpdatedAt = now
	id, err := s.repo.Create(ctx, "", perm)
	if err != nil {
		return nil, err
	}
	perm.Id = int64(id)
	return &perm, nil
}

func (s *accessPermissionService) Delete(ctx context.Context, id int64) error {
	_, err := s.repo.DeleteById(ctx, "", uint64(id))
	return err
}

// ------------------------------------------------------------------- helpers ---

func accessNoResult(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no result found")
}

func accessPathMatches(allowed, requestPath string) bool {
	allowed = strings.TrimRight(strings.TrimSpace(allowed), "/")
	requestPath = strings.TrimRight(strings.TrimSpace(requestPath), "/")
	if allowed == "" {
		return true
	}
	return requestPath == allowed || strings.HasPrefix(requestPath, allowed+"/")
}

func accessNormalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if trimmed := strings.TrimRight(p, "/"); trimmed != "" {
		return trimmed
	}
	return "/"
}
