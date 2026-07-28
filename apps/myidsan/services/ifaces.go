package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/login"
)

// IUserLoginService interface
type IUserLoginService interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.UserLogin, uint64, error)
	GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error)
	AuthenticateDefault(ctx context.Context, username string, password string) (*entities.UserLogin, error)
	// UpsertFederated resolves a federated login to a local account with strict
	// (provider, subject) matching; an unbound same-email account may claim the
	// identity once, a bound one refuses the login (ErrFederatedIdentityConflict).
	// Full miss creates a pending-clearance account.
	UpsertFederated(ctx context.Context, id login.Identity) (*entities.UserLogin, error)
	// AssignRole sets only the account's role (used by federated group→role
	// mapping); credentials and profile are untouched.
	AssignRole(ctx context.Context, userId int64, roleId int64) error
	RegisterLocal(ctx context.Context, model entities.UserLogin) (uint64, error)
	Create(ctx context.Context, model entities.UserLogin) (uint64, error)
	Update(ctx context.Context, model entities.UserLogin) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
	// EnsureStockSuperadmin seeds (or refreshes, while still untouched) the bootstrap
	// superadmin from config (email = username), forcing a first-login password change.
	// The seed result reports whether the account was freshly created and whether the
	// password was generated (empty config/env) — the first-run banner needs both.
	EnsureStockSuperadmin(ctx context.Context, username, password string, superRoleId int64) (StockSeedResult, error)
	// ResetStockSuperadmin force-resets the stock superadmin (lock-out recovery via
	// the RESET_ADMIN marker file); recreates the account if it is gone.
	ResetStockSuperadmin(ctx context.Context, username, password string, superRoleId int64) (StockSeedResult, error)
	// ChangePassword verifies the current password, sets a new (hashed) one, and clears
	// the must-change-password flag.
	ChangePassword(ctx context.Context, userId int64, current, next string) error
	// AdminResetPassword force-sets a fresh generated temporary password on a LOCAL
	// account, flags it must-change, and returns the plaintext ONCE for an operator to
	// hand over out-of-band. Refuses a third-party-only (federated) account and never
	// reactivates a disabled one.
	AdminResetPassword(ctx context.Context, userId int64) (string, error)
	// SetPasswordSelfService sets a user-chosen new password on a LOCAL account
	// (min length enforced) and clears the must-change flag — completes a self-service
	// reset token. Refuses a third-party-only account.
	SetPasswordSelfService(ctx context.Context, userId int64, newPassword string) error
}

// IUserGroupService interface
type IUserGroupService interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.UserGroup, uint64, error)
	Create(ctx context.Context, model entities.UserGroup) (uint64, error)
	Update(ctx context.Context, model entities.UserGroup) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}
