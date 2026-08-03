package services

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	myidsanentities "github.com/mysayasan/kopiv2/apps/myidsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	wa "github.com/mysayasan/kopiv2/infra/webauthn"
)

// WebAuthn (FIDO2) security keys as a second factor, alongside TOTP.
//
// Why this sits beside IMfaService rather than inside it: a TOTP factor is ONE row holding a
// symmetric secret that must be sealed at rest, and its verification is a code comparison.
// A WebAuthn credential is MANY rows holding public keys that need no sealing, and its
// verification is a signature check bound to the request's origin. The two share only the
// question they answer at the login gate ("does this account owe a second factor?"), which
// is why that question is asked of both by the challenge layer (apis/mfa_challenge.go's
// mfaChallenger — its required() ORs the two and methods() reports which are available)
// rather than by one service pretending to be the other.
//
// The private key never leaves the authenticator. That is the security argument for
// preferring a key over TOTP here: a read of myidsan's database yields every TOTP secret
// (sealed, but the key lives on the same host) and NO WebAuthn signing key.

// WebAuthn errors surfaced to the API layer.
var (
	ErrWebAuthnDisabled  = errors.New("security keys are not enabled on this server")
	ErrWebAuthnNoCeremony = errors.New("no pending security-key ceremony, or it expired")
	ErrWebAuthnNotFound  = errors.New("security key not found")
	ErrWebAuthnNoKeys    = errors.New("no security key is enrolled")
)

const (
	// webauthnSessionPrefix namespaces the in-flight ceremony state in the shared cache.
	// The ceremony is stateful (a server-generated challenge must be matched to the
	// response that signs it) but the state is short-lived and single-use — a cache entry,
	// not a table, which is the same call the pre-session MFA challenge makes.
	webauthnSessionPrefix = "webauthn:sess:"
	// webauthnSessionSlack pads the cache TTL past the ceremony timeout so the entry
	// outlives the browser prompt and the server, not the cache, is what reports expiry.
	webauthnSessionSlack = 60 * time.Second
	// webauthnMaxKeys caps enrolments per account. Generous enough for "one on the
	// keyring, one in the safe, one at the office", bounded so the exclusion list sent to
	// the browser cannot grow without limit.
	webauthnMaxKeys = 10
)

// WebAuthnCredentialView is the non-sensitive projection returned to the owning user and to
// admin screens. It carries no public key — that is useless to a client and only invites
// someone to try trusting it.
type WebAuthnCredentialView struct {
	Id           int64  `json:"id"`
	Label        string `json:"label"`
	Transports   string `json:"transports"`
	Aaguid       string `json:"aaguid"`
	SignCount    int64  `json:"signCount"`
	BackedUp     bool   `json:"backedUp"`
	CloneWarning bool   `json:"cloneWarning"`
	CreatedAt    int64  `json:"createdAt"`
	LastUsedAt   int64  `json:"lastUsedAt"`
}

// IWebAuthnService is the security-key lifecycle: enrolment, per-login assertion, and
// teardown. Every ceremony method takes the *http.Request because the Relying Party ID and
// allowed origin are derived from it (see infra/webauthn) — they are the security boundary
// of the ceremony and cannot be inferred from the user alone.
type IWebAuthnService interface {
	// Enabled reports whether the feature is switched on for this install.
	Enabled() bool
	// List returns the account's enrolled keys, newest first.
	List(ctx context.Context, userId int64) ([]WebAuthnCredentialView, error)
	// HasCredential is the login-gate predicate for this factor kind.
	HasCredential(ctx context.Context, userId int64) (bool, error)

	// BeginEnroll issues a creation challenge and stashes the ceremony state under
	// stateKey. Existing credentials are sent as exclusions so the same authenticator
	// cannot be enrolled to this account twice.
	BeginEnroll(ctx context.Context, r *http.Request, stateKey string, userId int64, email, display string) (*wa.CredentialCreation, error)
	// FinishEnroll verifies the attestation response and persists the credential.
	FinishEnroll(ctx context.Context, r *http.Request, stateKey string, userId int64, label string, body io.Reader) (WebAuthnCredentialView, error)

	// BeginAssert issues an assertion challenge for an already-identified account.
	BeginAssert(ctx context.Context, r *http.Request, stateKey string, userId int64, email, display string) (*wa.CredentialAssertion, error)
	// FinishAssert verifies an assertion. A true first return means the factor is proven.
	// The second return is a non-fatal anomaly note (today: a non-advancing signature
	// counter) that the caller should audit.
	FinishAssert(ctx context.Context, r *http.Request, stateKey string, userId int64, email, display string, body io.Reader) (bool, string, error)

	// Rename retitles one of the account's own keys.
	Rename(ctx context.Context, userId, credentialRowId int64, label string) error
	// Delete removes one of the account's own keys.
	Delete(ctx context.Context, userId, credentialRowId int64) error
	// DeleteAllForUser removes every key for an account (superadmin lost-device reset, and
	// the account-deletion path).
	DeleteAllForUser(ctx context.Context, userId int64) error
}

type webauthnService struct {
	repo      dbsql.IGenericRepo[myidsanentities.UserWebauthnCredential]
	store     cache.Store
	authority *wa.Authority
	timeout   time.Duration
}

// NewWebAuthnService constructs the service. A nil store disables the feature outright
// rather than silently degrading: without somewhere to keep the challenge between the two
// legs there is no ceremony to run, and pretending otherwise would accept an assertion
// against a challenge nobody issued.
func NewWebAuthnService(
	repo dbsql.IGenericRepo[myidsanentities.UserWebauthnCredential],
	store cache.Store,
	authority *wa.Authority,
	timeout time.Duration,
) IWebAuthnService {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &webauthnService{repo: repo, store: store, authority: authority, timeout: timeout}
}

func (s *webauthnService) Enabled() bool {
	return s != nil && s.repo != nil && s.store != nil && s.authority.Enabled()
}

// --- persistence ------------------------------------------------------------

// loadRows returns every credential for a user. Filtered with an Equal filter through Get
// rather than GetByForeign: that generic helper returns only ONE child row, which is
// precisely wrong for a table whose whole purpose is holding several keys per account.
func (s *webauthnService) loadRows(ctx context.Context, userId int64) ([]*myidsanentities.UserWebauthnCredential, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "UserLoginId", Compare: sqldataenums.Equal, Value: userId},
	}
	rows, _, err := s.repo.Get(ctx, "", webauthnMaxKeys+1, 0, filters, nil)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]*myidsanentities.UserWebauthnCredential, 0, len(rows))
	for _, r := range rows {
		if r != nil && r.Id != 0 {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *webauthnService) List(ctx context.Context, userId int64) ([]WebAuthnCredentialView, error) {
	if s.repo == nil {
		return nil, nil
	}
	rows, err := s.loadRows(ctx, userId)
	if err != nil {
		return nil, err
	}
	views := make([]WebAuthnCredentialView, 0, len(rows))
	for _, r := range rows {
		views = append(views, WebAuthnCredentialView{
			Id:           r.Id,
			Label:        r.Label,
			Transports:   r.Transports,
			Aaguid:       r.Aaguid,
			SignCount:    r.SignCount,
			BackedUp:     r.BackedUp,
			CloneWarning: r.CloneWarning,
			CreatedAt:    r.CreatedAt,
			LastUsedAt:   r.LastUsedAt,
		})
	}
	return views, nil
}

func (s *webauthnService) HasCredential(ctx context.Context, userId int64) (bool, error) {
	// Asked on every password login, so it must be cheap AND fail closed on error: a
	// transient database fault must not read as "this account has no second factor".
	if !s.Enabled() {
		return false, nil
	}
	rows, err := s.loadRows(ctx, userId)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// --- ceremony state ---------------------------------------------------------

// webauthnCeremony is the cached state between the two legs. UserId is stored so a response
// cannot be redirected onto a different account than the challenge was issued for.
type webauthnCeremony struct {
	UserId  int64           `json:"userId"`
	Session wa.SessionData  `json:"session"`
}

func (s *webauthnService) putCeremony(ctx context.Context, stateKey string, userId int64, session *wa.SessionData) error {
	return s.store.Set(ctx, webauthnSessionPrefix+stateKey,
		webauthnCeremony{UserId: userId, Session: *session}, s.timeout+webauthnSessionSlack)
}

// takeCeremony loads and DELETES the ceremony state — single-use, so a captured response
// cannot be replayed against the same challenge.
//
// The delete happens BEFORE the ownership check on purpose: a wrong-user response has already
// failed and leaving the challenge alive for another attempt only helps someone probing. It
// is not a way to destroy another user's in-flight ceremony either, because no caller can
// address someone else's stateKey — the enrolment key is built from the caller's OWN claims
// server-side (see apis/webauthn.go enrollStateKey) and the login key is the unguessable
// challenge token.
func (s *webauthnService) takeCeremony(ctx context.Context, stateKey string, userId int64) (*wa.SessionData, error) {
	key := webauthnSessionPrefix + stateKey
	var entry webauthnCeremony
	found, err := s.store.Get(ctx, key, &entry)
	if err != nil {
		return nil, err
	}
	_ = s.store.Delete(ctx, key)
	if !found || entry.UserId == 0 || entry.UserId != userId {
		return nil, ErrWebAuthnNoCeremony
	}
	return &entry.Session, nil
}

// --- lib.User adapter -------------------------------------------------------

// webauthnUser adapts an account to the library's User interface.
type webauthnUser struct {
	id      int64
	email   string
	display string
	creds   []wa.Credential
}

// WebAuthnID is the user handle baked into every credential this account creates, so it
// must be STABLE for the lifetime of the account — change the derivation and every enrolled
// key stops matching its owner. The row id, big-endian, is stable by definition and opaque
// enough: a user handle is not secret, it just must not be a guessable personal identifier.
func (u *webauthnUser) WebAuthnID() []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(u.id))
	return buf
}

func (u *webauthnUser) WebAuthnName() string { return u.email }

func (u *webauthnUser) WebAuthnDisplayName() string {
	if strings.TrimSpace(u.display) != "" {
		return u.display
	}
	return u.email
}

func (u *webauthnUser) WebAuthnCredentials() []wa.Credential { return u.creds }

// buildUser assembles the adapter, decoding stored credentials. A row that fails to decode
// is SKIPPED rather than failing the whole ceremony: one corrupt row must not lock a user
// out of the other keys they hold.
func (s *webauthnService) buildUser(ctx context.Context, userId int64, email, display string) (*webauthnUser, []*myidsanentities.UserWebauthnCredential, error) {
	rows, err := s.loadRows(ctx, userId)
	if err != nil {
		return nil, nil, err
	}
	u := &webauthnUser{id: userId, email: email, display: display}
	for _, r := range rows {
		id, err := base64.RawURLEncoding.DecodeString(r.CredentialId)
		if err != nil {
			continue
		}
		pk, err := base64.StdEncoding.DecodeString(r.PublicKey)
		if err != nil {
			continue
		}
		cred := wa.Credential{ID: id, PublicKey: pk}
		cred.Authenticator.SignCount = uint32(r.SignCount)
		if aaguid, err := base64.StdEncoding.DecodeString(r.Aaguid); err == nil {
			cred.Authenticator.AAGUID = aaguid
		}
		u.creds = append(u.creds, cred)
	}
	return u, rows, nil
}

// --- enrolment --------------------------------------------------------------

func (s *webauthnService) BeginEnroll(ctx context.Context, r *http.Request, stateKey string, userId int64, email, display string) (*wa.CredentialCreation, error) {
	if !s.Enabled() {
		return nil, ErrWebAuthnDisabled
	}
	user, rows, err := s.buildUser(ctx, userId, email, display)
	if err != nil {
		return nil, err
	}
	if len(rows) >= webauthnMaxKeys {
		return nil, fmt.Errorf("at most %d security keys can be enrolled; remove one first", webauthnMaxKeys)
	}
	exclude := make([][]byte, 0, len(user.creds))
	for _, c := range user.creds {
		exclude = append(exclude, c.ID)
	}
	creation, session, err := s.authority.BeginRegistration(r, user, exclude)
	if err != nil {
		return nil, err
	}
	if err := s.putCeremony(ctx, stateKey, userId, session); err != nil {
		return nil, err
	}
	return creation, nil
}

func (s *webauthnService) FinishEnroll(ctx context.Context, r *http.Request, stateKey string, userId int64, label string, body io.Reader) (WebAuthnCredentialView, error) {
	if !s.Enabled() {
		return WebAuthnCredentialView{}, ErrWebAuthnDisabled
	}
	session, err := s.takeCeremony(ctx, stateKey, userId)
	if err != nil {
		return WebAuthnCredentialView{}, err
	}
	// The user adapter is rebuilt (not carried across the two legs) so the verification
	// sees the credential set as it is NOW.
	user, _, err := s.buildUser(ctx, userId, "", "")
	if err != nil {
		return WebAuthnCredentialView{}, err
	}
	cred, err := s.authority.FinishRegistration(r, user, *session, body)
	if err != nil {
		return WebAuthnCredentialView{}, err
	}

	label = strings.TrimSpace(label)
	if label == "" {
		label = "Security key"
	}
	if len(label) > 120 {
		label = label[:120]
	}

	now := time.Now().Unix()
	row := myidsanentities.UserWebauthnCredential{
		UserLoginId:  userId,
		CredentialId: base64.RawURLEncoding.EncodeToString(cred.ID),
		PublicKey:    base64.StdEncoding.EncodeToString(cred.PublicKey),
		Aaguid:       base64.StdEncoding.EncodeToString(cred.Authenticator.AAGUID),
		SignCount:    int64(cred.Authenticator.SignCount),
		Transports:   wa.TransportsCSV(cred),
		Label:        label,
		BackedUp:     cred.Flags.BackupState,
		CreatedBy:    userId,
		CreatedAt:    now,
		UpdatedBy:    userId,
		UpdatedAt:    now,
	}
	id, err := s.repo.Create(ctx, "", row)
	if err != nil {
		return WebAuthnCredentialView{}, err
	}
	row.Id = int64(id)
	return WebAuthnCredentialView{
		Id:         row.Id,
		Label:      row.Label,
		Transports: row.Transports,
		Aaguid:     row.Aaguid,
		SignCount:  row.SignCount,
		BackedUp:   row.BackedUp,
		CreatedAt:  row.CreatedAt,
	}, nil
}

// --- assertion --------------------------------------------------------------

func (s *webauthnService) BeginAssert(ctx context.Context, r *http.Request, stateKey string, userId int64, email, display string) (*wa.CredentialAssertion, error) {
	if !s.Enabled() {
		return nil, ErrWebAuthnDisabled
	}
	user, rows, err := s.buildUser(ctx, userId, email, display)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(user.creds) == 0 {
		return nil, ErrWebAuthnNoKeys
	}
	assertion, session, err := s.authority.BeginLogin(r, user)
	if err != nil {
		return nil, err
	}
	if err := s.putCeremony(ctx, stateKey, userId, session); err != nil {
		return nil, err
	}
	return assertion, nil
}

func (s *webauthnService) FinishAssert(ctx context.Context, r *http.Request, stateKey string, userId int64, email, display string, body io.Reader) (bool, string, error) {
	if !s.Enabled() {
		return false, "", ErrWebAuthnDisabled
	}
	session, err := s.takeCeremony(ctx, stateKey, userId)
	if err != nil {
		return false, "", err
	}
	user, rows, err := s.buildUser(ctx, userId, email, display)
	if err != nil {
		return false, "", err
	}
	cred, err := s.authority.FinishLogin(r, user, *session, body)
	if err != nil {
		return false, "", err
	}

	// Match the signing credential back to its row and advance the replay guard.
	//
	// Signature-counter semantics are the subtle part. A counter that fails to advance is
	// the documented clone signal, but a great many authenticators — most platform ones and
	// every synced passkey — legitimately report 0 forever, so treating "did not advance"
	// as proof of cloning would refuse those outright. The spec leaves the response to the
	// Relying Party; this one ACCEPTS the assertion and records the anomaly (flag on the
	// row + an audit entry via the returned note), because the alternative locks users out
	// of working hardware on a signal that is ambiguous by design. The password was already
	// checked before this point, so the flag is a lead to investigate, not the only guard.
	stored := base64.RawURLEncoding.EncodeToString(cred.ID)
	note := ""
	now := time.Now().Unix()
	for _, row := range rows {
		if row.CredentialId != stored {
			continue
		}
		next := int64(cred.Authenticator.SignCount)
		if cred.Authenticator.CloneWarning || (next != 0 && next <= row.SignCount) {
			row.CloneWarning = true
			note = fmt.Sprintf("signature counter did not advance (stored %d, presented %d)", row.SignCount, next)
		}
		if next > row.SignCount {
			row.SignCount = next
		}
		row.LastUsedAt = now
		row.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *row); err != nil {
			// The assertion itself verified; failing the login because the bookkeeping
			// write failed would be the wrong trade. Surface it as a note instead.
			note = strings.TrimSpace(note + " (counter not persisted: " + err.Error() + ")")
		}
		break
	}
	return true, note, nil
}

// --- management -------------------------------------------------------------

// Rename and Delete deliberately do NOT require Enabled(). Turning the feature off must not
// strip an operator of the ability to REVOKE a key that is already enrolled — switching
// webauthn off is exactly what someone would do after a key was lost, and being unable to
// remove it at that moment is the wrong failure. Enrolment and assertion are gated; teardown
// is not.

// ownedRow fetches one credential and confirms it belongs to userId. Ownership is checked
// HERE rather than trusted from the route so a caller cannot delete someone else's key by
// guessing a row id.
func (s *webauthnService) ownedRow(ctx context.Context, userId, rowId int64) (*myidsanentities.UserWebauthnCredential, error) {
	if s.repo == nil {
		return nil, ErrWebAuthnNotFound
	}
	rows, err := s.loadRows(ctx, userId)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.Id == rowId {
			return r, nil
		}
	}
	return nil, ErrWebAuthnNotFound
}

func (s *webauthnService) Rename(ctx context.Context, userId, rowId int64, label string) error {
	row, err := s.ownedRow(ctx, userId, rowId)
	if err != nil {
		return err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("a name is required")
	}
	if len(label) > 120 {
		label = label[:120]
	}
	row.Label = label
	row.UpdatedBy = userId
	row.UpdatedAt = time.Now().Unix()
	_, err = s.repo.UpdateById(ctx, "", *row)
	return err
}

func (s *webauthnService) Delete(ctx context.Context, userId, rowId int64) error {
	row, err := s.ownedRow(ctx, userId, rowId)
	if err != nil {
		return err
	}
	_, err = s.repo.DeleteById(ctx, "", uint64(row.Id))
	return err
}

func (s *webauthnService) DeleteAllForUser(ctx context.Context, userId int64) error {
	if s.repo == nil {
		return nil
	}
	rows, err := s.loadRows(ctx, userId)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := s.repo.DeleteById(ctx, "", uint64(r.Id)); err != nil {
			return err
		}
	}
	return nil
}
