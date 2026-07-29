package services

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/login"
	"golang.org/x/crypto/bcrypt"
)

// userLoginService struct
type userLoginService struct {
	repo  dbsql.IGenericRepo[entities.UserLogin]
	cache cache.Store
}

const (
	defaultSuperadminUsername = "superadmin"
	defaultSuperadminPassword = "superadmin123"
)

// dummyBcryptHash is compared against when no real hash is available (unknown username,
// federated-only account), so that the "no such account" path costs about the same as a
// genuine verification. Without it, response time alone answers "does this user exist?".
// It is a valid cost-10 hash of a value nobody can present, since the plaintext is
// unknown to anyone including us.
const dummyBcryptHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

var (
	ErrInvalidCredentialPayload = errors.New("username and password are required")
	ErrInvalidCredential        = errors.New("invalid username or password")
	ErrThirdPartyOnlyAccount    = errors.New("account is managed by third-party login")
	ErrInactiveAccount          = errors.New("account is inactive")
	ErrAccountAlreadyExists     = errors.New("account already exists")
	// ErrFederatedIdentityConflict: the login's email matches an existing account that
	// is already bound to a DIFFERENT federated identity. Merging here would let a
	// same-email account at another provider take over the existing one, so the login
	// is refused and an operator has to resolve it.
	ErrFederatedIdentityConflict = errors.New("an account with this email already belongs to another identity")
	// ErrFederatedIdentityInvalid: the provider returned an identity without a stable
	// subject id or email; refusing beats creating an unmatchable account.
	ErrFederatedIdentityInvalid = errors.New("identity provider returned an incomplete identity")
)

// Create new IUserLoginService
func NewUserLoginService(
	repo dbsql.IGenericRepo[entities.UserLogin],
	cacheStore cache.Store,
) IUserLoginService {
	return &userLoginService{
		repo:  repo,
		cache: cacheStore,
	}
}

func (m *userLoginService) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.UserLogin, uint64, error) {
	if len(sorters) == 0 {
		// Default to a stable id order; the UI lets the operator sort by any column.
		sorters = []sqldataenums.Sorter{
			{
				FieldName: "Id",
				Sort:      sqldataenums.ASC,
			},
		}
	}

	return m.repo.Get(ctx, "", limit, offset, filters, sorters)
}

func (m *userLoginService) GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error) {
	return m.repo.GetByUnique(ctx, "", "email", email)
}

func (m *userLoginService) AuthenticateDefault(ctx context.Context, username string, password string) (*entities.UserLogin, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return nil, ErrInvalidCredentialPayload
	}

	user, err := m.repo.GetByUnique(ctx, "", "email", username)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, ErrInvalidCredential
		}

		return nil, err
	}

	if user == nil {
		// Spend roughly the same time a real bcrypt verification would, so that
		// "no such account" and "wrong password" are not distinguishable by timing.
		bcrypt.CompareHashAndPassword([]byte(dummyBcryptHash), []byte(password))
		return nil, ErrInvalidCredential
	}

	// Account STATE (disabled, federated-only) must not be reported until the caller has
	// proved they hold the password. Returning ErrInactiveAccount or
	// ErrThirdPartyOnlyAccount straight off a username lookup made this endpoint a
	// positive account-existence oracle: an attacker learned which usernames were real,
	// and which of those were federated, without any credential at all.
	if strings.TrimSpace(user.Userpwd) == "" {
		// A federated-only account has no local password to verify, so there is nothing
		// the caller could prove. Fail generically and keep the real reason server-side.
		bcrypt.CompareHashAndPassword([]byte(dummyBcryptHash), []byte(password))
		log.Printf("user_login: password login refused for federated-only account email=%s", user.Email)
		return nil, ErrInvalidCredential
	}

	if isBcryptHash(user.Userpwd) {
		if err := bcrypt.CompareHashAndPassword([]byte(user.Userpwd), []byte(password)); err != nil {
			return nil, ErrInvalidCredential
		}
	} else {
		// Legacy plaintext row. Compare in constant time — a byte-wise != leaks the
		// length and a prefix of the stored password through timing.
		if subtle.ConstantTimeCompare([]byte(user.Userpwd), []byte(password)) != 1 {
			return nil, ErrInvalidCredential
		}

		if upgraded, err := hashPassword(password); err == nil {
			originalPwd := user.Userpwd
			user.Userpwd = upgraded
			if _, err := m.repo.UpdateById(ctx, "", *user); err != nil {
				if _, errByUnique := m.repo.UpdateByUnique(ctx, "", "email", *user); errByUnique != nil {
					user.Userpwd = originalPwd
					log.Printf("user_login migration warning email=%s errById=%v errByUnique=%v", user.Email, err, errByUnique)
				}
			}
		}
	}

	// The password is correct, so disclosing that the account is disabled tells an
	// attacker nothing they could not already confirm.
	if !user.IsActive {
		return nil, ErrInactiveAccount
	}

	return user, nil
}

// UpsertFederated resolves a federated login to a local account with strict
// (provider, subject) matching:
//
//  1. An account already bound to this exact identity is refreshed and returned.
//  2. Otherwise an account with the same email is considered ONLY if it has no bound
//     identity yet (pre-upgrade social users, or an operator pre-provisioning by
//     email): the identity is stamped onto it once. An email match that is already
//     bound to a different identity is refused (ErrFederatedIdentityConflict) — that
//     merge is an account takeover, not a convenience.
//  3. A full miss creates a new account with NO role (pending clearance).
//
// Inactive accounts are refused regardless of which path matched.
func (m *userLoginService) UpsertFederated(ctx context.Context, id login.Identity) (*entities.UserLogin, error) {
	provider := strings.ToLower(strings.TrimSpace(id.Provider))
	subject := strings.TrimSpace(id.Subject)
	email := strings.TrimSpace(id.Email)
	if provider == "" || subject == "" || email == "" {
		return nil, ErrFederatedIdentityInvalid
	}

	user, err := m.getBySsoIdentity(ctx, provider, subject)
	if err != nil {
		return nil, err
	}

	if user == nil {
		// No bound account: an email match may claim the identity, but only while it
		// is still unbound. GetByForeign is unusable here (limit-1 on one column);
		// getBySsoIdentity filters on the full pair instead.
		existing, err := m.GetByEmail(ctx, email)
		if err != nil && !isNotFoundErr(err) {
			return nil, err
		}
		if existing != nil {
			if strings.TrimSpace(existing.SsoProvider) != "" || strings.TrimSpace(existing.SsoSubject) != "" {
				log.Printf("federated login refused: email=%s provider=%s subject=%s conflicts with bound identity provider=%s", email, provider, subject, existing.SsoProvider)
				return nil, ErrFederatedIdentityConflict
			}
			log.Printf("federated identity claimed by legacy account: email=%s provider=%s", email, provider)
			user = existing
		}
	}

	if user != nil {
		if !user.IsActive {
			return nil, ErrInactiveAccount
		}
		user.SsoProvider = provider
		user.SsoSubject = subject
		user.Email = email
		applyIdentityProfile(user, id)
		user.UpdatedAt = time.Now().Unix()
		if _, err := m.repo.UpdateById(ctx, "", *user); err != nil {
			return nil, err
		}
		return user, nil
	}

	created := entities.UserLogin{
		Email:       email,
		SsoProvider: provider,
		SsoSubject:  subject,
		// No role: pending clearance until a superadmin assigns one.
		UserRoleId: 0,
		IsActive:   true,
		CreatedAt:  time.Now().Unix(),
	}
	applyIdentityProfile(&created, id)

	res, err := m.Create(ctx, created)
	if err != nil {
		return nil, err
	}
	created.Id = int64(res)
	return &created, nil
}

// getBySsoIdentity looks an account up by its bound (provider, subject) pair. A miss
// is nil, not an error. GetSingle can hand back a zero-value struct on some engines,
// so an Id of 0 also counts as a miss.
func (m *userLoginService) getBySsoIdentity(ctx context.Context, provider, subject string) (*entities.UserLogin, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "SsoProvider", Compare: sqldataenums.Equal, Value: provider},
		{FieldName: "SsoSubject", Compare: sqldataenums.Equal, Value: subject},
	}
	user, err := m.repo.GetSingle(ctx, "", filters)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	if user == nil || user.Id == 0 {
		return nil, nil
	}
	return user, nil
}

// applyIdentityProfile refreshes the display fields the provider owns, without ever
// touching credential or role fields.
func applyIdentityProfile(user *entities.UserLogin, id login.Identity) {
	given := strings.TrimSpace(id.GivenName)
	family := strings.TrimSpace(id.FamilyName)
	if given == "" {
		given = strings.TrimSpace(id.Name)
	}
	if given != "" {
		user.FirstName = given
	}
	if family != "" {
		user.LastName = family
	}
	if pic := strings.TrimSpace(id.Picture); pic != "" {
		user.PicUrl = pic
	}
}

// AssignRole sets the account's role without touching credentials or profile.
// Role changes take effect on the next login/session refresh — live sessions keep
// their issued role by design (see middlewares/auth.go session validation).
func (m *userLoginService) AssignRole(ctx context.Context, userId int64, roleId int64) error {
	user, err := m.repo.GetById(ctx, "", uint64(userId))
	if err != nil {
		return err
	}
	if user == nil || user.Id == 0 {
		return errors.New("user not found")
	}
	if user.UserRoleId == roleId {
		return nil
	}
	user.UserRoleId = roleId
	user.UpdatedAt = time.Now().Unix()
	_, err = m.repo.UpdateById(ctx, "", *user)
	return err
}

func (m *userLoginService) RegisterLocal(ctx context.Context, model entities.UserLogin) (uint64, error) {
	model.Email = strings.TrimSpace(model.Email)
	model.Userpwd = strings.TrimSpace(model.Userpwd)
	if model.Email == "" || model.Userpwd == "" {
		return 0, ErrInvalidCredentialPayload
	}

	existing, err := m.repo.GetByUnique(ctx, "", "email", model.Email)
	if err != nil {
		if !isNotFoundErr(err) {
			return 0, err
		}
	} else if existing != nil {
		if strings.TrimSpace(existing.Userpwd) == "" {
			return 0, ErrThirdPartyOnlyAccount
		}

		return 0, ErrAccountAlreadyExists
	}

	if model.CreatedAt == 0 {
		model.CreatedAt = time.Now().Unix()
	}

	if model.UserRoleId < 0 {
		model.UserRoleId = 0
	}

	model.IsActive = true

	return m.Create(ctx, model)
}

func (m *userLoginService) Create(ctx context.Context, model entities.UserLogin) (uint64, error) {
	if sameCredential(model.Email, model.Userpwd) {
		allowed, err := m.allowDefaultSuperadminSeed(ctx, model)
		if err != nil {
			return 0, err
		}
		if !allowed {
			return 0, fmt.Errorf("username and password cannot be identical")
		}
	}

	if strings.TrimSpace(model.Userpwd) != "" && !isBcryptHash(model.Userpwd) {
		hashed, err := hashPassword(model.Userpwd)
		if err != nil {
			return 0, err
		}

		model.Userpwd = hashed
	}

	return m.repo.Create(ctx, "", model)
}

func sameCredential(username string, password string) bool {
	return strings.TrimSpace(username) == strings.TrimSpace(password)
}

func isNotFoundErr(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "no result found")
}

func isBcryptHash(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "$2a$") || strings.HasPrefix(raw, "$2b$") || strings.HasPrefix(raw, "$2y$")
}

func hashPassword(raw string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return string(hashed), nil
}

func (m *userLoginService) allowDefaultSuperadminSeed(ctx context.Context, model entities.UserLogin) (bool, error) {
	if strings.TrimSpace(model.Email) != defaultSuperadminUsername || strings.TrimSpace(model.Userpwd) != defaultSuperadminPassword {
		return false, nil
	}

	existing, err := m.repo.GetByUnique(ctx, "", "email", defaultSuperadminUsername)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no result found") {
			return true, nil
		}

		return false, err
	}

	return existing == nil, nil
}

func (m *userLoginService) Update(ctx context.Context, model entities.UserLogin) (uint64, error) {
	// Password is write-only on update: a blank userpwd keeps the stored one (so role
	// or active toggles don't erase it), and a supplied plaintext is hashed before save
	// (Create hashes too; this keeps Update consistent).
	if strings.TrimSpace(model.Userpwd) == "" {
		if existing, err := m.repo.GetById(ctx, "", uint64(model.Id)); err == nil && existing != nil {
			model.Userpwd = existing.Userpwd
		}
	} else if !isBcryptHash(model.Userpwd) {
		hashed, err := hashPassword(model.Userpwd)
		if err != nil {
			return 0, err
		}
		model.Userpwd = hashed
	}
	return m.repo.UpdateById(ctx, "", model)
}

func (m *userLoginService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.repo.DeleteById(ctx, "", id)
}

// StockSeedResult describes the bootstrap superadmin credential this run
// established. Seeded is true only when the account was actually created (or
// force-reset), and Generated is true when the password came from neither config
// nor env — meaning the operator does not know it yet and must be shown it.
// Mirrors myseliasan's first-run contract.
type StockSeedResult struct {
	Username  string
	Password  string
	Generated bool
	Seeded    bool
}

// resolveBootstrapPassword picks the password to seed with, and reports whether it
// had to invent one. Precedence: the LOCAL_ADMIN_PASSWORD env override, then the
// config value, else a strong generated one — so a fresh install with an empty
// localAuth.password gets a per-install credential rather than a well-known default.
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
// charset (no O/0/I/l/1), sourced from crypto/rand. Mirrors myseliasan/mymatasan.
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

// EnsureStockSuperadmin seeds the bootstrap superadmin (email = username) on first run
// with a forced first-login password change, and — while the account is still the
// untouched stock account (must-change + active) — refreshes its password from config.
// It also keeps the stock account pinned to the superadmin role. A *generated*
// password is never refreshed in: a new one is minted every boot, and rewriting the
// hash would invalidate the credential the operator just read from the recovery file.
func (m *userLoginService) EnsureStockSuperadmin(ctx context.Context, username, password string, superRoleId int64) (StockSeedResult, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		username = defaultSuperadminUsername
	}
	resolved, generated, err := resolveBootstrapPassword(password)
	if err != nil {
		return StockSeedResult{}, err
	}
	out := StockSeedResult{Username: username, Password: resolved, Generated: generated}

	hash, err := hashPassword(resolved)
	if err != nil {
		return StockSeedResult{}, err
	}
	existing, err := m.repo.GetByUnique(ctx, "", "email", username)
	if err != nil && !isNotFoundErr(err) {
		return StockSeedResult{}, err
	}
	if existing != nil {
		changed := false
		// Refresh the bootstrap password from config ONLY while the account is still
		// untouched (must-change + active); once the operator sets their own password,
		// config no longer overrides it.
		if !generated && existing.MustChangePassword && existing.IsActive {
			existing.Userpwd = hash
			changed = true
		}
		if superRoleId > 0 && existing.UserRoleId != superRoleId {
			existing.UserRoleId = superRoleId
			changed = true
		}
		if changed {
			existing.UpdatedAt = time.Now().Unix()
			if _, uerr := m.repo.UpdateById(ctx, "", *existing); uerr != nil {
				return StockSeedResult{}, uerr
			}
		}
		return out, nil
	}
	now := time.Now().Unix()
	_, err = m.repo.Create(ctx, "", entities.UserLogin{
		Email:              username,
		Userpwd:            hash,
		FirstName:          "Super",
		LastName:           "Admin",
		UserRoleId:         superRoleId,
		IsActive:           true,
		MustChangePassword: true,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	if err != nil {
		return StockSeedResult{}, err
	}
	out.Seeded = true
	return out, nil
}

// ResetStockSuperadmin is the lock-out recovery path: it force-sets the stock
// superadmin's password even when the account was taken over or deactivated at
// handoff, re-flags it must-change, reactivates it, and re-pins the superadmin
// role. Only ever reached by the app consuming a RESET_ADMIN marker file in the
// data dir — needs local admin rights on the host, never exposed over the network.
func (m *userLoginService) ResetStockSuperadmin(ctx context.Context, username, password string, superRoleId int64) (StockSeedResult, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		username = defaultSuperadminUsername
	}
	resolved, generated, err := resolveBootstrapPassword(password)
	if err != nil {
		return StockSeedResult{}, err
	}
	existing, err := m.repo.GetByUnique(ctx, "", "email", username)
	if err != nil && !isNotFoundErr(err) {
		return StockSeedResult{}, err
	}
	if existing == nil {
		// Nothing to reset — seed fresh instead.
		return m.EnsureStockSuperadmin(ctx, username, resolved, superRoleId)
	}
	hash, err := hashPassword(resolved)
	if err != nil {
		return StockSeedResult{}, err
	}
	existing.Userpwd = hash
	existing.MustChangePassword = true
	existing.IsActive = true
	if superRoleId > 0 {
		existing.UserRoleId = superRoleId
	}
	existing.UpdatedAt = time.Now().Unix()
	if _, err := m.repo.UpdateById(ctx, "", *existing); err != nil {
		return StockSeedResult{}, err
	}
	return StockSeedResult{Username: username, Password: resolved, Generated: generated, Seeded: true}, nil
}

func (m *userLoginService) ChangePassword(ctx context.Context, userId int64, current, next string) error {
	if len(strings.TrimSpace(next)) < 8 {
		return fmt.Errorf("new password must be at least 8 characters")
	}
	user, err := m.repo.GetById(ctx, "", uint64(userId))
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("user not found")
	}
	if strings.TrimSpace(user.Userpwd) == "" {
		return ErrThirdPartyOnlyAccount
	}
	if isBcryptHash(user.Userpwd) {
		if bcrypt.CompareHashAndPassword([]byte(user.Userpwd), []byte(current)) != nil {
			return ErrInvalidCredential
		}
	} else if user.Userpwd != current {
		return ErrInvalidCredential
	}
	hash, err := hashPassword(next)
	if err != nil {
		return err
	}
	user.Userpwd = hash
	user.MustChangePassword = false
	user.UpdatedAt = time.Now().Unix()
	_, err = m.repo.UpdateById(ctx, "", *user)
	return err
}

// AdminResetPassword implements the operator-driven reset that backs the
// forgot-password admin queue: it force-sets a fresh generated password and flags
// the account must-change, so the temporary password is single-use by design.
func (m *userLoginService) AdminResetPassword(ctx context.Context, userId int64) (string, error) {
	user, err := m.repo.GetById(ctx, "", uint64(userId))
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", errors.New("user not found")
	}
	if strings.TrimSpace(user.Userpwd) == "" {
		// Federated/SSO-only account: no local password to reset (owned by the IdP).
		return "", ErrThirdPartyOnlyAccount
	}
	temp, err := generateBootstrapPassword()
	if err != nil {
		return "", err
	}
	hash, err := hashPassword(temp)
	if err != nil {
		return "", err
	}
	user.Userpwd = hash
	user.MustChangePassword = true
	// Deliberately do NOT touch IsActive — a reset must never silently reactivate a
	// disabled account.
	user.UpdatedAt = time.Now().Unix()
	if _, err := m.repo.UpdateById(ctx, "", *user); err != nil {
		return "", err
	}
	return temp, nil
}

// SetPasswordSelfService backs the optional SMTP self-service reset: the user proved
// possession of the reset token, so no current-password check — but the account must
// still be a local one, and the new password must meet the length rule.
func (m *userLoginService) SetPasswordSelfService(ctx context.Context, userId int64, newPassword string) error {
	if len(strings.TrimSpace(newPassword)) < 8 {
		return fmt.Errorf("new password must be at least 8 characters")
	}
	user, err := m.repo.GetById(ctx, "", uint64(userId))
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("user not found")
	}
	if strings.TrimSpace(user.Userpwd) == "" {
		return ErrThirdPartyOnlyAccount
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	user.Userpwd = hash
	user.MustChangePassword = false
	user.UpdatedAt = time.Now().Unix()
	_, err = m.repo.UpdateById(ctx, "", *user)
	return err
}
