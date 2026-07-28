package apis

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	"github.com/mysayasan/kopiv2/infra/cache"
)

// The pre-session MFA challenge. When a password succeeds but the account has a
// confirmed second factor, NO session cookie is issued. Instead an opaque, single-
// use, server-revocable token is minted and the client must exchange it plus a
// valid code for the session. This ordering is load-bearing: a kopiv2_access cookie
// is a federation-capable SSO session, so handing one out before the second factor
// would let a password-only attacker mint relying-app tokens at /api/auth/authorize.
const (
	mfaChallengeTTL     = 5 * time.Minute
	mfaChallengeMaxTry  = 5
	mfaChallengePrefix  = "mfa:challenge:"
	mfaChallengeTokLen  = 32
)

var errMfaChallengeInvalid = errors.New("mfa challenge invalid or expired")

// mfaPendingEntry is the cache value bound to a challenge token. It grants nothing
// on its own — only the right to attempt the second factor for one resolved user,
// from the same client, a bounded number of times.
type mfaPendingEntry struct {
	UserId      int64  `json:"userId"`
	Fingerprint string `json:"fp"`
	Attempts    int    `json:"attempts"`
}

// mfaChallenger issues and redeems challenge tokens against the shared cache Store
// (the same store used for session revocation), delegating code checks to the MFA
// service. Nil-safe: when either dependency is absent (minimal wiring, tests) it
// behaves as "no MFA configured".
type mfaChallenger struct {
	store cache.Store
	mfa   services.IMfaService
}

func newMfaChallenger(store cache.Store, mfa services.IMfaService) *mfaChallenger {
	return &mfaChallenger{store: store, mfa: mfa}
}

// required reports whether the account must clear a second factor before a session
// is issued. False (fail-open to password-only) when MFA is not wired — never when
// a lookup errors, so a transient DB fault cannot silently drop the second factor.
func (c *mfaChallenger) required(ctx context.Context, userId int64) (bool, error) {
	if c == nil || c.mfa == nil || c.store == nil {
		return false, nil
	}
	return c.mfa.HasConfirmedFactor(ctx, userId)
}

// issue mints a challenge token for a resolved user, bound to the request's client
// fingerprint, and stores it with a short TTL.
func (c *mfaChallenger) issue(ctx context.Context, r *http.Request, userId int64) (string, error) {
	token, err := newMfaToken()
	if err != nil {
		return "", err
	}
	entry := mfaPendingEntry{UserId: userId, Fingerprint: mfaFingerprint(r)}
	if err := c.store.Set(ctx, mfaChallengePrefix+token, entry, mfaChallengeTTL); err != nil {
		return "", err
	}
	return token, nil
}

// redeem verifies (token, code) from this request. On success it deletes the token
// (single-use) and returns the resolved user id. Every failure path is explicit:
// unknown/expired token, client rebinding, exhausted attempts, or a bad code. A bad
// code increments the per-token counter and deletes the token once exhausted, so a
// captured token cannot be ground against 10^6 codes.
func (c *mfaChallenger) redeem(ctx context.Context, r *http.Request, token, code string) (int64, error) {
	if c == nil || c.mfa == nil || c.store == nil {
		return 0, errMfaChallengeInvalid
	}
	key := mfaChallengePrefix + token
	var entry mfaPendingEntry
	found, err := c.store.Get(ctx, key, &entry)
	if err != nil {
		return 0, err
	}
	if !found || entry.UserId == 0 {
		return 0, errMfaChallengeInvalid
	}
	// Reject if the token is replayed from a different client than it was issued to.
	if subtle.ConstantTimeCompare([]byte(entry.Fingerprint), []byte(mfaFingerprint(r))) != 1 {
		_ = c.store.Delete(ctx, key)
		return 0, errMfaChallengeInvalid
	}

	ok, err := c.mfa.VerifyCode(ctx, entry.UserId, code)
	if err != nil {
		return 0, err
	}
	if !ok {
		entry.Attempts++
		if entry.Attempts >= mfaChallengeMaxTry {
			_ = c.store.Delete(ctx, key)
		} else {
			// Preserve the remaining TTL best-effort by re-setting with the full
			// window; a short over-extension is harmless versus losing the counter.
			_ = c.store.Set(ctx, key, entry, mfaChallengeTTL)
		}
		return 0, services.ErrMfaBadCode
	}

	_ = c.store.Delete(ctx, key)
	return entry.UserId, nil
}

// mfaFingerprint binds a challenge to the client that requested it: the connecting
// IP (never a spoofable forwarded header — reuses loginGuardKey) plus the
// User-Agent, hashed so the cache never stores the raw values.
func mfaFingerprint(r *http.Request) string {
	sum := sha256.Sum256([]byte(loginGuardKey(r) + "\x00" + r.UserAgent()))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func newMfaToken() (string, error) {
	buf := make([]byte, mfaChallengeTokLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
