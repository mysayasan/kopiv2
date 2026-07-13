package services

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
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
	// local superadmin (must-change-pw). Returns what it established, so a fresh
	// install can show the operator a credential they do not otherwise know.
	EnsureStockSuperadmin(ctx context.Context, username, password string) (StockSeedResult, error)
	// ResetStockSuperadmin force-resets the bootstrap superadmin's password (and
	// re-enables it) regardless of whether it was already taken over. This is the
	// lock-out recovery path and requires local admin rights on the host to trigger,
	// so it is deliberately not reachable over the network.
	ResetStockSuperadmin(ctx context.Context, username, password string) (StockSeedResult, error)
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
	// SuperadminStatus reports whether an active stock superadmin still exists, and
	// whether an active real (non-stock) superadmin exists. Drives the handoff banner
	// and guards stock-account disabling.
	SuperadminStatus(ctx context.Context) (stockActive bool, realActive bool, err error)
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

// StockSeedResult describes the bootstrap superadmin credential this run established.
// Seeded is true only when the account was actually created, and Generated is true
// when the password came from neither config nor env — meaning the operator does not
// know it yet and must be shown it.
type StockSeedResult struct {
	Username  string
	Password  string
	Generated bool
	Seeded    bool
}

// resolveBootstrapPassword picks the password to seed with, and reports whether it had
// to invent one. Precedence: the LOCAL_ADMIN_PASSWORD env override, then the config
// value, else a strong generated one. The packaged config.json ships an empty
// localAuth.password precisely so a fresh install generates a per-install credential
// rather than everyone sharing a well-known default.
func resolveBootstrapPassword(password string) (string, bool, error) {
	if envPass := strings.TrimSpace(os.Getenv("LOCAL_ADMIN_PASSWORD")); envPass != "" {
		return envPass, false, nil
	}
	if p := strings.TrimSpace(password); p != "" {
		return p, false, nil
	}
	generated, err := generateBootstrapPassword()
	if err != nil {
		return "", false, err
	}
	return generated, true, nil
}

// generateBootstrapPassword returns a 16-char random password from an unambiguous
// charset (no O/0/I/l/1), sourced from crypto/rand. It only ever seeds the first-run
// superadmin, which is must-change on first login. Mirrors mymatasan's generator so
// every platform hands out a per-install credential.
func generateBootstrapPassword() (string, error) {
	const charset = "ABCDEFGHJKMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = charset[int(b)%len(charset)]
	}
	return string(buf), nil
}

func (s *controlUserService) EnsureStockSuperadmin(ctx context.Context, username, password string) (StockSeedResult, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	resolved, generated, err := resolveBootstrapPassword(password)
	if err != nil {
		return StockSeedResult{}, err
	}
	out := StockSeedResult{Username: username, Password: resolved, Generated: generated}

	existing, err := s.getByUsername(ctx, username)
	if err != nil {
		return StockSeedResult{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(resolved), bcrypt.DefaultCost)
	if err != nil {
		return StockSeedResult{}, err
	}
	if existing != nil {
		// Refresh the bootstrap password from config ONLY while the account is still
		// the untouched stock account (must-change + active). Once the admin has set
		// their own password or the account was retired at handoff, config no longer
		// overrides it.
		//
		// A *generated* password is never refreshed in: we mint a new random one on
		// every boot, so rewriting the hash each start would silently invalidate the
		// credential the operator just read out of INITIAL_ADMIN_LOGIN.txt.
		if !generated && existing.IsStock && existing.MustChangePassword && !existing.Disabled {
			existing.PasswordHash = string(hash)
			existing.UpdatedAt = time.Now().Unix()
			if _, uerr := s.repo.UpdateById(ctx, "", *existing); uerr != nil {
				return StockSeedResult{}, uerr
			}
		}
		return out, nil
	}
	role, err := s.roles.GetByName(ctx, sharedservices.RoleSuperadmin)
	if err != nil {
		return StockSeedResult{}, err
	}
	if role == nil {
		return StockSeedResult{}, errors.New("superadmin role not seeded")
	}
	now := time.Now().Unix()
	if _, err = s.repo.Create(ctx, "", entities.ControlUser{
		Kind:               "local",
		Username:           username,
		Name:               "Stock Superadmin",
		PasswordHash:       string(hash),
		RoleId:             role.Id,
		MustChangePassword: true,
		IsStock:            true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}); err != nil {
		return StockSeedResult{}, err
	}
	out.Seeded = true
	return out, nil
}

// ResetStockSuperadmin is the lock-out recovery path: it force-sets the stock
// superadmin's password even when the account has already been taken over or was
// disabled at handoff, and re-flags it must-change. Unlike EnsureStockSuperadmin it
// does not care about the account's current state — that is the whole point.
//
// It is only ever reached by the app consuming a marker file dropped in the data dir
// (by the Windows installer's "reset the admin login" option, or by hand), so it needs
// local admin rights on the host and is not exposed over the network. If the account is
// gone entirely, it is re-created.
func (s *controlUserService) ResetStockSuperadmin(ctx context.Context, username, password string) (StockSeedResult, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	resolved, generated, err := resolveBootstrapPassword(password)
	if err != nil {
		return StockSeedResult{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(resolved), bcrypt.DefaultCost)
	if err != nil {
		return StockSeedResult{}, err
	}
	existing, err := s.getByUsername(ctx, username)
	if err != nil {
		return StockSeedResult{}, err
	}
	if existing == nil {
		// Nothing to reset — seed it fresh instead.
		return s.EnsureStockSuperadmin(ctx, username, resolved)
	}
	role, err := s.roles.GetByName(ctx, sharedservices.RoleSuperadmin)
	if err != nil {
		return StockSeedResult{}, err
	}
	if role == nil {
		return StockSeedResult{}, errors.New("superadmin role not seeded")
	}
	existing.PasswordHash = string(hash)
	existing.MustChangePassword = true
	existing.Disabled = false
	existing.RoleId = role.Id
	existing.UpdatedAt = time.Now().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *existing); err != nil {
		return StockSeedResult{}, err
	}
	return StockSeedResult{Username: username, Password: resolved, Generated: generated, Seeded: true}, nil
}

func (s *controlUserService) UpsertFederated(ctx context.Context, ssoUserId int64, email, name string) (*entities.ControlUser, error) {
	// A federated identity MUST carry a stable SSO subject id — that is the only key
	// we trust. Without it we cannot safely tell two operators apart (email is not a
	// reliable identifier; see findFederated), so refuse rather than risk binding the
	// login to the wrong account.
	if ssoUserId <= 0 {
		return nil, errors.New("federated login is missing a stable user id")
	}
	existing, err := s.findFederated(ctx, ssoUserId)
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
	// New federated identities are provisioned with NO role (pending). They can
	// authenticate but have zero access until a superadmin assigns a role on the RBAC
	// page — the SPA shows them an "access pending" screen. (Previously they were auto-
	// granted the viewer role, whose GET /api default exposed every admin page.)
	user := entities.ControlUser{
		Kind:        "federated",
		SsoUserId:   ssoUserId,
		Email:       strings.TrimSpace(email),
		Name:        strings.TrimSpace(name),
		RoleId:      0,
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
	// Look up by PRIMARY key — GetByUnique keyed on "id" matches no field (no ukey:"id")
	// and would return the FIRST user, making every session resolve to that user's role.
	row, err := s.repo.GetById(ctx, "", uint64(id))
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
	// Stable id order by default; the Users table lets the operator sort by any column.
	rows, _, err := s.repo.Get(ctx, "", 1000, 0, nil, []sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}})
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

func (s *controlUserService) SuperadminStatus(ctx context.Context) (stockActive bool, realActive bool, err error) {
	all, err := s.List(ctx)
	if err != nil {
		return false, false, err
	}
	for _, u := range all {
		if u.Disabled {
			continue
		}
		role, rerr := s.roles.GetById(ctx, u.RoleId)
		if rerr != nil || role == nil || !role.IsSuperadmin {
			continue
		}
		if u.IsStock {
			stockActive = true
		} else {
			realActive = true
		}
	}
	return stockActive, realActive, nil
}

// findFederated locates a federated user strictly by its SSO subject id. Email is
// deliberately NOT a match key: myidsan can emit a non-unique placeholder email
// (e.g. "admin") for multiple identities, and matching on it would let a new SSO
// identity inherit an existing — possibly privileged — account (account takeover /
// privilege escalation). A changed SSO id therefore provisions a fresh viewer
// account rather than silently rebinding to (and inheriting the role of) another row.
func (s *controlUserService) findFederated(ctx context.Context, ssoUserId int64) (*entities.ControlUser, error) {
	if ssoUserId <= 0 {
		return nil, nil
	}
	rows, err := s.query(ctx,
		sqldataenums.Filter{FieldName: "Kind", Compare: sqldataenums.Equal, Value: "federated"},
		sqldataenums.Filter{FieldName: "SsoUserId", Compare: sqldataenums.Equal, Value: ssoUserId})
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows[0], nil
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
