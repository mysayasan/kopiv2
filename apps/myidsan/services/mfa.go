package services

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/mfa"
	"golang.org/x/crypto/bcrypt"
)

// MFA errors surfaced to the API layer.
var (
	ErrMfaAlreadyEnrolled = errors.New("a second factor is already enrolled")
	ErrMfaNotEnrolling    = errors.New("no pending enrollment to confirm")
	ErrMfaNotEnrolled     = errors.New("no confirmed second factor")
	ErrMfaBadCode         = errors.New("invalid verification code")
)

// mfaFactorKind is the only kind this table holds. Security keys were once expected to
// arrive as a second Kind here; they did not — they are MANY-per-user and carry an entirely
// different shape (public key, signature counter, transports), so they live in their own
// table and their own service (services/webauthn.go). The Kind filter on every lookup
// remains correct and cheap regardless, and does not assume a single row.
const mfaFactorKind = "totp"

// MfaStatus is the non-sensitive projection returned to the owning user and to the
// login gate. It NEVER carries the secret or the recovery codes.
type MfaStatus struct {
	Enrolled          bool   `json:"enrolled"`
	ConfirmedAt       int64  `json:"confirmedAt"`
	Label             string `json:"label"`
	RecoveryRemaining int    `json:"recoveryRemaining"`
}

// MfaVerifyResult is the outcome of checking one second-factor code. It reports not just
// whether the code was accepted but WHICH KIND was spent, because the two are different
// events to whoever reads the trail: a TOTP code is the routine case, while a recovery code
// is break-glass — a scarce, single-use secret that is gone once burned, and whose use is
// the strongest available signal that someone has lost (or taken over) an authenticator.
// Collapsed into a bare bool, the two are indistinguishable after the fact.
type MfaVerifyResult struct {
	Ok           bool
	UsedRecovery bool
}

// MfaEnrollChallenge is handed back once, at the start of enrollment: the base32
// secret for manual entry, the otpauth URI, and a rendered QR PNG (base64). The
// factor it describes is UNCONFIRMED until the user proves a code.
type MfaEnrollChallenge struct {
	Secret      string `json:"secret"`
	OtpauthUri  string `json:"otpauthUri"`
	QrPngBase64 string `json:"qrPngBase64"`
}

// IMfaService is myidsan's TOTP second-factor lifecycle: self-serve enrollment,
// per-login verification (TOTP or a single-use recovery code), and teardown.
type IMfaService interface {
	Status(ctx context.Context, userId int64) (MfaStatus, error)
	// HasConfirmedFactor is the login-gate predicate: does this account require a
	// second factor to complete a password login?
	HasConfirmedFactor(ctx context.Context, userId int64) (bool, error)
	// BeginEnroll stages a new (unconfirmed) TOTP factor and returns the enrollment
	// challenge. Refuses if a CONFIRMED factor already exists (delete it first).
	BeginEnroll(ctx context.Context, userId int64, accountEmail, label string) (MfaEnrollChallenge, error)
	// ConfirmEnroll validates the first code against the pending factor, marks it
	// confirmed, and mints the one-time recovery codes (returned in plaintext once).
	ConfirmEnroll(ctx context.Context, userId int64, code string) ([]string, error)
	// VerifyCode checks a code against a CONFIRMED factor: first TOTP (advancing the
	// replay guard), then an unused recovery code (consuming it). This is the single
	// verification path used by the login challenge and by self-service teardown, so it
	// is also the only place that knows a recovery code was burned — hence the result
	// carries the kind rather than a bare bool.
	VerifyCode(ctx context.Context, userId int64, code string) (MfaVerifyResult, error)
	// RegenerateRecovery issues a fresh recovery-code set, invalidating the old one.
	RegenerateRecovery(ctx context.Context, userId int64) ([]string, error)
	// Disable removes the user's factor and all recovery codes (self-service teardown
	// after re-proving possession, or superadmin lost-device reset).
	Disable(ctx context.Context, userId int64) error
}

type mfaService struct {
	factors  dbsql.IGenericRepo[myidsanentities.UserMfaFactor]
	recovery dbsql.IGenericRepo[myidsanentities.UserMfaRecoveryCode]
	cipher   *atrest.Cipher
	issuer   string
}

// NewMfaService constructs the MFA service. cipher may be nil (encryption-at-rest
// disabled) — the shared secret is then stored as-is, matching the suite-wide
// atrest fallback used for the directory bind password.
func NewMfaService(
	factors dbsql.IGenericRepo[myidsanentities.UserMfaFactor],
	recovery dbsql.IGenericRepo[myidsanentities.UserMfaRecoveryCode],
	cipher *atrest.Cipher,
	issuer string,
) IMfaService {
	if issuer == "" {
		issuer = "myidsan"
	}
	return &mfaService{factors: factors, recovery: recovery, cipher: cipher, issuer: issuer}
}

// loadFactor returns the user's TOTP factor, or nil if none is enrolled.
func (m *mfaService) loadFactor(ctx context.Context, userId int64) (*myidsanentities.UserMfaFactor, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "UserLoginId", Compare: sqldataenums.Equal, Value: userId},
		{FieldName: "Kind", Compare: sqldataenums.Equal, Value: mfaFactorKind},
	}
	rows, _, err := m.factors.Get(ctx, "", 1, 0, filters, nil)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil || rows[0].Id == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (m *mfaService) Status(ctx context.Context, userId int64) (MfaStatus, error) {
	factor, err := m.loadFactor(ctx, userId)
	if err != nil {
		return MfaStatus{}, err
	}
	if factor == nil || factor.ConfirmedAt == 0 {
		return MfaStatus{Enrolled: false}, nil
	}
	remaining, err := m.countUnusedRecovery(ctx, userId)
	if err != nil {
		return MfaStatus{}, err
	}
	return MfaStatus{
		Enrolled:          true,
		ConfirmedAt:       factor.ConfirmedAt,
		Label:             factor.Label,
		RecoveryRemaining: remaining,
	}, nil
}

func (m *mfaService) HasConfirmedFactor(ctx context.Context, userId int64) (bool, error) {
	factor, err := m.loadFactor(ctx, userId)
	if err != nil {
		return false, err
	}
	return factor != nil && factor.ConfirmedAt != 0, nil
}

func (m *mfaService) BeginEnroll(ctx context.Context, userId int64, accountEmail, label string) (MfaEnrollChallenge, error) {
	existing, err := m.loadFactor(ctx, userId)
	if err != nil {
		return MfaEnrollChallenge{}, err
	}
	if existing != nil && existing.ConfirmedAt != 0 {
		return MfaEnrollChallenge{}, ErrMfaAlreadyEnrolled
	}

	secret, err := mfa.GenerateSecret()
	if err != nil {
		return MfaEnrollChallenge{}, err
	}
	sealed, err := encodeSecret(m.cipher, secret)
	if err != nil {
		return MfaEnrollChallenge{}, err
	}

	now := time.Now().Unix()
	if existing != nil {
		// Overwrite the prior UNCONFIRMED staging factor with a fresh secret.
		existing.SecretEnc = sealed
		existing.Label = label
		existing.ConfirmedAt = 0
		existing.LastStep = 0
		existing.UpdatedBy = userId
		existing.UpdatedAt = now
		if _, err := m.factors.UpdateById(ctx, "", *existing); err != nil {
			return MfaEnrollChallenge{}, err
		}
	} else {
		factor := myidsanentities.UserMfaFactor{
			UserLoginId: userId,
			Kind:        mfaFactorKind,
			SecretEnc:   sealed,
			Label:       label,
			CreatedBy:   userId,
			CreatedAt:   now,
			UpdatedBy:   userId,
			UpdatedAt:   now,
		}
		if _, err := m.factors.Create(ctx, "", factor); err != nil {
			return MfaEnrollChallenge{}, err
		}
	}

	uri := mfa.OtpauthURI(m.issuer, accountEmail, secret)
	png, err := mfa.QRPNG(uri, 256)
	if err != nil {
		return MfaEnrollChallenge{}, err
	}
	return MfaEnrollChallenge{
		Secret:      secret,
		OtpauthUri:  uri,
		QrPngBase64: base64.StdEncoding.EncodeToString(png),
	}, nil
}

func (m *mfaService) ConfirmEnroll(ctx context.Context, userId int64, code string) ([]string, error) {
	factor, err := m.loadFactor(ctx, userId)
	if err != nil {
		return nil, err
	}
	if factor == nil {
		return nil, ErrMfaNotEnrolling
	}
	if factor.ConfirmedAt != 0 {
		return nil, ErrMfaAlreadyEnrolled
	}

	secret := decodeSecret(m.cipher, factor.SecretEnc)
	ok, step := mfa.Validate(secret, code, time.Now(), factor.LastStep)
	if !ok {
		return nil, ErrMfaBadCode
	}

	now := time.Now().Unix()
	factor.ConfirmedAt = now
	factor.LastStep = step
	factor.LastUsedAt = now
	factor.UpdatedBy = userId
	factor.UpdatedAt = now
	if _, err := m.factors.UpdateById(ctx, "", *factor); err != nil {
		return nil, err
	}

	return m.replaceRecoveryCodes(ctx, userId)
}

func (m *mfaService) VerifyCode(ctx context.Context, userId int64, code string) (MfaVerifyResult, error) {
	factor, err := m.loadFactor(ctx, userId)
	if err != nil {
		return MfaVerifyResult{}, err
	}
	if factor == nil || factor.ConfirmedAt == 0 {
		return MfaVerifyResult{}, ErrMfaNotEnrolled
	}

	// TOTP first: cheapest, and the common case.
	secret := decodeSecret(m.cipher, factor.SecretEnc)
	if ok, step := mfa.Validate(secret, code, time.Now(), factor.LastStep); ok {
		now := time.Now().Unix()
		factor.LastStep = step
		factor.LastUsedAt = now
		factor.UpdatedAt = now
		if _, err := m.factors.UpdateById(ctx, "", *factor); err != nil {
			return MfaVerifyResult{}, err
		}
		return MfaVerifyResult{Ok: true}, nil
	}

	// Fall back to a single-use recovery code.
	ok, err := m.consumeRecoveryCode(ctx, userId, code)
	if err != nil {
		return MfaVerifyResult{}, err
	}
	return MfaVerifyResult{Ok: ok, UsedRecovery: ok}, nil
}

func (m *mfaService) RegenerateRecovery(ctx context.Context, userId int64) ([]string, error) {
	confirmed, err := m.HasConfirmedFactor(ctx, userId)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		return nil, ErrMfaNotEnrolled
	}
	return m.replaceRecoveryCodes(ctx, userId)
}

func (m *mfaService) Disable(ctx context.Context, userId int64) error {
	if err := m.deleteRecoveryCodes(ctx, userId); err != nil {
		return err
	}
	factor, err := m.loadFactor(ctx, userId)
	if err != nil {
		return err
	}
	if factor == nil {
		return nil
	}
	_, err = m.factors.DeleteById(ctx, "", uint64(factor.Id))
	return err
}

// --- recovery-code helpers ------------------------------------------------

func (m *mfaService) loadRecoveryCodes(ctx context.Context, userId int64) ([]*myidsanentities.UserMfaRecoveryCode, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "UserLoginId", Compare: sqldataenums.Equal, Value: userId},
	}
	rows, _, err := m.recovery.Get(ctx, "", 100, 0, filters, nil)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	return rows, nil
}

func (m *mfaService) countUnusedRecovery(ctx context.Context, userId int64) (int, error) {
	rows, err := m.loadRecoveryCodes(ctx, userId)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if r != nil && r.UsedAt == 0 {
			n++
		}
	}
	return n, nil
}

func (m *mfaService) deleteRecoveryCodes(ctx context.Context, userId int64) error {
	rows, err := m.loadRecoveryCodes(ctx, userId)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r == nil || r.Id == 0 {
			continue
		}
		if _, err := m.recovery.DeleteById(ctx, "", uint64(r.Id)); err != nil {
			return err
		}
	}
	return nil
}

func (m *mfaService) replaceRecoveryCodes(ctx context.Context, userId int64) ([]string, error) {
	if err := m.deleteRecoveryCodes(ctx, userId); err != nil {
		return nil, err
	}
	codes, err := mfa.GenerateRecoveryCodes()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	for _, code := range codes {
		hash, err := bcrypt.GenerateFromPassword([]byte(mfa.NormalizeRecoveryCode(code)), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		row := myidsanentities.UserMfaRecoveryCode{
			UserLoginId: userId,
			CodeHash:    string(hash),
			CreatedBy:   userId,
			CreatedAt:   now,
		}
		if _, err := m.recovery.Create(ctx, "", row); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// consumeRecoveryCode matches `code` against the user's unused recovery codes in
// constant time across the set, marking the first match used. Returns false if no
// unused code matches.
func (m *mfaService) consumeRecoveryCode(ctx context.Context, userId int64, code string) (bool, error) {
	normalized := mfa.NormalizeRecoveryCode(code)
	if normalized == "" {
		return false, nil
	}
	rows, err := m.loadRecoveryCodes(ctx, userId)
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if r == nil || r.UsedAt != 0 {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(r.CodeHash), []byte(normalized)) == nil {
			r.UsedAt = time.Now().Unix()
			if _, err := m.recovery.UpdateById(ctx, "", *r); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

// --- at-rest secret wrapping (mirrors directory.go) -----------------------

func encodeSecret(cipher *atrest.Cipher, plaintext string) (string, error) {
	if cipher == nil || plaintext == "" {
		return plaintext, nil
	}
	enc, err := cipher.EncryptBytes([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

func decodeSecret(cipher *atrest.Cipher, stored string) string {
	if stored == "" {
		return stored
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || !atrest.IsEncrypted(raw) {
		return stored
	}
	if cipher == nil {
		return stored
	}
	pt, err := cipher.DecryptBytes(raw)
	if err != nil {
		return stored
	}
	return string(pt)
}
