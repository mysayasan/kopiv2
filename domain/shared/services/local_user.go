package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultLocalAdminUsername = "admin"
	// defaultLocalAdminPassword is the legacy shipped default. It is NOT a first-run
	// seed fallback (an unset password now generates a per-install one) — it is
	// retained only so flagDefaultAdminPassword can nudge old installs still on it.
	defaultLocalAdminPassword = "Admin123"
)

var (
	ErrLocalUserInvalidCredential = errors.New("invalid username or password")
	ErrLocalUserInactive          = errors.New("account is inactive")
)

// authCacheTTL bounds how long a verified Basic credential is trusted without
// re-running bcrypt. The mymatasan SPA replays its HTTP Basic credential on
// EVERY request, so without this cache every authenticated call pays a full
// bcrypt.CompareHashAndPassword (~tens of ms of CPU) plus a LastLoginAt DB
// write — which caps throughput at roughly cores/bcrypt-cost regardless of what
// the endpoint does. Caching successful verifications collapses that to a hash
// lookup for the (identical) credential a client repeats. The TTL keeps
// staleness (deactivation, role change) short; password rotation is handled
// eagerly by flushing on every user mutation, so an old credential can never
// outlive its change. Only successes are cached — a wrong password always pays
// bcrypt, so the cache can't be used to cheapen credential guessing or be
// blown up by an attacker (its size is bounded by the real user count).
const authCacheTTL = 30 * time.Second

type authCacheEntry struct {
	identity *AuthenticatedUser
	expires  time.Time
}

type localUserService struct {
	repo dbsql.IGenericRepo[entities.LocalUser]
	// roles resolves a user's RoleId into the role that decides what they may do. It is what
	// makes AuthenticatedUser.IsAdmin a DERIVED value (the role's IsSuperadmin flag) rather
	// than a second, independent source of truth alongside RoleId.
	roles IAccessRoleService

	authMu    sync.RWMutex
	authCache map[string]authCacheEntry
}

// NewLocalUserService creates a standalone local user service for mymatasan.
func NewLocalUserService(repo dbsql.IGenericRepo[entities.LocalUser], roles IAccessRoleService) ILocalUserService {
	return &localUserService{
		repo:      repo,
		roles:     roles,
		authCache: make(map[string]authCacheEntry),
	}
}

// authCacheKey binds a verification to the exact username+password pair. The
// password is hashed (never stored in clear) with a fast digest — this is a
// cache key, not a credential store, and only ever holds pairs that already
// verified against bcrypt.
func authCacheKey(username, password string) string {
	sum := sha256.Sum256([]byte(username + "\x00" + password))
	return username + "\x00" + hex.EncodeToString(sum[:])
}

func (s *localUserService) authCacheGet(key string) (*AuthenticatedUser, bool) {
	s.authMu.RLock()
	entry, ok := s.authCache[key]
	s.authMu.RUnlock()
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	// Return a copy so callers can't mutate the shared cached identity.
	cp := *entry.identity
	return &cp, true
}

func (s *localUserService) authCachePut(key string, identity *AuthenticatedUser) {
	cp := *identity
	s.authMu.Lock()
	s.authCache[key] = authCacheEntry{identity: &cp, expires: time.Now().Add(authCacheTTL)}
	s.authMu.Unlock()
}

// authCacheFlush drops every cached verification. Called on any user mutation
// (password change, role/active toggle, delete, admin reset) so a stale or
// rotated credential can never be served from cache.
func (s *localUserService) authCacheFlush() {
	s.authMu.Lock()
	s.authCache = make(map[string]authCacheEntry)
	s.authMu.Unlock()
}

// EnsureDefaultAdmin seeds the bootstrap admin on first run (empty user table).
// The credentials come from the config file's localAuth block (username +
// password), mirroring myseliasan: edit config, log in with those credentials.
// Precedence for the password: the LOCAL_ADMIN_PASSWORD env var (ops override)
// wins, then the config value, then the shipped default. The seeded account is
// ALWAYS flagged must-change (like myseliasan's stock superadmin), so the
// config/default password is only a bootstrap credential and the operator is
// forced to set their own on first login.
func (s *localUserService) EnsureDefaultAdmin(ctx context.Context, username, password string) (AdminSeedResult, error) {
	_, total, err := s.repo.Get(ctx, "", 1, 0, nil, nil)
	if err != nil {
		return AdminSeedResult{}, err
	}
	if total > 0 {
		// An install already exists: don't seed, but if the admin is still on the
		// shipped default password, force a change so old installs are protected too.
		return AdminSeedResult{}, s.flagDefaultAdminPassword(ctx)
	}
	// First run: seed the admin from config (env overrides the password).
	username = normalizeUsername(username)
	if username == "" {
		username = defaultLocalAdminUsername
	}
	password, generated, err := resolveBootstrapPassword(password)
	if err != nil {
		return AdminSeedResult{}, err
	}
	if _, err = s.Create(ctx, CreateLocalUserRequest{
		Username:    username,
		Password:    password,
		DisplayName: "Administrator",
		IsAdmin:     true,
		IsActive:    true,
		// Always force a change on first login — the bootstrap password (config,
		// env, or generated) is never meant to be the operator's final one.
		MustChangePassword: true,
	}); err != nil {
		return AdminSeedResult{}, err
	}
	return AdminSeedResult{Seeded: true, Username: username, Password: password, Generated: generated}, nil
}

// ResetAdmin forces the admin account's password back to a bootstrap credential —
// the recovery path for an operator locked out of an existing install (e.g. a
// Windows reinstall over a data dir whose admin password is unknown). It is a
// deliberate, one-shot action driven by the caller (the app consumes an installer
// reset marker before calling this), NOT something that runs on every start.
// Resolution mirrors seeding: LOCAL_ADMIN_PASSWORD wins, then the config value,
// else a strong password is generated. The account is flagged must-change so the
// operator sets their own on next login. Returns the reset credential (Seeded set)
// so the caller can reveal it. On an empty user table it seeds instead.
func (s *localUserService) ResetAdmin(ctx context.Context, username, password string) (AdminSeedResult, error) {
	username = normalizeUsername(username)
	if username == "" {
		username = defaultLocalAdminUsername
	}
	target, err := s.findAdminToReset(ctx, username)
	if err != nil {
		return AdminSeedResult{}, err
	}
	if target == nil {
		// Nothing to reset (no users at all) — seed a fresh admin instead.
		return s.EnsureDefaultAdmin(ctx, username, password)
	}
	effPassword, generated, err := resolveBootstrapPassword(password)
	if err != nil {
		return AdminSeedResult{}, err
	}
	hashed, err := hashLocalPassword(effPassword)
	if err != nil {
		return AdminSeedResult{}, err
	}
	target.PasswordHash = hashed
	target.MustChangePassword = true
	target.IsActive = true
	target.UpdatedAt = time.Now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *target); err != nil {
		return AdminSeedResult{}, err
	}
	// The prior admin credential must stop authenticating immediately.
	s.authCacheFlush()
	return AdminSeedResult{Seeded: true, Username: target.Username, Password: effPassword, Generated: generated}, nil
}

// findAdminToReset picks the account ResetAdmin should act on: the configured
// username if it exists, otherwise the first admin (covers a renamed admin). Nil
// (no error) means the user table has no admin to reset.
func (s *localUserService) findAdminToReset(ctx context.Context, username string) (*entities.LocalUser, error) {
	if u, err := s.repo.GetByUnique(ctx, "", "username", username); err == nil && u != nil {
		return u, nil
	} else if err != nil && !isNoResultFoundErr(err) {
		return nil, err
	}
	users, _, err := s.repo.Get(ctx, "", 1000, 0, nil, nil)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u != nil && u.IsAdmin {
			return u, nil
		}
	}
	return nil, nil
}

// resolveBootstrapPassword returns the bootstrap password to seed/reset with, and
// whether it was randomly generated (so the caller knows the operator does not yet
// know it and must be shown it). Precedence: LOCAL_ADMIN_PASSWORD env override,
// then the supplied config value, else a strong generated one. The packaged
// config.json ships an empty localAuth.password so non-Windows installs generate.
func resolveBootstrapPassword(password string) (string, bool, error) {
	password = strings.TrimSpace(password)
	if envPass := strings.TrimSpace(os.Getenv("LOCAL_ADMIN_PASSWORD")); envPass != "" {
		return envPass, false, nil
	}
	if password != "" {
		return password, false, nil
	}
	generated, err := generateBootstrapPassword()
	if err != nil {
		return "", false, err
	}
	return generated, true, nil
}

// generateBootstrapPassword returns a 16-char random password from an unambiguous
// charset (no O/0/I/l/1), sourced from crypto/rand. It only ever seeds the
// first-run admin, which is must-change on first login, so a small modulo bias is
// irrelevant. Mirrors the Windows installer's GenPassword so every platform hands
// out a per-install credential instead of a shared default.
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

// flagDefaultAdminPassword sets MustChangePassword on the seeded admin when it is
// still using the shipped default credential, so existing installs are nudged off
// it on next login. A non-default password, or an already-flagged admin, is left
// untouched.
func (s *localUserService) flagDefaultAdminPassword(ctx context.Context) error {
	user, err := s.repo.GetByUnique(ctx, "", "username", defaultLocalAdminUsername)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil
		}
		return err
	}
	if user == nil || user.MustChangePassword {
		return nil
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(defaultLocalAdminPassword)) != nil {
		return nil
	}
	user.MustChangePassword = true
	user.UpdatedAt = time.Now().UTC().Unix()
	_, err = s.repo.UpdateById(ctx, "", *user)
	return err
}

func (s *localUserService) Authenticate(ctx context.Context, username string, password string) (*AuthenticatedUser, error) {
	username = normalizeUsername(username)
	if username == "" || password == "" {
		return nil, ErrLocalUserInvalidCredential
	}
	// Fast path: this exact credential verified within the TTL. Skips both the
	// bcrypt compare and the LastLoginAt write — the two per-request costs that
	// otherwise cap throughput, since the SPA replays Basic auth on every call.
	key := authCacheKey(username, password)
	if identity, ok := s.authCacheGet(key); ok {
		return identity, nil
	}
	user, err := s.repo.GetByUnique(ctx, "", "username", username)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, ErrLocalUserInvalidCredential
		}
		return nil, err
	}
	if user == nil {
		return nil, ErrLocalUserInvalidCredential
	}
	if !user.IsActive {
		return nil, ErrLocalUserInactive
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrLocalUserInvalidCredential
	}
	user.LastLoginAt = time.Now().UTC().Unix()
	user.UpdatedAt = user.LastLoginAt
	_, _ = s.repo.UpdateById(ctx, "", *user)
	identity, err := s.identity(ctx, user)
	if err != nil {
		return nil, err
	}
	s.authCachePut(key, identity)
	return identity, nil
}

func (s *localUserService) AuthenticateSession(ctx context.Context, username string, sessionHash string) (*AuthenticatedUser, error) {
	username = normalizeUsername(username)
	sessionHash = strings.TrimSpace(sessionHash)
	if username == "" || sessionHash == "" {
		return nil, ErrLocalUserInvalidCredential
	}
	user, err := s.repo.GetByUnique(ctx, "", "username", username)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, ErrLocalUserInvalidCredential
		}
		return nil, err
	}
	if user == nil {
		return nil, ErrLocalUserInvalidCredential
	}
	if !user.IsActive {
		return nil, ErrLocalUserInactive
	}
	expected := localSessionHash(user)
	if len(sessionHash) != len(expected) || subtle.ConstantTimeCompare([]byte(sessionHash), []byte(expected)) != 1 {
		return nil, ErrLocalUserInvalidCredential
	}
	return s.identity(ctx, user)
}

func (s *localUserService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.LocalUser, uint64, error) {
	if limit == 0 {
		limit = 100
	}
	sorters := []sqldataenums.Sorter{{FieldName: "Username", Sort: sqldataenums.ASC}}
	return s.repo.Get(ctx, "", limit, offset, nil, sorters)
}

func (s *localUserService) Create(ctx context.Context, req CreateLocalUserRequest) (*entities.LocalUser, error) {
	username := normalizeUsername(req.Username)
	password := strings.TrimSpace(req.Password)
	if username == "" {
		return nil, errors.New("username is required")
	}
	if strings.Contains(username, ":") {
		return nil, errors.New("username cannot contain ':'")
	}
	if password == "" {
		return nil, errors.New("password is required")
	}
	if len(password) < 8 {
		return nil, errors.New("password must be at least 8 characters")
	}
	if _, err := s.repo.GetByUnique(ctx, "", "username", username); err == nil {
		return nil, fmt.Errorf("username %q already exists", username)
	} else if !isNoResultFoundErr(err) {
		return nil, err
	}

	hashed, err := hashLocalPassword(password)
	if err != nil {
		return nil, err
	}
	roleId, isSuperadmin, err := s.resolveRole(ctx, req.RoleId, req.IsAdmin)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Unix()
	model := entities.LocalUser{
		Username:     username,
		PasswordHash: hashed,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		RoleId:       roleId,
		// IsAdmin is a MIRROR of the role, not an independent flag — it is written from the
		// role and never read for authorization (see identity()). It is kept so the shipped
		// Users screen, which renders an admin badge, still shows the right thing.
		IsAdmin:            isSuperadmin,
		IsActive:           req.IsActive,
		MustChangePassword: req.MustChangePassword,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	id, err := s.repo.Create(ctx, "", model)
	if err != nil {
		return nil, err
	}
	model.Id = int64(id)
	return &model, nil
}

func (s *localUserService) Update(ctx context.Context, id uint64, req UpdateLocalUserRequest) (*entities.LocalUser, error) {
	user, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	username := normalizeUsername(req.Username)
	if username == "" {
		return nil, errors.New("username is required")
	}
	if strings.Contains(username, ":") {
		return nil, errors.New("username cannot contain ':'")
	}
	if username != user.Username {
		if _, err := s.repo.GetByUnique(ctx, "", "username", username); err == nil {
			return nil, fmt.Errorf("username %q already exists", username)
		} else if !isNoResultFoundErr(err) {
			return nil, err
		}
	}
	roleId, isSuperadmin, err := s.resolveRole(ctx, req.RoleId, req.IsAdmin)
	if err != nil {
		return nil, err
	}
	if err := s.ensureNotRemovingLastAdmin(ctx, user, isSuperadmin, req.IsActive); err != nil {
		return nil, err
	}
	user.Username = username
	user.DisplayName = strings.TrimSpace(req.DisplayName)
	user.RoleId = roleId
	user.IsAdmin = isSuperadmin // mirror; see Create
	user.IsActive = req.IsActive
	user.UpdatedAt = time.Now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *user); err != nil {
		return nil, err
	}
	// A rename / role / active-state change must invalidate any cached auth.
	s.authCacheFlush()
	return user, nil
}

func (s *localUserService) ResetPassword(ctx context.Context, id uint64, password string) (*entities.LocalUser, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return nil, errors.New("password is required")
	}
	if len(password) < 8 {
		return nil, errors.New("password must be at least 8 characters")
	}
	user, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	hashed, err := hashLocalPassword(password)
	if err != nil {
		return nil, err
	}
	user.PasswordHash = hashed
	// An admin deliberately setting a password clears any forced-change flag.
	user.MustChangePassword = false
	user.UpdatedAt = time.Now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *user); err != nil {
		return nil, err
	}
	// Old password must stop working immediately.
	s.authCacheFlush()
	return user, nil
}

// ChangePassword is the authenticated user's self-service password change. It
// verifies the current password, enforces the new password rules, clears the
// forced-change flag, and returns a fresh identity (with a rotated session hash).
func (s *localUserService) ChangePassword(ctx context.Context, userId int64, currentPassword string, newPassword string) (*AuthenticatedUser, error) {
	currentPassword = strings.TrimSpace(currentPassword)
	newPassword = strings.TrimSpace(newPassword)
	if currentPassword == "" || newPassword == "" {
		return nil, errors.New("current and new password are required")
	}
	if len(newPassword) < 8 {
		return nil, errors.New("new password must be at least 8 characters")
	}
	if newPassword == currentPassword {
		return nil, errors.New("new password must differ from the current password")
	}
	if newPassword == defaultLocalAdminPassword {
		return nil, errors.New("choose a password other than the default")
	}
	user, err := s.repo.GetById(ctx, "", uint64(userId))
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil, ErrLocalUserInvalidCredential
		}
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, ErrLocalUserInvalidCredential
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)) != nil {
		return nil, ErrLocalUserInvalidCredential
	}
	hashed, err := hashLocalPassword(newPassword)
	if err != nil {
		return nil, err
	}
	user.PasswordHash = hashed
	user.MustChangePassword = false
	user.UpdatedAt = time.Now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *user); err != nil {
		return nil, err
	}
	// Old password must stop working immediately.
	s.authCacheFlush()
	return s.identity(ctx, user)
}

func (s *localUserService) Delete(ctx context.Context, id uint64) (uint64, error) {
	user, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return 0, err
	}
	if err := s.ensureNotRemovingLastAdmin(ctx, user, false, false); err != nil {
		return 0, err
	}
	deleted, err := s.repo.DeleteById(ctx, "", id)
	if err == nil {
		// A deleted user must not keep authenticating from cache.
		s.authCacheFlush()
	}
	return deleted, err
}

// ensureNotRemovingLastAdmin refuses to demote, disable or delete the only remaining
// administrator — the change that would lock the operator out of their own appliance with no
// way back in.
//
// It now counts by ROLE rather than by the legacy IsAdmin bool. Counting the bool would have
// been a real hazard once roles are the authority: an admin whose ROLE was changed while the
// mirror lagged would not be counted, and the guard would let the last one go.
func (s *localUserService) ensureNotRemovingLastAdmin(ctx context.Context, user *entities.LocalUser, nextIsAdmin bool, nextIsActive bool) error {
	adminRole, err := s.adminRoleId(ctx)
	if err != nil {
		return err
	}
	isCurrentlyAdmin := user != nil && user.RoleId == adminRole
	if user == nil || !isCurrentlyAdmin || !user.IsActive || (nextIsAdmin && nextIsActive) {
		return nil
	}
	filters := []sqldataenums.Filter{
		{FieldName: "RoleId", Compare: sqldataenums.Equal, Value: adminRole},
		{FieldName: "IsActive", Compare: sqldataenums.Equal, Value: true},
	}
	_, total, err := s.repo.Get(ctx, "", 2, 0, filters, nil)
	if err != nil {
		return err
	}
	if total <= 1 {
		return errors.New("cannot remove the last active admin user")
	}
	return nil
}

// BackfillRoles gives a role to every user who does not have one yet, derived from the
// legacy IsAdmin bool: an admin becomes a superadmin, everyone else becomes a viewer.
//
// This is what carries an existing install across. It runs once — it only touches users
// with RoleId == 0 — and it is deliberately NOT a bootstrap migration: migrations run
// BEFORE the schema is auto-migrated (so role_id does not exist yet) and before the roles
// themselves are seeded (so there is no id to point at). It has to happen here, after both.
//
// A non-admin becomes an OPERATOR, not a viewer. That is what makes the upgrade a
// non-regression: today's non-admin can already review recorded footage and acknowledge
// alerts, and VIEWER — the stricter role the old model could not express — cannot. Demoting
// them would silently take away access they use every day.
//
// They do gain two things they did not have: PTZ and talk-back. That is a deliberate,
// documented widening — neither destroys evidence, both are camera controls — and an admin
// can move any account down to viewer.
func (s *localUserService) BackfillRoles(ctx context.Context, adminRoleId, defaultRoleId int64) (int, error) {
	if adminRoleId <= 0 || defaultRoleId <= 0 {
		return 0, fmt.Errorf("backfill roles: admin=%d default=%d, both are required", adminRoleId, defaultRoleId)
	}

	users, _, err := s.repo.Get(ctx, "", 1000, 0, []sqldataenums.Filter{
		{FieldName: "RoleId", Compare: sqldataenums.Equal, Value: 0},
	}, nil)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, user := range users {
		if user == nil || user.RoleId > 0 {
			continue
		}
		user.RoleId = defaultRoleId
		if user.IsAdmin {
			user.RoleId = adminRoleId
		}
		user.UpdatedAt = time.Now().UTC().Unix()
		if _, err := s.repo.UpdateById(ctx, "", *user); err != nil {
			return updated, fmt.Errorf("backfill role for %q: %w", user.Username, err)
		}
		updated++
	}
	if updated > 0 {
		s.authCacheFlush()
	}
	return updated, nil
}

// resolveRole turns a create/update request into the role the user will carry.
//
// RoleId is the authority. A zero RoleId falls back to the legacy IsAdmin bool (admin ->
// superadmin, otherwise viewer), so a client that predates roles — including the shipped
// Settings > Users screen — keeps working unchanged.
func (s *localUserService) resolveRole(ctx context.Context, roleId int64, legacyIsAdmin bool) (int64, bool, error) {
	if s.roles == nil {
		return 0, false, fmt.Errorf("roles are not configured")
	}
	if roleId > 0 {
		role, err := s.roles.GetById(ctx, roleId)
		if err != nil {
			return 0, false, fmt.Errorf("resolve role %d: %w", roleId, err)
		}
		if role == nil {
			return 0, false, fmt.Errorf("role %d does not exist", roleId)
		}
		return role.Id, role.IsSuperadmin, nil
	}

	name := RoleViewer
	if legacyIsAdmin {
		name = RoleSuperadmin
	}
	role, err := s.roles.GetByName(ctx, name)
	if err != nil || role == nil {
		return 0, false, fmt.Errorf("resolve %s role: %w", name, err)
	}
	return role.Id, role.IsSuperadmin, nil
}

// adminRoleId is the id of the superadmin role — what "an admin" means now that it is a
// role rather than a bool.
func (s *localUserService) adminRoleId(ctx context.Context) (int64, error) {
	if s.roles == nil {
		return 0, fmt.Errorf("roles are not configured")
	}
	role, err := s.roles.GetByName(ctx, RoleSuperadmin)
	if err != nil || role == nil {
		return 0, fmt.Errorf("resolve superadmin role: %w", err)
	}
	return role.Id, nil
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// identity builds the request-scoped principal.
//
// IsAdmin is DERIVED from the user's role (its IsSuperadmin flag), never read from the
// legacy LocalUser.IsAdmin column. That is what keeps one source of truth: RoleId decides
// everything, and the handlers that still ask "is this an admin?" get an answer consistent
// with the matrix rather than a bool that could disagree with it.
//
// A role that cannot be resolved is an ERROR, not a quiet downgrade to non-admin. Silently
// demoting the only administrator because a lookup failed would lock the operator out of
// their own appliance, and they would have no way to tell why.
func (s *localUserService) identity(ctx context.Context, user *entities.LocalUser) (*AuthenticatedUser, error) {
	isAdmin := false
	if user.RoleId > 0 && s.roles != nil {
		role, err := s.roles.GetById(ctx, user.RoleId)
		if err != nil {
			return nil, fmt.Errorf("resolve role %d for %q: %w", user.RoleId, user.Username, err)
		}
		if role != nil {
			isAdmin = role.IsSuperadmin
		}
	}
	return &AuthenticatedUser{
		Id:                 user.Id,
		Username:           user.Username,
		DisplayName:        user.DisplayName,
		RoleId:             user.RoleId,
		IsAdmin:            isAdmin,
		MustChangePassword: user.MustChangePassword,
		SessionHash:        localSessionHash(user),
	}, nil
}

func localSessionHash(user *entities.LocalUser) string {
	if user == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(user.Username + "\x00" + user.PasswordHash + "\x00" + boolLabel(user.IsAdmin) + "\x00" + boolLabel(user.IsActive)))
	return hex.EncodeToString(sum[:])
}

func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func hashLocalPassword(raw string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

// isNoResultFoundErr reports whether an error is the repo's "row not present" sentinel,
// which the generic repo signals by message rather than by a typed error.
func isNoResultFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no result found")
}
