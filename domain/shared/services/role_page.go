package services

import (
	"context"
	"fmt"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// IAccessRolePageService owns what an administrator chose — which pages a role holds, at which
// level — and keeps the enforced permission matrix derived from it.
//
// Writing a role's pages and re-deriving its matrix is ONE operation on purpose. If they could
// drift, the navigation (built from pages) and the server (built from the matrix) would be back
// to being two sources of truth, which is the problem page-level access exists to remove.
type IAccessRolePageService interface {
	ListForRole(ctx context.Context, roleId int64) ([]PageGrant, error)
	// SetForRole replaces a role's page grants and re-derives its managed permission rows.
	SetForRole(ctx context.Context, roleId int64, catalog PageCatalog, grants []PageGrant) error
	// AdoptPreset gives a role its preset pages and takes ownership of its existing permission
	// rows, but only if it holds no pages yet. Used at boot for the built-in roles.
	AdoptPreset(ctx context.Context, roleId int64, catalog PageCatalog, preset []PageGrant) error
}

type accessRolePageService struct {
	repo  dbsql.IGenericRepo[entities.AccessRolePage]
	perms IAccessPermissionService
	// permRepo is needed for the parts of the matrix the permission service does not expose:
	// deleting the rows this service owns.
	permRepo dbsql.IGenericRepo[entities.AccessRolePermission]
}

// NewAccessRolePageService builds the service over an app's database.
func NewAccessRolePageService(db dbsql.IDbCrud, perms IAccessPermissionService) IAccessRolePageService {
	return &accessRolePageService{
		repo:     dbsql.NewGenericRepo[entities.AccessRolePage](db),
		perms:    perms,
		permRepo: dbsql.NewGenericRepo[entities.AccessRolePermission](db),
	}
}

// NewAccessRolePageServiceWithRepos is the repo-injecting constructor used by tests.
func NewAccessRolePageServiceWithRepos(
	repo dbsql.IGenericRepo[entities.AccessRolePage],
	permRepo dbsql.IGenericRepo[entities.AccessRolePermission],
	perms IAccessPermissionService,
) IAccessRolePageService {
	return &accessRolePageService{repo: repo, perms: perms, permRepo: permRepo}
}

func (s *accessRolePageService) rows(ctx context.Context, roleId int64) ([]*entities.AccessRolePage, error) {
	filters := []sqldataenums.Filter{{FieldName: "RoleId", Compare: sqldataenums.Equal, Value: roleId}}
	found, _, err := s.repo.Get(ctx, "", 1000, 0, filters, nil)
	return found, err
}

func (s *accessRolePageService) ListForRole(ctx context.Context, roleId int64) ([]PageGrant, error) {
	found, err := s.rows(ctx, roleId)
	if err != nil {
		return nil, err
	}
	out := make([]PageGrant, 0, len(found))
	for _, r := range found {
		if r != nil {
			out = append(out, PageGrant{PageId: r.PageId, Level: r.Level})
		}
	}
	return out, nil
}

func (s *accessRolePageService) SetForRole(ctx context.Context, roleId int64, catalog PageCatalog, grants []PageGrant) error {
	// Drop the role's existing page rows, then write the new set. A page removed here must
	// actually disappear, or un-ticking it would leave the access behind.
	existing, err := s.rows(ctx, roleId)
	if err != nil {
		return fmt.Errorf("list pages for role %d: %w", roleId, err)
	}
	for _, r := range existing {
		if r == nil {
			continue
		}
		if _, err := s.repo.DeleteById(ctx, "", uint64(r.Id)); err != nil {
			return fmt.Errorf("delete page row %d: %w", r.Id, err)
		}
	}

	now := time.Now().Unix()
	for _, g := range grants {
		page, ok := catalog.Page(g.PageId)
		// Refuse silently-invalid input rather than storing it: an admin-only page or an unknown
		// level would derive to nothing and leave the UI claiming access that does not exist.
		if !ok || page.AdminOnly || !page.HasLevel(g.Level) {
			continue
		}
		if _, err := s.repo.Create(ctx, "", entities.AccessRolePage{
			RoleId: roleId, PageId: g.PageId, Level: g.Level, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("create page row %s: %w", g.PageId, err)
		}
	}

	return s.rederive(ctx, roleId, catalog, grants)
}

// rederive replaces the role's MANAGED permission rows with the derivation of its pages, leaving
// any row an administrator typed in under Advanced untouched.
func (s *accessRolePageService) rederive(ctx context.Context, roleId int64, catalog PageCatalog, grants []PageGrant) error {
	current, err := s.perms.ListForRole(ctx, roleId)
	if err != nil {
		return fmt.Errorf("list permissions for role %d: %w", roleId, err)
	}
	handEdited := map[string]bool{}
	for _, row := range current {
		if row == nil {
			continue
		}
		if !row.Managed {
			// An Advanced edit wins over the derivation. Silently overwriting somebody's
			// deliberate exception is the one behaviour that would make this feature untrusted.
			handEdited[row.Path] = true
			continue
		}
		if _, err := s.permRepo.DeleteById(ctx, "", uint64(row.Id)); err != nil {
			return fmt.Errorf("delete managed permission %d: %w", row.Id, err)
		}
	}

	for _, row := range DerivePermissions(roleId, catalog, grants) {
		if handEdited[row.Path] {
			continue
		}
		if _, err := s.perms.Set(ctx, row); err != nil {
			return fmt.Errorf("write derived permission %s: %w", row.Path, err)
		}
	}
	return nil
}

func (s *accessRolePageService) AdoptPreset(ctx context.Context, roleId int64, catalog PageCatalog, preset []PageGrant) error {
	existing, err := s.rows(ctx, roleId)
	if err != nil {
		return fmt.Errorf("list pages for role %d: %w", roleId, err)
	}
	current, err := s.perms.ListForRole(ctx, roleId)
	if err != nil {
		return fmt.Errorf("list permissions for role %d: %w", roleId, err)
	}

	// Already page-managed? Never re-impose the preset over an administrator's choices.
	//
	// Holding zero pages is a legitimate choice — it is how a role is suspended without being
	// deleted — so the presence of page rows cannot be the test on its own, or a suspended role
	// would quietly get its access back on the next boot. A role that has ever been saved
	// through SetForRole has MANAGED permission rows (the baseline, at minimum), and that is
	// what distinguishes "an admin chose nothing" from "this role has never been adopted".
	for _, row := range current {
		if row != nil && row.Managed {
			return nil
		}
	}
	if len(existing) > 0 {
		return nil
	}

	// An install that predates page-level access has permission rows but no page rows, and every
	// one of those rows came from the policy catalog — so they ARE managed, the column just did
	// not exist when they were written. Claiming them here is what lets the first derivation
	// replace them instead of layering a second, overlapping set on top.
	for _, row := range current {
		if row == nil || row.Managed {
			continue
		}
		claimed := *row
		claimed.Managed = true
		if _, err := s.perms.Set(ctx, claimed); err != nil {
			return fmt.Errorf("claim permission %s: %w", row.Path, err)
		}
	}

	return s.SetForRole(ctx, roleId, catalog, preset)
}
