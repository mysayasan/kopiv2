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
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
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

type localUserService struct {
	repo dbsql.IGenericRepo[entities.LocalUser]
}

// NewLocalUserService creates a standalone local user service for mymatasan.
func NewLocalUserService(repo dbsql.IGenericRepo[entities.LocalUser]) ILocalUserService {
	return &localUserService{repo: repo}
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
	return localUserIdentity(user), nil
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
	return localUserIdentity(user), nil
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
	now := time.Now().UTC().Unix()
	model := entities.LocalUser{
		Username:           username,
		PasswordHash:       hashed,
		DisplayName:        strings.TrimSpace(req.DisplayName),
		IsAdmin:            req.IsAdmin,
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
	if err := s.ensureNotRemovingLastAdmin(ctx, user, req.IsAdmin, req.IsActive); err != nil {
		return nil, err
	}
	user.Username = username
	user.DisplayName = strings.TrimSpace(req.DisplayName)
	user.IsAdmin = req.IsAdmin
	user.IsActive = req.IsActive
	user.UpdatedAt = time.Now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *user); err != nil {
		return nil, err
	}
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
	return localUserIdentity(user), nil
}

func (s *localUserService) Delete(ctx context.Context, id uint64) (uint64, error) {
	user, err := s.repo.GetById(ctx, "", id)
	if err != nil {
		return 0, err
	}
	if err := s.ensureNotRemovingLastAdmin(ctx, user, false, false); err != nil {
		return 0, err
	}
	return s.repo.DeleteById(ctx, "", id)
}

func (s *localUserService) ensureNotRemovingLastAdmin(ctx context.Context, user *entities.LocalUser, nextIsAdmin bool, nextIsActive bool) error {
	if user == nil || !user.IsAdmin || !user.IsActive || (nextIsAdmin && nextIsActive) {
		return nil
	}
	filters := []sqldataenums.Filter{
		{FieldName: "IsAdmin", Compare: sqldataenums.Equal, Value: true},
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

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func localUserIdentity(user *entities.LocalUser) *AuthenticatedUser {
	return &AuthenticatedUser{
		Id:                 user.Id,
		Username:           user.Username,
		DisplayName:        user.DisplayName,
		IsAdmin:            user.IsAdmin,
		MustChangePassword: user.MustChangePassword,
		SessionHash:        localSessionHash(user),
	}
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
