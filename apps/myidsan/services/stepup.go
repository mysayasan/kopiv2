package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/cache"
)

// Step-up (re-)authentication.
//
// The problem it solves: a myidsan session lasts 72 hours, and a superadmin session can
// assign roles, clear anyone's second factor, issue a temporary password for any account,
// and export or restore the entire identity store. A cookie stolen from an unlocked laptop
// therefore grants all of that for three days with no further proof of identity.
//
// Step-up requires the operator to prove they still hold the credential — password, plus a
// TOTP code when they have a factor enrolled — within a short window before those specific
// actions. It is NOT a second session: it is a short-lived marker attached to the session
// that already exists, so an attacker holding only the cookie cannot produce it.
//
// The marker lives in the cache rather than the database deliberately. It must expire on
// its own, it must vanish when the session is revoked (the key is derived from the session
// id, and a revoked session can never present that id again), and it should not survive a
// restart of the server any longer than the sessions it qualifies.

// StepUpWindow is how long a re-authentication counts for. Long enough to complete a batch
// of admin work without re-typing a password per click; short enough that a walk-away
// laptop is not still elevated. Five minutes is the same order as the MFA challenge TTL.
const StepUpWindow = 5 * time.Minute

const stepUpCachePrefix = "stepup:"

// ErrStepUpRequired is returned to a caller that must re-authenticate first. The API layer
// translates it into a distinguishable response so the UI can prompt rather than showing a
// generic "forbidden" the operator cannot act on.
var ErrStepUpRequired = errors.New("recent re-authentication required for this action")

// IStepUpService grants and checks short-lived re-authentication markers.
type IStepUpService interface {
	// Verify checks the caller's own credentials and, on success, marks their session as
	// recently re-authenticated. Returns ErrInvalidCredential or ErrMfaBadCode on failure,
	// and reports whether a RECOVERY code was what satisfied the second factor so the API
	// layer can record that burn separately from an ordinary re-authentication.
	//
	// email and userId come from the SERVER-ISSUED session claims, never from the request
	// body: taking an identifier from the caller would turn this into a way to test other
	// accounts' passwords from inside any authenticated session.
	Verify(ctx context.Context, userId int64, email, sessionId, password, code string) (usedRecovery bool, err error)
	// IsRecent reports whether this session re-authenticated inside the window.
	IsRecent(ctx context.Context, sessionId string) bool
	// Window exposes the configured lifetime so the UI can say how long it lasts.
	Window() time.Duration
}

type stepUpService struct {
	users IUserLoginService
	mfa   IMfaService
	store cache.Store
}

func NewStepUpService(users IUserLoginService, mfa IMfaService, store cache.Store) IStepUpService {
	return &stepUpService{users: users, mfa: mfa, store: store}
}

func (s *stepUpService) Window() time.Duration { return StepUpWindow }

func (s *stepUpService) Verify(ctx context.Context, userId int64, email, sessionId, password, code string) (bool, error) {
	if s.store == nil || strings.TrimSpace(sessionId) == "" {
		// Without a cache there is nowhere to record the grant. Fail closed: silently
		// treating every caller as re-authenticated would remove the control entirely.
		return false, errors.New("step-up is unavailable on this server")
	}
	if strings.TrimSpace(email) == "" {
		return false, ErrInvalidCredential
	}
	// Re-verify the PASSWORD against the session's own identity.
	if _, err := s.users.AuthenticateDefault(ctx, email, password); err != nil {
		return false, ErrInvalidCredential
	}

	// A second factor, when enrolled, must also be presented — otherwise step-up would be
	// satisfied by a password alone, which is precisely what an attacker who phished one
	// already has.
	usedRecovery := false
	if s.mfa != nil {
		enrolled, err := s.mfa.HasConfirmedFactor(ctx, userId)
		if err != nil {
			return false, err
		}
		if enrolled {
			res, err := s.mfa.VerifyCode(ctx, userId, code)
			if err != nil {
				return false, err
			}
			if !res.Ok {
				return false, ErrMfaBadCode
			}
			usedRecovery = res.UsedRecovery
		}
	}

	return usedRecovery, s.store.Set(ctx, stepUpCachePrefix+sessionId, time.Now().Unix(), StepUpWindow)
}

func (s *stepUpService) IsRecent(ctx context.Context, sessionId string) bool {
	if s.store == nil || strings.TrimSpace(sessionId) == "" {
		return false
	}
	var at int64
	found, err := s.store.Get(ctx, stepUpCachePrefix+sessionId, &at)
	if err != nil {
		// Fail CLOSED. A cache blip must not silently elevate every session; the operator
		// is asked to re-authenticate, which is an inconvenience rather than a breach.
		return false
	}
	return found
}
