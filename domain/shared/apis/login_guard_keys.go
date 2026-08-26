package apis

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The keys a credential surface throttles on, and the refusal it writes, in ONE place.
//
// myidsan grew these first, as unexported helpers next to its login handlers. myseliasan then
// needed the identical pair — it is the other Tier A clusterable app, and it had no lockout at
// all — and a second copy is how the audit trail ended up written three times before anyone
// noticed the copies had drifted (only one truncated hostile input, only one had retention).
// The rule these encode is a security decision, not a formatting one, so there is one of it:
//
//   - the SOURCE key comes from RemoteAddr and never from a forwarded header. X-Forwarded-For
//     is attacker-controlled, so believing it would let anyone dodge their own lockout by
//     changing a string. A deployment behind a reverse proxy must terminate at this process;
//     that is the documented posture for the whole suite (see clientIP in local_auth.go), and
//     the audit trail records the forwarded address separately when the proxy is trusted.
//   - the ACCOUNT key exists because a lockout keyed only on the source never sees a spray
//     distributed across many addresses, which is the shape credential stuffing actually
//     takes. The tradeoff is deliberate and was measured, not assumed: someone who knows a
//     username can hold that account locked. That is a nuisance the owner recovers from by
//     waiting; unlimited guessing against a known account is a compromise they do not recover
//     from. Any successful sign-in clears both keys.

// LoginGuardSourceKey is the per-SOURCE key: the connecting peer's IP, never a spoofable
// forwarded header. It throttles one machine trying many accounts.
func LoginGuardSourceKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "ip:" + strings.TrimSpace(r.RemoteAddr)
	}
	return "ip:" + host
}

// LoginGuardAccountKey is the per-ACCOUNT key, lower-cased so "Admin" and "admin" share one
// counter. It returns "" for an empty identifier and callers skip empty keys — a failure with
// no username attached (a malformed body) must not be attributed to some arbitrary account.
func LoginGuardAccountKey(identifier string) string {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" {
		return ""
	}
	return "user:" + id
}

// LoginGuardKeys is the pair every credential surface should throttle on.
func LoginGuardKeys(r *http.Request, identifier string) []string {
	keys := []string{LoginGuardSourceKey(r)}
	if accountKey := LoginGuardAccountKey(identifier); accountKey != "" {
		keys = append(keys, accountKey)
	}
	return keys
}

// WriteLockoutJSON sends the 429 a locked-out caller gets.
//
// The remaining wait travels BOTH as a Retry-After header and as retryAfterSeconds in the
// body: a browser SPA cannot read a response header cross-origin without an explicit
// Access-Control-Expose-Headers, and a countdown that silently shows nothing is how an
// operator concludes the app is broken rather than that they are locked out.
//
// It is also what tells this refusal apart from the generic rate limiter's, which answers the
// same status on the same endpoints. A bench that could not tell them apart passed two
// throttle checks on rate-limit refusals while the lockout it was measuring did not exist.
func WriteLockoutJSON(w http.ResponseWriter, retry time.Duration, message string) {
	secs := int(math.Ceil(retry.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message":           message,
		"retryAfterSeconds": secs,
	})
}
