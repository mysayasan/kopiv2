package apis

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/myidsan/services"
	sharedapis "github.com/mysayasan/kopiv2/domain/shared/apis"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
)

// stepUpErrorCode is the sentinel the SPA looks for to decide "prompt for the password"
// instead of "show an error". A generic 403 would leave the operator stuck: the action is
// legitimately theirs to take, they simply have to prove it is still them.
const stepUpErrorCode = "step_up_required"

type stepUpApi struct {
	stepUp services.IStepUpService
	guard  *sharedapis.LoginGuard
	auditRecorder
}

// NewStepUpApi mounts re-authentication.
//
//	GET  /api/step-up   is this session currently elevated, and for how long
//	POST /api/step-up   prove the password (+ TOTP when enrolled) to elevate it
//
// Authenticated but NOT superadmin-gated: an ordinary user has nothing to elevate today,
// but the endpoint must work for whoever the gated actions are later extended to, and
// restricting it would only mean a role change could never be re-authenticated by the
// person about to make one.
//
// guard is the SAME LoginGuard the sign-in surfaces share, and passing it here is not
// belt-and-braces. POST /api/step-up takes a password and says whether it was right, which
// makes it a password-checking endpoint in every sense that matters — and it was the only
// one on this server with no lockout behind it. An attacker holding a stolen cookie could
// not sign in (they lack the password) but could ask this endpoint about candidate
// passwords as fast as the network allowed, unthrottled and undelayed, until they found
// the one credential the whole control rests on. A live bench guessed twelve times in
// 0.6 seconds with no push-back of any kind.
func NewStepUpApi(
	router *mux.Router,
	auth middlewares.AuthMidware,
	stepUp services.IStepUpService,
	audit services.IAuditService,
	guard *sharedapis.LoginGuard,
	trustedProxies []string,
) {
	h := &stepUpApi{stepUp: stepUp, guard: guard, auditRecorder: newAuditRecorder(audit, trustedProxies)}

	group := router.PathPrefix("/step-up").Subrouter()
	group.Use(auth.Middleware)
	group.HandleFunc("", h.status).Methods("GET")
	group.HandleFunc("", h.verify).Methods("POST")
}

func (a *stepUpApi) status(w http.ResponseWriter, r *http.Request) {
	claims := callerClaims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "not signed in")
		return
	}
	controllers.SendResult(w, map[string]any{
		"elevated":      a.stepUp.IsRecent(r.Context(), claims.SessionId),
		"windowSeconds": int(a.stepUp.Window().Seconds()),
	})
}

func (a *stepUpApi) verify(w http.ResponseWriter, r *http.Request) {
	claims := callerClaims(r)
	if claims == nil {
		controllers.SendError(w, controllers.ErrAuthFailed, "not signed in")
		return
	}
	// Checked BEFORE the body is read, and before any credential work: a locked-out caller
	// must cost nothing to refuse, or the throttle is also the load.
	if locked, retry := guardLocked(a.guard, r, claims.Email); locked {
		// The wait goes in the message as well as the Retry-After header: the SPA shows this
		// text inside the re-authentication prompt, and "try again later" in a modal the
		// operator is already blocked by is the kind of dead end people route around.
		writeLockout(w, retry, fmt.Sprintf(
			"too many failed re-authentication attempts — try again in %d seconds",
			int(math.Ceil(retry.Seconds()))))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		controllers.SendError(w, controllers.ErrParseFailed, "invalid request body")
		return
	}

	// The identity comes from the session claims, never the body.
	usedRecovery, err := a.stepUp.Verify(r.Context(), claims.Id, claims.Email, claims.SessionId, body.Password, body.Code)
	if err != nil {
		// A failed step-up is a security event in its own right: it is what an attacker
		// holding only a stolen cookie would produce while trying to escalate.
		a.record(r, services.AuditEntry{
			Action:     services.ActionStepUpFailure,
			TargetType: "self",
			TargetId:   strconv.FormatInt(claims.Id, 10),
			Outcome:    services.OutcomeDenied,
			Detail:     "re-authentication failed",
		})
		switch {
		case errors.Is(err, services.ErrInvalidCredential), errors.Is(err, services.ErrMfaBadCode):
			// Count it against the SAME lockout the sign-in surfaces share. The account key
			// comes from the session claims rather than a submitted username, so unlike the
			// login door this counter cannot be aimed at an account by a stranger — only by
			// someone already holding that account's cookie, which is exactly the attacker
			// this endpoint has to slow down.
			a.recordThrottledFailure(r, claims.Email)
			// One message for both, so this cannot be used to learn whether the account
			// has a second factor enrolled.
			controllers.SendError(w, controllers.ErrAuthFailed, "those credentials were not accepted")
		default:
			controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		}
		return
	}
	guardSuccess(a.guard, r, claims.Email)

	if usedRecovery {
		// A recovery code spent here is a break-glass secret gone for good. Recorded in its
		// own right: the step-up entry below says someone re-authenticated, which is the
		// routine case and looks identical.
		a.record(r, services.AuditEntry{
			Action:     services.ActionMfaRecovery,
			TargetType: "self",
			TargetId:   strconv.FormatInt(claims.Id, 10),
			Detail:     "a single-use recovery code was spent to re-authenticate",
			Metadata:   map[string]any{"method": services.MethodRecovery, "surface": "step-up"},
		})
	}
	a.record(r, services.AuditEntry{
		Action:     services.ActionStepUpSuccess,
		TargetType: "self",
		TargetId:   strconv.FormatInt(claims.Id, 10),
		Detail:     "re-authenticated for a sensitive action",
		Metadata:   map[string]any{"windowSeconds": int(a.stepUp.Window().Seconds())},
	})
	controllers.SendResult(w, map[string]any{
		"elevated":      true,
		"windowSeconds": int(a.stepUp.Window().Seconds()),
	})
}

// recordThrottledFailure delays this response and advances the shared lockout counters,
// then records the lockout itself if this attempt is the one that engaged it.
//
// The constant delay matters as much as the counter: without it an attacker gets to make
// their first eight guesses at wire speed, and eight free guesses against a password is a
// meaningfully different proposition from eight slow ones when the candidate list is a
// breach dump ordered by likelihood.
func (a *stepUpApi) recordThrottledFailure(r *http.Request, email string) {
	if a.guard == nil {
		return
	}
	time.Sleep(a.guard.FailedDelay())
	lockedNow, retry := a.guard.RecordFailure(loginGuardKeys(r, email)...)
	if !lockedNow {
		return
	}
	log.Printf("step-up lockout engaged ip=%s account=%q retryAfter=%s", loginGuardKey(r), email, retry)
	a.record(r, services.AuditEntry{
		Action:     services.ActionLoginLockout,
		TargetType: "self",
		TargetId:   email,
		Outcome:    services.OutcomeDenied,
		Detail:     "too many failed re-authentication attempts",
		Metadata:   map[string]any{"surface": "step-up", "retryAfterSeconds": int(retry.Seconds())},
	})
}

// requireStepUp guards one sensitive handler. It is a plain wrapper rather than middleware
// because it is applied to individual routes, not whole subrouters — most of what a
// superadmin does does not warrant re-typing a password.
//
// A nil service means step-up is not configured, in which case the guard is inert. That is
// a deliberate choice for an install with no cache: the alternative would be to make the
// affected admin actions permanently unreachable.
func requireStepUp(stepUp services.IStepUpService, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if stepUp == nil {
			next(w, r)
			return
		}
		claims := callerClaims(r)
		if claims == nil {
			controllers.SendError(w, controllers.ErrAuthFailed, "not signed in")
			return
		}
		if !stepUp.IsRecent(r.Context(), claims.SessionId) {
			// The code travels in the message because SendError has no structured-detail
			// channel; the SPA matches on it to open the re-authentication prompt.
			controllers.SendError(w, controllers.ErrLimitedAccess, stepUpErrorCode)
			return
		}
		next(w, r)
	}
}
