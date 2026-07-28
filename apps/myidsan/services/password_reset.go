package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/mailer"
)

// Password-reset errors surfaced to the API layer.
var (
	ErrResetRequestNotFound = errors.New("reset request not found")
	ErrResetTokenInvalid    = errors.New("reset link is invalid or has expired")
)

const (
	resetStatusPending  = "pending"
	resetStatusResolved = "resolved"
	resetChannelAdmin   = "admin"
	resetChannelSelf    = "self"

	// resetTokenTTL bounds the self-service email link. Short enough to limit a leaked
	// link, long enough for a user to act on an internal mail.
	resetTokenTTL      = 30 * time.Minute
	resetTokenCacheKey = "pwreset:token:"
)

// resetTokenEntry is the cache value behind a self-service reset link. It grants
// only the right to set a new password for one resolved account, once.
type resetTokenEntry struct {
	UserId    int64  `json:"userId"`
	RequestId int64  `json:"requestId"`
	Email     string `json:"email"`
}

// IPasswordResetService is myidsan's account-recovery flow: a public request that
// feeds an operator queue (always available, air-gap safe) and — when an internal
// SMTP relay is configured — an optional self-service email link.
type IPasswordResetService interface {
	// Request handles a public "forgot password" submission. It resolves a LOCAL
	// account for the identifier; on a match it records a pending queue entry and,
	// when mail is enabled, emails a self-service link built from origin. It returns
	// no signal about whether an account matched — the caller ALWAYS responds
	// generically (no account-enumeration oracle). A transient email failure is
	// swallowed (the admin queue still covers the user); only storage errors bubble.
	Request(ctx context.Context, identifier, requestIp, origin string) error
	// MailEnabled reports whether the optional self-service email path is active.
	MailEnabled() bool
	// ListPending returns the open operator queue, newest first.
	ListPending(ctx context.Context) ([]*myidsanentities.PasswordResetRequest, error)
	// Resolve issues a fresh temporary password for the request's account, marks the
	// request resolved (admin channel), and returns the plaintext ONCE.
	Resolve(ctx context.Context, requestId, adminId int64) (string, error)
	// Dismiss closes a request without issuing a password (spam/duplicate).
	Dismiss(ctx context.Context, requestId, adminId int64) error
	// ResolveToken returns the account email for a valid self-service token, for
	// rendering the set-new-password page.
	ResolveToken(ctx context.Context, token string) (string, error)
	// CompleteSelfService validates the token, sets the user-chosen password, marks
	// the originating request resolved (self channel), and consumes the token.
	CompleteSelfService(ctx context.Context, token, newPassword string) error
}

type passwordResetService struct {
	repo   dbsql.IGenericRepo[myidsanentities.PasswordResetRequest]
	users  IUserLoginService
	mailer *mailer.Mailer
	store  cache.Store
}

// NewPasswordResetService constructs the service. mailer may be nil/disabled — the
// queue works either way.
func NewPasswordResetService(
	repo dbsql.IGenericRepo[myidsanentities.PasswordResetRequest],
	users IUserLoginService,
	mail *mailer.Mailer,
	store cache.Store,
) IPasswordResetService {
	return &passwordResetService{repo: repo, users: users, mailer: mail, store: store}
}

func (m *passwordResetService) MailEnabled() bool { return m.mailer != nil && m.mailer.Enabled() }

func (m *passwordResetService) Request(ctx context.Context, identifier, requestIp, origin string) error {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil // generic response; nothing to do
	}
	user, err := m.users.GetByEmail(ctx, identifier)
	if err != nil {
		if isNotFoundErr(err) {
			return nil // unknown account — say nothing (no oracle)
		}
		return err
	}
	if user == nil || user.Id == 0 || strings.TrimSpace(user.Userpwd) == "" {
		// No account, or a federated/SSO-only account with no local password to reset.
		return nil
	}

	now := time.Now().Unix()
	req := myidsanentities.PasswordResetRequest{
		UserLoginId: user.Id,
		Email:       user.Email,
		Status:      resetStatusPending,
		RequestIp:   requestIp,
		RequestedAt: now,
	}
	id, err := m.repo.Create(ctx, "", req)
	if err != nil {
		return err
	}

	// Optional self-service email. A failure here is non-fatal: the operator queue
	// entry above already guarantees the user can recover.
	if m.MailEnabled() {
		if err := m.sendSelfServiceEmail(ctx, user.Id, int64(id), user.Email, origin); err != nil {
			// Swallow — do not leak send failures to the caller, and never block the
			// generic response. (Logged by the API layer if desired.)
			return nil
		}
	}
	return nil
}

func (m *passwordResetService) sendSelfServiceEmail(ctx context.Context, userId, requestId int64, email, origin string) error {
	token, err := newResetToken()
	if err != nil {
		return err
	}
	entry := resetTokenEntry{UserId: userId, RequestId: requestId, Email: email}
	if err := m.store.Set(ctx, resetTokenCacheKey+token, entry, resetTokenTTL); err != nil {
		return err
	}
	link := strings.TrimRight(origin, "/") + "/api/auth/reset?token=" + token
	subject := "Reset your MyIDSan password"
	body := fmt.Sprintf(
		"A password reset was requested for your account (%s).\n\n"+
			"Open this link to choose a new password (valid for 30 minutes):\n\n%s\n\n"+
			"If you did not request this, you can ignore this message — your password is unchanged.\n",
		email, link)
	return m.mailer.Send(email, subject, body)
}

func (m *passwordResetService) ListPending(ctx context.Context) ([]*myidsanentities.PasswordResetRequest, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "Status", Compare: sqldataenums.Equal, Value: resetStatusPending},
	}
	sorters := []sqldataenums.Sorter{
		{FieldName: "RequestedAt", Sort: sqldataenums.DESC},
	}
	rows, _, err := m.repo.Get(ctx, "", 500, 0, filters, sorters)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	return rows, nil
}

func (m *passwordResetService) loadRequest(ctx context.Context, requestId int64) (*myidsanentities.PasswordResetRequest, error) {
	req, err := m.repo.GetById(ctx, "", uint64(requestId))
	if err != nil {
		if isNotFoundErr(err) {
			return nil, ErrResetRequestNotFound
		}
		return nil, err
	}
	if req == nil || req.Id == 0 {
		return nil, ErrResetRequestNotFound
	}
	return req, nil
}

func (m *passwordResetService) Resolve(ctx context.Context, requestId, adminId int64) (string, error) {
	req, err := m.loadRequest(ctx, requestId)
	if err != nil {
		return "", err
	}
	temp, err := m.users.AdminResetPassword(ctx, req.UserLoginId)
	if err != nil {
		return "", err
	}
	if err := m.markResolved(ctx, req, resetChannelAdmin, adminId); err != nil {
		return "", err
	}
	return temp, nil
}

func (m *passwordResetService) Dismiss(ctx context.Context, requestId, adminId int64) error {
	req, err := m.loadRequest(ctx, requestId)
	if err != nil {
		return err
	}
	return m.markResolved(ctx, req, "", adminId)
}

func (m *passwordResetService) markResolved(ctx context.Context, req *myidsanentities.PasswordResetRequest, channel string, adminId int64) error {
	req.Status = resetStatusResolved
	req.Channel = channel
	req.ResolvedBy = adminId
	req.ResolvedAt = time.Now().Unix()
	_, err := m.repo.UpdateById(ctx, "", *req)
	return err
}

func (m *passwordResetService) ResolveToken(ctx context.Context, token string) (string, error) {
	entry, err := m.lookupToken(ctx, token)
	if err != nil {
		return "", err
	}
	return entry.Email, nil
}

func (m *passwordResetService) CompleteSelfService(ctx context.Context, token, newPassword string) error {
	entry, err := m.lookupToken(ctx, token)
	if err != nil {
		return err
	}
	if err := m.users.SetPasswordSelfService(ctx, entry.UserId, newPassword); err != nil {
		return err
	}
	// Single-use: consume the token so the link cannot be replayed.
	_ = m.store.Delete(ctx, resetTokenCacheKey+token)
	// Best-effort: mark the originating queue entry resolved (self channel).
	if entry.RequestId > 0 {
		if req, err := m.loadRequest(ctx, entry.RequestId); err == nil && req.Status == resetStatusPending {
			_ = m.markResolved(ctx, req, resetChannelSelf, entry.UserId)
		}
	}
	return nil
}

func (m *passwordResetService) lookupToken(ctx context.Context, token string) (*resetTokenEntry, error) {
	token = strings.TrimSpace(token)
	if token == "" || m.store == nil {
		return nil, ErrResetTokenInvalid
	}
	var entry resetTokenEntry
	found, err := m.store.Get(ctx, resetTokenCacheKey+token, &entry)
	if err != nil {
		return nil, err
	}
	if !found || entry.UserId == 0 {
		return nil, ErrResetTokenInvalid
	}
	return &entry, nil
}

func newResetToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
