package apis

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// The source key must come from the peer address and NEVER from a forwarded header. A guard
// that believes X-Forwarded-For can be stepped around by changing a string, which makes the
// whole lockout decorative.
func TestLoginGuardSourceKeyIgnoresForwardedHeader(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/auth/local-login", nil)
	r.RemoteAddr = "203.0.113.7:51514"
	r.Header.Set("X-Forwarded-For", "198.51.100.9")
	r.Header.Set("X-Real-IP", "198.51.100.9")

	if got := LoginGuardSourceKey(r); got != "ip:203.0.113.7" {
		t.Fatalf("got %q, want the PEER address — a forwarded header is attacker-controlled", got)
	}
}

// A RemoteAddr with no port (some test servers, some tunnels) must still yield a usable key
// rather than an empty one, which would collapse every source onto a single counter.
func TestLoginGuardSourceKeyWithoutPort(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/auth/local-login", nil)
	r.RemoteAddr = "203.0.113.7"
	if got := LoginGuardSourceKey(r); got != "ip:203.0.113.7" {
		t.Fatalf("got %q, want ip:203.0.113.7", got)
	}
}

func TestLoginGuardAccountKeyIsCaseFolded(t *testing.T) {
	// "Admin" and "admin" are the same account, so a sprayer must not get a fresh allowance
	// by changing the capitalisation of the username.
	if a, b := LoginGuardAccountKey("Admin"), LoginGuardAccountKey("  admin "); a != b {
		t.Fatalf("got %q and %q, want one key for one account", a, b)
	}
	// An empty identifier yields no key at all: a failure with no username attached (a
	// malformed body) must not be attributed to some arbitrary account.
	if got := LoginGuardAccountKey("   "); got != "" {
		t.Fatalf("got %q, want no account key for an empty identifier", got)
	}
}

func TestLoginGuardKeysPairsSourceAndAccount(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/auth/local-login", nil)
	r.RemoteAddr = "203.0.113.7:51514"

	want := []string{"ip:203.0.113.7", "user:admin"}
	if got := LoginGuardKeys(r, "Admin"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// No identifier — the source key alone, never an empty second key that would make every
	// anonymous failure share one counter with every other.
	if got := LoginGuardKeys(r, ""); !reflect.DeepEqual(got, []string{"ip:203.0.113.7"}) {
		t.Fatalf("got %v, want the source key alone", got)
	}
}

// The remaining wait has to reach the BODY, not only the Retry-After header: a browser cannot
// read that header cross-origin without an explicit Access-Control-Expose-Headers, and a
// countdown that silently shows nothing is how an operator concludes the app is broken.
//
// The body shape is also what tells this refusal apart from the generic rate limiter's, which
// answers the same status on the same endpoints.
func TestWriteLockoutJSONCarriesTheWaitBothWays(t *testing.T) {
	w := httptest.NewRecorder()
	WriteLockoutJSON(w, 90*time.Second, "too many failed sign-in attempts")

	if w.Code != 429 {
		t.Fatalf("status got %d want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "90" {
		t.Fatalf("Retry-After got %q want 90", got)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, w.Body.String())
	}
	if body["retryAfterSeconds"] != float64(90) {
		t.Fatalf("retryAfterSeconds got %v want 90", body["retryAfterSeconds"])
	}
	if body["message"] != "too many failed sign-in attempts" {
		t.Fatalf("message got %v", body["message"])
	}
}

// A sub-second remainder must round UP to 1, never down to 0: "retry after 0 seconds" invites
// an immediate retry that is refused again, which reads as a broken endpoint rather than a
// lockout and turns a countdown into a hot loop.
func TestWriteLockoutJSONNeverReportsZero(t *testing.T) {
	w := httptest.NewRecorder()
	WriteLockoutJSON(w, 200*time.Millisecond, "locked")
	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After got %q want 1", got)
	}
}
