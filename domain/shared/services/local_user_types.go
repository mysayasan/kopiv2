package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// The appliance user model — the login/identity types shared by every appliance app
// (mymatasan, myiotsan). They live here rather than in each app because this is
// security-critical code: a fix to the bcrypt handling or the session comparison must
// land in ONE place, not be chased across forks.

// AuthenticatedUser is the local user identity attached to authenticated requests.
type AuthenticatedUser struct {
	Id          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	// RoleId is the authority: every request is decided against this role's permission
	// matrix.
	RoleId int64 `json:"roleId"`
	// IsAdmin is DERIVED from the role's IsSuperadmin flag — it is not read from the legacy
	// LocalUser.IsAdmin column. It exists so the handlers that ask "is this an admin?"
	// (the settings self-gates, the control dispatcher) get an answer that cannot disagree
	// with the matrix.
	IsAdmin            bool   `json:"isAdmin"`
	MustChangePassword bool   `json:"mustChangePassword"`
	SessionHash        string `json:"-"`
}

// AdminSeedResult reports what EnsureDefaultAdmin did on startup so the caller can
// surface the bootstrap credentials on a fresh install (first-run console banner +
// recovery file for CLI / Docker / package / portable runs, which have no GUI
// installer finish page). The seeded account is always must-change on first login.
type AdminSeedResult struct {
	// Seeded is true only when a fresh admin was created this run (empty user table).
	Seeded bool
	// Username is the seeded admin's username.
	Username string
	// Password is the plaintext bootstrap password. Set only when Seeded.
	Password string
	// Generated is true when the password was randomly generated because neither the
	// config nor the LOCAL_ADMIN_PASSWORD env var supplied one — i.e. it is not known
	// to the operator yet and must be revealed.
	Generated bool
}

type CreateLocalUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
	// RoleId is the authority. When it is 0 the legacy IsAdmin bool is used instead
	// (admin -> superadmin, otherwise viewer), so a client that predates roles keeps working.
	RoleId             int64 `json:"roleId"`
	IsAdmin            bool  `json:"isAdmin"`
	IsActive           bool  `json:"isActive"`
	MustChangePassword bool  `json:"mustChangePassword"`
}

type UpdateLocalUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	// RoleId is the authority; 0 falls back to the legacy IsAdmin bool. See
	// CreateLocalUserRequest.
	RoleId   int64 `json:"roleId"`
	IsAdmin  bool  `json:"isAdmin"`
	IsActive bool  `json:"isActive"`
}

// ChangeLocalUserPasswordRequest is the self-service password change body.
type ChangeLocalUserPasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type ResetLocalUserPasswordRequest struct {
	Password string `json:"password"`
}

// ILocalUserService manages standalone mymatasan login users.
type ILocalUserService interface {
	EnsureDefaultAdmin(ctx context.Context, username, password string) (AdminSeedResult, error)
	// ResetAdmin forces the admin password back to a bootstrap credential (locked-out
	// recovery, e.g. an installer "reset admin login" reinstall). Must-change; the
	// result carries the credential to reveal.
	ResetAdmin(ctx context.Context, username, password string) (AdminSeedResult, error)
	Authenticate(ctx context.Context, username string, password string) (*AuthenticatedUser, error)
	AuthenticateSession(ctx context.Context, username string, sessionHash string) (*AuthenticatedUser, error)
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.LocalUser, uint64, error)
	Create(ctx context.Context, req CreateLocalUserRequest) (*entities.LocalUser, error)
	Update(ctx context.Context, id uint64, req UpdateLocalUserRequest) (*entities.LocalUser, error)
	ResetPassword(ctx context.Context, id uint64, password string) (*entities.LocalUser, error)
	ChangePassword(ctx context.Context, userId int64, currentPassword string, newPassword string) (*AuthenticatedUser, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
	// BackfillRoles gives a role to every user that has none yet, derived from the legacy
	// IsAdmin bool. Runs at startup, after the roles are seeded; only touches RoleId == 0.
	BackfillRoles(ctx context.Context, adminRoleId, defaultRoleId int64) (int, error)
}
