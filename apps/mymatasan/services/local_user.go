package services

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultLocalAdminUsername = "admin"
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

func (s *localUserService) EnsureDefaultAdmin(ctx context.Context) error {
	_, total, err := s.repo.Get(ctx, "", 1, 0, nil, nil)
	if err != nil {
		return err
	}
	if total > 0 {
		return nil
	}
	_, err = s.Create(ctx, CreateLocalUserRequest{
		Username:    defaultLocalAdminUsername,
		Password:    defaultLocalAdminPassword,
		DisplayName: "Administrator",
		IsAdmin:     true,
		IsActive:    true,
	})
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
		Username:     username,
		PasswordHash: hashed,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		IsAdmin:      req.IsAdmin,
		IsActive:     req.IsActive,
		CreatedAt:    now,
		UpdatedAt:    now,
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
	user.UpdatedAt = time.Now().UTC().Unix()
	if _, err := s.repo.UpdateById(ctx, "", *user); err != nil {
		return nil, err
	}
	return user, nil
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
		Id:          user.Id,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		IsAdmin:     user.IsAdmin,
		SessionHash: localSessionHash(user),
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
